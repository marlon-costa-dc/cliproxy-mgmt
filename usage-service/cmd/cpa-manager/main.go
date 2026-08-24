package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/collector"
	"github.com/seakee/cpa-manager/usage-service/internal/config"
	"github.com/seakee/cpa-manager/usage-service/internal/httpapi"
	"github.com/seakee/cpa-manager/usage-service/internal/mail"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	// Build alert config: env defaults overridden by DB-persisted config
	alertCfg := buildAlertConfig(db, cfg)
	sender := mail.NewSender(mail.Config{
		Host:     alertCfg.SMTPHost,
		Port:     alertCfg.SMTPPort,
		Username: alertCfg.SMTPUsername,
		Password: alertCfg.SMTPPassword,
		From:     alertCfg.SMTPFrom,
		FromName: alertCfg.SMTPFromName,
	})
	manager := collector.NewManager(cfg, db, sender, collector.AlertConfig{
		Enabled:           alertCfg.AlertEnabled,
		ThresholdCents:    alertCfg.ThresholdCents,
		CheckInterval:     time.Duration(alertCfg.CheckIntervalMS) * time.Millisecond,
		PoolAlertEnabled:  alertCfg.PoolCheckEnabled,
		PoolCheckInterval: time.Duration(alertCfg.PoolCheckInterval) * time.Minute,
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.CPAUpstreamURL != "" && cfg.ManagementKey != "" {
		manager.Start(ctx, collector.RuntimeConfig{
			CPAUpstreamURL: cfg.CPAUpstreamURL,
			ManagementKey:  cfg.ManagementKey,
			CollectorMode:  cfg.CollectorMode,
			Queue:          cfg.Queue,
			PopSide:        cfg.PopSide,
			BatchSize:      cfg.BatchSize,
			PollInterval:   cfg.PollInterval,
			TLSSkipVerify:  cfg.TLSSkipVerify,
		})
	} else if managerCfg, ok, err := db.LoadManagerConfig(ctx); err == nil && ok &&
		managerCfg.CPAConnection.CPABaseURL != "" && managerCfg.CPAConnection.ManagementKey != "" {
		if managerCollectorEnabled(managerCfg) {
			manager.Start(ctx, runtimeConfigFromManagerConfig(managerCfg, cfg))
		}
	} else if setup, ok, err := db.LoadSetup(ctx); err == nil && ok {
		manager.Start(ctx, collector.RuntimeConfig{
			CPAUpstreamURL: setup.CPAUpstreamURL,
			ManagementKey:  setup.ManagementKey,
			CollectorMode:  cfg.CollectorMode,
			Queue:          setup.Queue,
			PopSide:        setup.PopSide,
			BatchSize:      cfg.BatchSize,
			PollInterval:   cfg.PollInterval,
			TLSSkipVerify:  cfg.TLSSkipVerify,
		})
	} else if err != nil {
		log.Printf("load setup: %v", err)
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.New(cfg, db, manager).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("cpa-manager listening on %s", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	// Periodic cleanup of old usage events
	if cfg.RetentionDays > 0 {
		cleanupInterval := 24 * time.Hour
		log.Printf("cleanup: purging usage_events older than %d days", cfg.RetentionDays)
		go func() {
			ticker := time.NewTicker(cleanupInterval)
			defer ticker.Stop()
			for {
				cleanupCutoff := time.Now().AddDate(0, 0, -cfg.RetentionDays).UnixMilli()
				if n, err := db.PurgeEventsBefore(ctx, cleanupCutoff); err != nil {
					log.Printf("cleanup: purge error: %v", err)
				} else if n > 0 {
					log.Printf("cleanup: purged %d old events", n)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	manager.Stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func runtimeConfigFromManagerConfig(managerCfg store.ManagerConfig, base config.Config) collector.RuntimeConfig {
	pollInterval := time.Duration(managerCfg.Collector.PollIntervalMS) * time.Millisecond
	if pollInterval <= 0 {
		pollInterval = base.PollInterval
	}
	batchSize := managerCfg.Collector.BatchSize
	if batchSize <= 0 {
		batchSize = base.BatchSize
	}
	return collector.RuntimeConfig{
		CPAUpstreamURL: managerCfg.CPAConnection.CPABaseURL,
		ManagementKey:  managerCfg.CPAConnection.ManagementKey,
		CollectorMode:  valueOr(managerCfg.Collector.CollectorMode, base.CollectorMode),
		Queue:          valueOr(managerCfg.Collector.Queue, base.Queue),
		PopSide:        valueOr(managerCfg.Collector.PopSide, base.PopSide),
		BatchSize:      batchSize,
		PollInterval:   pollInterval,
		TLSSkipVerify:  managerCfg.Collector.TLSSkipVerify,
	}
}

func valueOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func managerCollectorEnabled(managerCfg store.ManagerConfig) bool {
	return managerCfg.Collector.Enabled == nil || *managerCfg.Collector.Enabled
}

// buildAlertConfig merges env-based config with DB-persisted alert config.
// DB values take precedence when present.
func buildAlertConfig(s *store.Store, cfg config.Config) store.AlertConfigStored {
	result := store.AlertConfigStored{
		SMTPHost:          cfg.SMTPHost,
		SMTPPort:          cfg.SMTPPort,
		SMTPUsername:      cfg.SMTPUsername,
		SMTPPassword:      cfg.SMTPPassword,
		SMTPFrom:          cfg.SMTPFrom,
		SMTPFromName:      cfg.SMTPFromName,
		AlertEnabled:      cfg.AlertEnabled,
		ThresholdCents:    cfg.AlertThresholdCents,
		CheckIntervalMS:   int(cfg.AlertCheckInterval / time.Millisecond),
		PoolCheckEnabled:  false,
		PoolCheckInterval: 5,
	}

	dbCfg, ok, err := s.LoadAlertConfigWithPassword(context.Background())
	if err != nil {
		log.Printf("alert: load DB config failed: %v, using env defaults", err)
		return result
	}
	if !ok {
		return result
	}

	// DB values override env defaults (non-zero fields only for optional fields)
	if dbCfg.SMTPHost != "" {
		result.SMTPHost = dbCfg.SMTPHost
	}
	if dbCfg.SMTPPort > 0 {
		result.SMTPPort = dbCfg.SMTPPort
	}
	if dbCfg.SMTPUsername != "" {
		result.SMTPUsername = dbCfg.SMTPUsername
	}
	if dbCfg.SMTPPassword != "" {
		result.SMTPPassword = dbCfg.SMTPPassword
	}
	if dbCfg.SMTPFrom != "" {
		result.SMTPFrom = dbCfg.SMTPFrom
	}
	if dbCfg.SMTPFromName != "" {
		result.SMTPFromName = dbCfg.SMTPFromName
	}
	if dbCfg.ThresholdCents > 0 {
		result.ThresholdCents = dbCfg.ThresholdCents
	}
	if dbCfg.CheckIntervalMS > 0 {
		result.CheckIntervalMS = dbCfg.CheckIntervalMS
	}
	if dbCfg.PoolCheckInterval > 0 {
		result.PoolCheckInterval = dbCfg.PoolCheckInterval
	}

	// These are DB-only — always use DB value when present
	result.AlertEnabled = dbCfg.AlertEnabled
	result.PoolCheckEnabled = dbCfg.PoolCheckEnabled

	return result
}
