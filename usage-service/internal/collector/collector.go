package collector

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/config"
	"github.com/seakee/cpa-manager/usage-service/internal/httpqueue"
	"github.com/seakee/cpa-manager/usage-service/internal/mail"
	"github.com/seakee/cpa-manager/usage-service/internal/resp"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

type Status struct {
	Collector      string `json:"collector"`
	Upstream       string `json:"upstream"`
	Mode           string `json:"mode"`
	Transport      string `json:"transport"`
	Queue          string `json:"queue"`
	LastConsumedAt int64  `json:"lastConsumedAt"`
	LastInsertedAt int64  `json:"lastInsertedAt"`
	TotalInserted  int64  `json:"totalInserted"`
	TotalSkipped   int64  `json:"totalSkipped"`
	DeadLetters    int64  `json:"deadLetters"`
	LastError      string `json:"lastError,omitempty"`
}

type RuntimeConfig struct {
	CPAUpstreamURL string
	ManagementKey  string
	CollectorMode  string
	Queue          string
	PopSide        string
	BatchSize      int
	PollInterval   time.Duration
	TLSSkipVerify  bool
}

type Manager struct {
	base             config.Config
	store            *store.Store
	snapshotResolver *authSnapshotResolver
	mu               sync.Mutex
	// spendLimitMu 将配置写入与对 CLIProxyAPI 的协调串行化，避免旧规则在新配置成功后覆盖外部暂停状态。
	spendLimitMu sync.Mutex
	cancel       context.CancelFunc
	status       Status
	runtimeCfg   RuntimeConfig
	mailSender   *mail.Sender
	alertCfg     AlertConfig
	alertMu      sync.RWMutex
}

// AlertConfig controls the periodic spend alert check that sends email
// notifications when a user's daily spend crosses a threshold multiple.
type AlertConfig struct {
	Enabled           bool
	ThresholdCents    int64
	CheckInterval     time.Duration
	PoolAlertEnabled  bool
	PoolCheckInterval time.Duration
}

func NewManager(base config.Config, store *store.Store, sender *mail.Sender, alertCfg AlertConfig) *Manager {
	if alertCfg.CheckInterval <= 0 {
		alertCfg.CheckInterval = 60 * time.Second
	}
	return &Manager{
		base:             base,
		store:            store,
		snapshotResolver: newAuthSnapshotResolver(),
		mailSender:       sender,
		alertCfg:         alertCfg,
		status: Status{
			Collector: "stopped",
			Mode:      collectorMode(base.CollectorMode),
			Queue:     base.Queue,
		},
	}
}

// getAlertConfig returns a copy of the current alert configuration.
func (m *Manager) getAlertConfig() AlertConfig {
	m.alertMu.RLock()
	defer m.alertMu.RUnlock()
	return m.alertCfg
}

// getMailSender returns the current mail sender instance.
func (m *Manager) getMailSender() *mail.Sender {
	m.alertMu.RLock()
	defer m.alertMu.RUnlock()
	return m.mailSender
}

// reloadAlertConfig re-reads alert config from the DB and updates the managed
// copies of mailSender and alertCfg. This allows config changes made via the
// HTTP API (saved to settings table) to take effect without restarting the service.
func (m *Manager) reloadAlertConfig() {
	if m.store == nil {
		return
	}
	dbCfg, ok, err := m.store.LoadAlertConfigWithPassword(context.Background())
	if err != nil {
		log.Printf("alert: reload config from DB failed: %v", err)
		return
	}
	if !ok {
		return
	}

	m.alertMu.Lock()
	defer m.alertMu.Unlock()

	// Merge: DB overrides env defaults; empty fields fall back to env defaults
	smtpHost := dbCfg.SMTPHost
	if smtpHost == "" {
		smtpHost = m.base.SMTPHost
	}
	smtpPort := dbCfg.SMTPPort
	if smtpPort <= 0 {
		smtpPort = m.base.SMTPPort
	}
	smtpUsername := dbCfg.SMTPUsername
	if smtpUsername == "" {
		smtpUsername = m.base.SMTPUsername
	}
	smtpPassword := dbCfg.SMTPPassword
	if smtpPassword == "" {
		smtpPassword = m.base.SMTPPassword
	}
	smtpFrom := dbCfg.SMTPFrom
	if smtpFrom == "" {
		smtpFrom = m.base.SMTPFrom
	}
	smtpFromName := dbCfg.SMTPFromName
	if smtpFromName == "" {
		smtpFromName = m.base.SMTPFromName
	}

	m.mailSender = mail.NewSender(mail.Config{
		Host:     smtpHost,
		Port:     smtpPort,
		Username: smtpUsername,
		Password: smtpPassword,
		From:     smtpFrom,
		FromName: smtpFromName,
	})

	checkInterval := time.Duration(dbCfg.CheckIntervalMS) * time.Millisecond
	if checkInterval <= 0 {
		checkInterval = 60 * time.Second
	}
	poolInterval := time.Duration(dbCfg.PoolCheckInterval) * time.Minute
	if poolInterval <= 0 {
		poolInterval = 5 * time.Minute
	}

	m.alertCfg = AlertConfig{
		Enabled:           dbCfg.AlertEnabled,
		ThresholdCents:    dbCfg.ThresholdCents,
		CheckInterval:     checkInterval,
		PoolAlertEnabled:  dbCfg.PoolCheckEnabled,
		PoolCheckInterval: poolInterval,
	}
}

