package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
)

// pauseClient calls CLIProxyAPI's /v0/management/quota/pause endpoint.
type pauseClient struct {
	baseURL string
	mgmtKey string
	client  *http.Client
}

func newPauseClient(baseURL, mgmtKey string) *pauseClient {
	return &pauseClient{
		baseURL: baseURL,
		mgmtKey: mgmtKey,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func normalizePauseKeyHash(keyHash string) string {
	keyHash = strings.ToLower(strings.TrimSpace(keyHash))
	if len(keyHash) == 64 {
		return keyHash[:8]
	}
	return keyHash
}

const spendLimitExceededReason = "spend_limit_exceeded"

// 限额消费窗口按上海时区统计，到期时间必须使用相同时区避免跨日偏差。
var shanghaiLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return location
}()

type pausedKey struct {
	KeyHash   string `json:"key_hash"`
	Reason    string `json:"reason"`
	ExpiresAt time.Time
	Expired   bool `json:"-"`
}

type downgradedKey struct {
	KeyHash       string `json:"key_hash"`
	Reason        string `json:"reason"`
	FallbackModel string `json:"fallback_model"`
	ExpiresAt     string `json:"expires_at"`
	ExpiresAtTime time.Time
	Expired       bool `json:"-"`
}

type automaticQuotaState struct {
	Action        string
	KeyHash       string
	Reason        string
	FallbackModel string
	ExpiresAt     time.Time
}

type quotaHTTPError struct {
	method string
	path   string
	status int
	body   string
}

func (e *quotaHTTPError) Error() string {
	if e.body != "" {
		return fmt.Sprintf("quota request %s %s returned status %d: %s", e.method, e.path, e.status, e.body)
	}
	return fmt.Sprintf("quota request %s %s returned status %d", e.method, e.path, e.status)
}

func isRetryableQuotaError(err error) bool {
	var quotaErr *quotaHTTPError
	if !errors.As(err, &quotaErr) {
		return false
	}
	if quotaErr.status >= http.StatusInternalServerError {
		// Management quota reads and writes are idempotent from the reconciler's
		// perspective. Retry all upstream 5xx responses; validation/client errors
		// remain visible and are not retried as if they were transport failures.
		return true
	}
	switch quotaErr.status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

func (c *pauseClient) PauseKey(keyHash, reason string, expiresAt time.Time) error {
	expiresIn := int64(time.Until(expiresAt).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}
	return c.doJSON(http.MethodPost, "/v0/management/quota/pause", map[string]any{
		"key_hash":           normalizePauseKeyHash(keyHash),
		"reason":             reason,
		"expires_in_seconds": expiresIn,
	}, nil)
}

func (c *pauseClient) ResumeKey(keyHash, expectedReason string) error {
	return c.doJSON(http.MethodPost, "/v0/management/quota/resume", map[string]any{
		"key_hash":        normalizePauseKeyHash(keyHash),
		"expected_reason": expectedReason,
	}, nil)
}

func (c *pauseClient) PausedKeys() ([]pausedKey, error) {
	var result struct {
		Entries []struct {
			KeyHash   string `json:"key_hash"`
			Reason    string `json:"reason"`
			ExpiresAt string `json:"expires_at"`
		} `json:"entries"`
	}
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		result.Entries = nil
		err = c.doJSON(http.MethodGet, "/v0/management/quota/paused", nil, &result)
		if err == nil {
			break
		}
		if attempt == 0 && isRetryableQuotaError(err) {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	entries := make([]pausedKey, 0, len(result.Entries))
	for _, entry := range result.Entries {
		paused := pausedKey{KeyHash: entry.KeyHash, Reason: entry.Reason}
		if entry.ExpiresAt != "" {
			if expiresAt, err := time.Parse(time.RFC3339, entry.ExpiresAt); err == nil && !expiresAt.IsZero() {
				paused.ExpiresAt = expiresAt
				paused.Expired = !expiresAt.After(time.Now())
			}
		}
		entries = append(entries, paused)
	}
	return entries, nil
}

func (c *pauseClient) DowngradeKey(keyHash, reason, fallbackModel string, expiresAt time.Time) error {
	expiresIn := int64(time.Until(expiresAt).Seconds())
	if expiresAt.IsZero() || expiresIn < 0 {
		expiresIn = 0
	}
	return c.doJSON(http.MethodPost, "/v0/management/quota/downgrade", map[string]any{
		"key_hash":           normalizePauseKeyHash(keyHash),
		"reason":             reason,
		"fallback_model":     strings.TrimSpace(fallbackModel),
		"expires_in_seconds": expiresIn,
	}, nil)
}

func (c *pauseClient) ResumeDowngradeKey(keyHash, expectedReason string) error {
	return c.doJSON(http.MethodPost, "/v0/management/quota/downgrade/resume", map[string]any{
		"key_hash":        normalizePauseKeyHash(keyHash),
		"expected_reason": expectedReason,
	}, nil)
}

func (c *pauseClient) DowngradedKeys() ([]downgradedKey, error) {
	var result struct {
		Entries []downgradedKey `json:"entries"`
	}
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		result.Entries = nil
		err = c.doJSON(http.MethodGet, "/v0/management/quota/downgraded", nil, &result)
		if err == nil {
			break
		}
		if attempt == 0 && isRetryableQuotaError(err) {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	for idx := range result.Entries {
		entry := &result.Entries[idx]
		if entry.ExpiresAt == "" {
			continue
		}
		if expiresAt, parseErr := time.Parse(time.RFC3339, entry.ExpiresAt); parseErr == nil && !expiresAt.IsZero() {
			entry.ExpiresAtTime = expiresAt
			entry.Expired = !expiresAt.After(time.Now())
		}
	}
	return result.Entries, nil
}

func (c *pauseClient) ValidateFallbackModel(fallbackModel string) error {
	return c.doJSON(http.MethodPost, "/v0/management/quota/validate-model", map[string]any{
		"fallback_model": strings.TrimSpace(fallbackModel),
	}, nil)
}

// ValidateFallbackModel checks a fallback model through CPA before it is persisted.
func ValidateFallbackModel(baseURL, managementKey, fallbackModel string) error {
	return newPauseClient(baseURL, managementKey).ValidateFallbackModel(fallbackModel)
}

// QuotaHTTPStatus returns the upstream HTTP status when err came from CPA's
// quota management API.
func QuotaHTTPStatus(err error) (int, bool) {
	var quotaErr *quotaHTTPError
	if !errors.As(err, &quotaErr) {
		return 0, false
	}
	return quotaErr.status, true
}

// doJSON 统一处理已认证的 CLIProxyAPI 限额管理请求，并将非 2xx 视为同步失败。
func (c *pauseClient) doJSON(method, path string, body any, result any) error {
	if c == nil || c.baseURL == "" || c.mgmtKey == "" {
		return fmt.Errorf("pause client not configured")
	}
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.baseURL, "/")+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("quota request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return &quotaHTTPError{
			method: method,
			path:   path,
			status: resp.StatusCode,
			body:   strings.TrimSpace(string(body)),
		}
	}
	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode quota response: %w", err)
		}
	}
	return nil
}