func (m *Manager) Start(ctx context.Context, cfg RuntimeConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.runtimeCfg = cfg
	m.status.Collector = "starting"
	m.status.Upstream = cfg.CPAUpstreamURL
	m.status.Mode = collectorMode(valueOr(cfg.CollectorMode, m.base.CollectorMode))
	m.status.Transport = ""
	m.status.Queue = valueOr(cfg.Queue, m.base.Queue)
	m.status.LastError = ""

	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	go m.run(runCtx, cfg)
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.status.Collector = "stopped"
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Manager) setStatus(update func(*Status)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	update(&m.status)
}

func (m *Manager) run(ctx context.Context, cfg RuntimeConfig) {
	go m.spendLimitTicker(ctx)
	go m.alertTicker(ctx, cfg)

	mode := collectorMode(valueOr(cfg.CollectorMode, m.base.CollectorMode))

	if mode == "http" {
		m.runHTTP(ctx, cfg, mode)
		return
	}
	if mode == "auto" && m.runHTTP(ctx, cfg, mode) {
		return
	}
	m.runRESP(ctx, cfg)
}

func (m *Manager) runHTTP(ctx context.Context, cfg RuntimeConfig, mode string) bool {
	client := httpqueue.New(cfg.CPAUpstreamURL, cfg.ManagementKey)
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			return true
		}
		err := m.consumeHTTP(ctx, cfg, client)
		if ctx.Err() != nil {
			return true
		}
		if errors.Is(err, httpqueue.ErrUnsupported) && mode == "auto" {
			m.setStatus(func(status *Status) {
				status.Collector = "starting"
				status.Transport = "resp"
				status.LastError = ""
			})
			return false
		}
		if err != nil {
			m.markError("http", err)
			sleep(ctx, backoff)
			backoff = nextBackoff(backoff)
		}
	}
}

func (m *Manager) runRESP(ctx context.Context, cfg RuntimeConfig) {
	queue := valueOr(cfg.Queue, m.base.Queue)
	popSide := valueOr(cfg.PopSide, m.base.PopSide)
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			return
		}
		client, err := resp.Dial(cfg.CPAUpstreamURL, cfg.TLSSkipVerify)
		if err != nil {
			m.markError("connect", err)
			sleep(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		if err := client.Auth(cfg.ManagementKey); err != nil {
			_ = client.Close()
			m.markError("auth", err)
			sleep(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = time.Second
		m.setStatus(func(status *Status) {
			status.Collector = "running"
			status.Transport = "resp"
			status.LastError = ""
		})

		err = m.consumeRESP(ctx, cfg, client, queue, popSide)
		_ = client.Close()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			m.markError("consume", err)
			sleep(ctx, backoff)
			backoff = nextBackoff(backoff)
		}
	}
}

func (m *Manager) consumeHTTP(ctx context.Context, cfg RuntimeConfig, client *httpqueue.Client) error {
	ticker := time.NewTicker(m.pollInterval(cfg))
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		m.setStatus(func(status *Status) {
			status.Collector = "running"
			status.Transport = "http"
			status.LastError = ""
		})
		items, err := client.Pop(ctx, m.batchSize(cfg))
		if err != nil {
			return err
		}
		if len(items) == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				continue
			}
		}
		if err := m.processItems(ctx, cfg, items); err != nil {
			return err
		}
	}
}

func (m *Manager) consumeRESP(ctx context.Context, cfg RuntimeConfig, client *resp.Client, queue string, popSide string) error {
	ticker := time.NewTicker(m.pollInterval(cfg))
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		items, err := client.Pop(queue, popSide, m.batchSize(cfg))
		if err != nil {
			return err
		}
		if len(items) == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				continue
			}
		}
		if err := m.processItems(ctx, cfg, items); err != nil {
			return err
		}
	}
}

func (m *Manager) processItems(ctx context.Context, cfg RuntimeConfig, items []string) error {
	if len(items) == 0 {
		return nil
	}
	m.setStatus(func(status *Status) {
		status.LastConsumedAt = time.Now().UnixMilli()
	})
	events := make([]usage.Event, 0, len(items))
	for _, item := range items {
		event, err := usage.NormalizeRaw([]byte(item))
		if err != nil {
			_ = m.store.AddDeadLetter(ctx, item, err)
			m.setStatus(func(status *Status) {
				status.DeadLetters++
			})
			continue
		}
		events = append(events, event)
	}
	m.enrichAccountSnapshots(ctx, cfg, events)
	result, err := m.store.InsertEvents(ctx, events)
	if err != nil {
		return err
	}
	if result.Inserted > 0 || result.Skipped > 0 {
		m.setStatus(func(status *Status) {
			status.LastInsertedAt = time.Now().UnixMilli()
			status.TotalInserted += int64(result.Inserted)
			status.TotalSkipped += int64(result.Skipped)
		})
	}
	return nil
}

func (m *Manager) enrichAccountSnapshots(ctx context.Context, cfg RuntimeConfig, events []usage.Event) {
	if len(events) == 0 || m.snapshotResolver == nil {
		return
	}
	authIndices := make(map[string]struct{})
	for i := range events {
		if events[i].AccountSnapshot != "" || events[i].AuthIndex == "" {
			continue
		}
		authIndices[events[i].AuthIndex] = struct{}{}
	}
	if len(authIndices) == 0 {
		return
	}
	snapshots := m.snapshotResolver.lookup(ctx, cfg, authIndices)
	if len(snapshots) == 0 {
		return
	}
	for i := range events {
		if events[i].AuthIndex == "" || events[i].AccountSnapshot != "" {
			continue
		}
		snapshot, ok := snapshots[events[i].AuthIndex]
		if !ok {
			continue
		}
		events[i].AccountSnapshot = snapshot.Account
		events[i].AuthLabelSnapshot = snapshot.Label
		events[i].AuthFileSnapshot = snapshot.FileName
		events[i].AuthProviderSnapshot = snapshot.Provider
		events[i].AuthSnapshotAtMS = snapshot.CapturedAtMS
	}
}

func (m *Manager) markError(stage string, err error) {
	m.setStatus(func(status *Status) {
		status.Collector = "error"
		status.LastError = stage + ": " + err.Error()
	})
}

func sleep(ctx context.Context, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > 30*time.Second {
		return 30 * time.Second
	}
	return next
}

func valueOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func collectorMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "http", "resp":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "auto"
	}
}

func (m *Manager) batchSize(cfg RuntimeConfig) int {
	if cfg.BatchSize > 0 {
		return cfg.BatchSize
	}
	if m.base.BatchSize <= 0 {
		return 100
	}
	return m.base.BatchSize
}

func (m *Manager) pollInterval(cfg RuntimeConfig) time.Duration {
	if cfg.PollInterval > 0 {
		return cfg.PollInterval
	}
	if m.base.PollInterval <= 0 {
		return 500 * time.Millisecond
	}
	return m.base.PollInterval
}