// ReconcileSpendLimits coordinates the independent automatic pause and
// downgrade policies for the current spend window.
func ReconcileSpendLimits(s *store.Store, client *pauseClient) error {
	ctx := context.Background()
	pauseCfg, pauseConfigured, err := s.LoadPauseSpendLimitConfig(ctx)
	if err != nil {
		return fmt.Errorf("load pause config: %w", err)
	}
	downgradeCfg, downgradeConfigured, err := s.LoadDowngradeSpendLimitConfig(ctx)
	if err != nil {
		return fmt.Errorf("load downgrade config: %w", err)
	}
	if !pauseConfigured && !downgradeConfigured {
		hasSpend, err := s.HasCurrentKeySpend(ctx)
		if err != nil {
			return fmt.Errorf("check key spend: %w", err)
		}
		if !hasSpend {
			return nil
		}
	}

	paused, err := client.PausedKeys()
	if err != nil {
		return fmt.Errorf("list paused keys: %w", err)
	}
	downgraded, err := client.DowngradedKeys()
	if err != nil {
		// Older CPA versions do not expose downgrade endpoints. This is safe
		// only while no independent downgrade policy is configured.
		if (downgradeConfigured && downgradeCfg.Enabled) || !isNotFoundQuotaError(err) {
			return fmt.Errorf("list downgraded keys: %w", err)
		}
		downgraded = nil
	}

	pausedAutomatic := make(map[string]pausedKey, len(paused))
	for _, entry := range paused {
		if entry.Reason == spendLimitExceededReason && !entry.Expired {
			pausedAutomatic[normalizePauseKeyHash(entry.KeyHash)] = entry
		}
	}
	downgradedAutomatic := make(map[string]downgradedKey, len(downgraded))
	for _, entry := range downgraded {
		if entry.Reason == spendLimitExceededReason && !entry.Expired {
			downgradedAutomatic[normalizePauseKeyHash(entry.KeyHash)] = entry
		}
	}

	keys, err := s.QueryKeySpend(ctx)
	if err != nil {
		return fmt.Errorf("query key spend: %w", err)
	}
	keysByHash := make(map[string]store.KeySpend, len(keys)+len(pausedAutomatic)+len(downgradedAutomatic))
	for _, key := range keys {
		if key.KeyHash != "" {
			keysByHash[normalizePauseKeyHash(key.KeyHash)] = key
		}
	}
	for keyHash := range pausedAutomatic {
		if _, exists := keysByHash[keyHash]; !exists {
			keysByHash[keyHash] = store.KeySpend{KeyHash: keyHash}
		}
	}
	for keyHash := range downgradedAutomatic {
		if _, exists := keysByHash[keyHash]; !exists {
			keysByHash[keyHash] = store.KeySpend{KeyHash: keyHash}
		}
	}

	now := time.Now()
	for keyHash, key := range keysByHash {
		pauseExceeded := false
		var pauseExpiresAt time.Time
		if pauseConfigured && pauseCfg.Enabled {
			pauseExceeded, pauseExpiresAt = spendLimitExceeded(key, pauseCfg.LimitForKey(key.KeyHash), now)
		}
		if pauseExceeded {
			log.Printf("spend-limit: pausing key %s", keyHash)
		}
		if err := reconcileAutomaticPause(client, keyHash, pauseExceeded, pauseExpiresAt, pausedAutomatic[keyHash]); err != nil {
			return fmt.Errorf("reconcile pause for key %s: %w", keyHash, err)
		}

		downgradeExceeded := false
		var downgradeExpiresAt time.Time
		if downgradeConfigured && downgradeCfg.Enabled {
			downgradeExceeded, downgradeExpiresAt = spendLimitExceeded(key, downgradeCfg.LimitForKey(key.KeyHash), now)
		}
		if downgradeExceeded {
			log.Printf("spend-limit: downgrading key %s to %s", keyHash, downgradeCfg.EffectiveFallbackModel())
		}
		if err := reconcileAutomaticDowngrade(client, keyHash, downgradeExceeded, downgradeCfg.EffectiveFallbackModel(), downgradeExpiresAt, downgradedAutomatic[keyHash]); err != nil {
			return fmt.Errorf("reconcile downgrade for key %s: %w", keyHash, err)
		}
	}
	return nil
}