// alertTicker periodically checks user daily spend against $ThresholdStep thresholds and
// pool quota exhaustion. Pool check runs at a separate, slower interval.
func (m *Manager) alertTicker(ctx context.Context, cfg RuntimeConfig) {
	if m.mailSender == nil && m.store == nil {
		return
	}

	// Config reload ticker (30s) checks for DB-side changes so HTTP API edits
	// take effect without restarting.
	const reloadInterval = 30 * time.Second
	reloadTicker := time.NewTicker(reloadInterval)
	defer reloadTicker.Stop()

	// Initial load from DB overrides startup defaults
	m.reloadAlertConfig()

	// Build initial tickers with current config
	rebuildTickers := func() (spendTicker, poolTicker *time.Ticker, spendInterval, poolInterval time.Duration) {
		ac := m.getAlertConfig()

		spendInterval = ac.CheckInterval
		if spendInterval <= 0 {
			spendInterval = 60 * time.Second
		}
		spendTicker = time.NewTicker(spendInterval)

		poolInterval = ac.PoolCheckInterval
		if poolInterval <= 0 {
			poolInterval = 5 * time.Minute
		}
		poolTicker = time.NewTicker(poolInterval)
		return
	}

	spendTk, poolTk, curSpendInt, curPoolInt := rebuildTickers()
	defer spendTk.Stop()
	defer poolTk.Stop()

	// Run both checks once immediately on startup
	if ac := m.getAlertConfig(); ac.Enabled && ac.ThresholdCents > 0 {
		if sender := m.getMailSender(); sender != nil {
			CheckUserSpendAlerts(m.store, sender, ac.ThresholdCents)
		}
	}
	if ac := m.getAlertConfig(); ac.PoolAlertEnabled {
		if sender := m.getMailSender(); sender != nil {
			checker := newPoolQuotaChecker(m.store, sender, cfg.CPAUpstreamURL, cfg.ManagementKey)
			checker.Check(ctx)
		}
	}

	// poolCheckRunning guards against overlapping pool quota checks
	var poolCheckRunning bool

	for {
		select {
		case <-ctx.Done():
			return

		case <-reloadTicker.C:
			m.reloadAlertConfig()
			newCfg := m.getAlertConfig()
			newSpendInt := newCfg.CheckInterval
			if newSpendInt <= 0 {
				newSpendInt = 60 * time.Second
			}
			newPoolInt := newCfg.PoolCheckInterval
			if newPoolInt <= 0 {
				newPoolInt = 5 * time.Minute
			}
			// Rebuild tickers only if interval changed
			if newSpendInt != curSpendInt {
				spendTk.Stop()
				spendTk = time.NewTicker(newSpendInt)
				curSpendInt = newSpendInt
			}
			if newPoolInt != curPoolInt {
				poolTk.Stop()
				poolTk = time.NewTicker(newPoolInt)
				curPoolInt = newPoolInt
			}

		case <-spendTk.C:
			ac := m.getAlertConfig()
			if !ac.Enabled || ac.ThresholdCents <= 0 {
				continue
			}
			sender := m.getMailSender()
			if sender == nil {
				continue
			}
			CheckUserSpendAlerts(m.store, sender, ac.ThresholdCents)

		case <-poolTk.C:
			ac := m.getAlertConfig()
			if !ac.PoolAlertEnabled {
				continue
			}
			if poolCheckRunning {
				log.Println("pool-quota: previous check still running, skipping this tick")
				continue
			}
			sender := m.getMailSender()
			if sender == nil {
				continue
			}
			poolCheckRunning = true
			checker := newPoolQuotaChecker(m.store, sender, cfg.CPAUpstreamURL, cfg.ManagementKey)
			go func() {
				defer func() { poolCheckRunning = false }()
				checker.Check(ctx)
			}()
		}
	}
}

// WithSpendLimitSync 串行化配置持久化和协调，确保成功保存后的外部状态只由该次最新配置决定。
func (m *Manager) WithSpendLimitSync(save func() error) error {
	m.spendLimitMu.Lock()
	defer m.spendLimitMu.Unlock()
	if err := save(); err != nil {
		return err
	}
	return m.reconcileSpendLimitsLocked()
}

// ReconcileSpendLimits 使用当前运行连接同步自动暂停状态，并与配置保存及定时兜底共享同一串行入口。
func (m *Manager) ReconcileSpendLimits() error {
	m.spendLimitMu.Lock()
	defer m.spendLimitMu.Unlock()
	return m.reconcileSpendLimitsLocked()
}

func (m *Manager) reconcileSpendLimitsLocked() error {
	m.mu.Lock()
	cfg := m.runtimeCfg
	m.mu.Unlock()
	if cfg.CPAUpstreamURL == "" {
		cfg.CPAUpstreamURL = m.base.CPAUpstreamURL
	}
	if cfg.ManagementKey == "" {
		cfg.ManagementKey = m.base.ManagementKey
	}
	return ReconcileSpendLimits(m.store, newPauseClient(cfg.CPAUpstreamURL, cfg.ManagementKey))
}

// spendLimitTicker 定期复用保存路径的协调逻辑，作为即时同步的兜底。
func nextSpendLimitRetryDelay(current time.Duration) time.Duration {
	if current <= 0 {
		return 30 * time.Second
	}
	next := current * 2
	if next > 5*time.Minute {
		return 5 * time.Minute
	}
	return next
}

func (m *Manager) spendLimitTicker(ctx context.Context) {
	retryDelay := time.Duration(0)
	reconcile := func() {
		if err := m.ReconcileSpendLimits(); err != nil {
			log.Printf("spend-limit: reconcile failed: %v", err)
			retryDelay = nextSpendLimitRetryDelay(retryDelay)
			return
		}
		retryDelay = 0
	}
	reconcile()

	for {
		delay := 30 * time.Second
		if retryDelay > 0 {
			delay = retryDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
			reconcile()
		}
	}
}