func isNotFoundQuotaError(err error) bool {
	var quotaErr *quotaHTTPError
	return errors.As(err, &quotaErr) && quotaErr.status == http.StatusNotFound
}

func reconcileAutomaticPause(client *pauseClient, keyHash string, exceeded bool, expiresAt time.Time, existing pausedKey) error {
	if exceeded {
		if err := client.PauseKey(keyHash, spendLimitExceededReason, expiresAt); err != nil {
			return fmt.Errorf("pause: %w", err)
		}
		return nil
	}
	if existing.KeyHash == "" {
		return nil
	}
	if err := client.ResumeKey(keyHash, spendLimitExceededReason); err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	return nil
}

func reconcileAutomaticDowngrade(client *pauseClient, keyHash string, exceeded bool, fallbackModel string, expiresAt time.Time, existing downgradedKey) error {
	if exceeded {
		if err := client.DowngradeKey(keyHash, spendLimitExceededReason, fallbackModel, expiresAt); err != nil {
			return fmt.Errorf("downgrade: %w", err)
		}
		return nil
	}
	if existing.KeyHash == "" {
		return nil
	}
	if err := client.ResumeDowngradeKey(keyHash, spendLimitExceededReason); err != nil {
		return fmt.Errorf("resume downgrade: %w", err)
	}
	return nil
}

func spendLimitExceeded(key store.KeySpend, limit store.SpendLimit, now time.Time) (bool, time.Time) {
	now = now.In(shanghaiLocation)
	if limit.DailyCents > 0 && key.TodayCents >= limit.DailyCents {
		return true, time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, shanghaiLocation)
	}
	if limit.WeeklyCents > 0 && key.WeekCents >= limit.WeeklyCents {
		daysUntilMonday := (8 - int(now.Weekday())) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 7
		}
		return true, time.Date(now.Year(), now.Month(), now.Day()+daysUntilMonday, 0, 0, 0, 0, shanghaiLocation)
	}
	return false, time.Time{}
}

// CheckAndEnforceLimits 保持兼容调用；后台任务记录错误而不影响下一轮扫描。
func CheckAndEnforceLimits(s *store.Store, client *pauseClient) {
	if err := ReconcileSpendLimits(s, client); err != nil {
		log.Printf("spend-limit: reconcile failed: %v", err)
	}
}
