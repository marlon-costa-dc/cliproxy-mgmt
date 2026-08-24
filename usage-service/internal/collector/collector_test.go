package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/config"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestManagerConsumesHTTPUsageQueue(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			if r.Header.Get("Authorization") != "Bearer management-key" {
				http.Error(w, "bad key", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[{"auth_index":"auth-1","account":"alice@example.com","label":"Alice","name":"alice.json","provider":"codex"}]}`))
			return
		}
		if r.URL.Path != "/v0/management/usage-queue" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer management-key" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&calls, 1) == 1 {
			_, _ = w.Write([]byte(`[{
				"timestamp": "2026-05-06T00:00:00Z",
				"model": "gpt-test",
				"endpoint": "POST /v1/chat/completions",
				"auth_index": "auth-1",
				"input_tokens": 10,
				"output_tokens": 5
			}]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(upstream.Close)

	db := newTestStore(t)
	cfg := testConfig(t, "auto")
	manager := NewManager(cfg, db, nil, AlertConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx, RuntimeConfig{
		CPAUpstreamURL: upstream.URL,
		ManagementKey:  "management-key",
	})

	waitFor(t, func() bool {
		events, _, err := db.Counts(context.Background())
		return err == nil && events == 1
	})

	status := manager.Status()
	if status.Transport != "http" {
		t.Fatalf("transport = %q, want http", status.Transport)
	}
	if status.TotalInserted != 1 {
		t.Fatalf("total inserted = %d, want 1", status.TotalInserted)
	}
	events, err := db.RecentEvents(context.Background(), 10)
	if err != nil {
		t.Fatalf("recent events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].AccountSnapshot != "alice@example.com" {
		t.Fatalf("account snapshot = %q", events[0].AccountSnapshot)
	}
	if events[0].AuthLabelSnapshot != "Alice" {
		t.Fatalf("auth label snapshot = %q", events[0].AuthLabelSnapshot)
	}
}

func TestManagerFallsBackToRESPWhenHTTPQueueUnsupported(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(upstream.Close)

	db := newTestStore(t)
	cfg := testConfig(t, "auto")
	manager := NewManager(cfg, db, nil, AlertConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx, RuntimeConfig{
		CPAUpstreamURL: upstream.URL,
		ManagementKey:  "management-key",
	})

	waitFor(t, func() bool {
		status := manager.Status()
		return status.Transport == "resp" && strings.Contains(status.LastError, "unsupported RESP prefix")
	})
}

func TestSpendLimitRetryDelayBacksOff(t *testing.T) {
	if got := nextSpendLimitRetryDelay(0); got != 30*time.Second {
		t.Fatalf("initial retry delay = %s, want 30s", got)
	}
	if got := nextSpendLimitRetryDelay(30 * time.Second); got != time.Minute {
		t.Fatalf("second retry delay = %s, want 1m", got)
	}
	if got := nextSpendLimitRetryDelay(5 * time.Minute); got != 5*time.Minute {
		t.Fatalf("retry delay cap = %s, want 5m", got)
	}
}

func TestPausedKeysRetriesTransientBadGateway(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/quota/paused" {
			http.NotFound(w, r)
			return
		}
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entries":[]}`))
	}))
	t.Cleanup(upstream.Close)

	if _, err := newPauseClient(upstream.URL, "management-key").PausedKeys(); err != nil {
		t.Fatalf("PausedKeys should recover from a transient 502: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("PausedKeys calls = %d, want 2", got)
	}
}

func TestPausedKeysIncludesRemoteErrorBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "quota backend is unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(upstream.Close)

	_, err := newPauseClient(upstream.URL, "management-key").PausedKeys()
	if err == nil || !strings.Contains(err.Error(), "quota backend is unavailable") {
		t.Fatalf("error = %v, want remote response body", err)
	}
}

func TestPausedKeysRetriesTransientInternalFailure(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, "temporary quota store failure", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entries":[]}`))
	}))
	t.Cleanup(upstream.Close)

	if _, err := newPauseClient(upstream.URL, "management-key").PausedKeys(); err != nil {
		t.Fatalf("PausedKeys should recover from a transient 500: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("PausedKeys calls = %d, want 2", got)
	}
}

func TestPausedKeysTreatsZeroExpiryAsPermanent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entries":[{"key_hash":"manual-key","reason":"manual pause","expires_at":"0001-01-01T00:00:00Z"}]}`))
	}))
	t.Cleanup(upstream.Close)

	entries, err := newPauseClient(upstream.URL, "management-key").PausedKeys()
	if err != nil {
		t.Fatalf("PausedKeys failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Expired {
		t.Fatalf("permanent pause = %#v, want one non-expired entry", entries)
	}
}

func TestCheckAndEnforceLimitsUsesDefaultAndOverrides(t *testing.T) {
	paused := make(map[string]bool)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/quota/config" {
			t.Fatal("CheckAndEnforceLimits must not fetch quota config from CPA")
		}
		if r.URL.Path == "/v0/management/quota/paused" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entries":[]}`))
			return
		}
		if r.URL.Path != "/v0/management/quota/pause" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer management-key" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		var body struct {
			KeyHash string `json:"key_hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode pause body: %v", err)
		}
		paused[body.KeyHash] = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(upstream.Close)

	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 30, Cache: 5},
	}); err != nil {
		t.Fatalf("UpsertSyncedModelPrices failed: %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{
		Enabled: true,
		Default: store.SpendLimit{DailyCents: 1000, WeeklyCents: 1000},
		Overrides: []store.SpendLimitEntry{
			{ApplyTo: "api-key", ApplyValue: "hash-b", DailyCents: 100, WeeklyCents: 1000},
		},
	}); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}
	now := time.Now()
	if _, err := db.InsertEvents(ctx, []usage.Event{
		spendLimitEvent("event-a", "hash-a", now),
		spendLimitEvent("event-b", "hash-b", now),
	}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}

	CheckAndEnforceLimits(db, newPauseClient(upstream.URL, "management-key"))

	if paused["hash-a"] {
		t.Fatal("hash-a should use default limit and stay active")
	}
	if !paused["hash-b"] {
		t.Fatal("hash-b should use override limit and be paused")
	}
}

func TestCheckAndEnforceLimitsMatchesShortOverrideToFullUsageHash(t *testing.T) {
	paused := make(map[string]bool)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/quota/paused" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entries":[]}`))
			return
		}
		if r.URL.Path != "/v0/management/quota/pause" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			KeyHash string `json:"key_hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode pause body: %v", err)
		}
		paused[body.KeyHash] = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	fullHash := strings.Repeat("b", 64)
	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 30, Cache: 5},
	}); err != nil {
		t.Fatalf("UpsertSyncedModelPrices failed: %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{
		Enabled: true,
		Overrides: []store.SpendLimitEntry{{
			ApplyTo:     "api-key",
			ApplyValue:  fullHash[:8],
			DailyCents:  1,
			WeeklyCents: 1000,
		}},
	}); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{spendLimitEvent("event-full-hash", fullHash, time.Now())}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}

	CheckAndEnforceLimits(db, newPauseClient(upstream.URL, "management-key"))

	if !paused[fullHash[:8]] {
		t.Fatalf("short override should pause full usage hash as %q", fullHash[:8])
	}
}

func TestCheckAndEnforceLimitsZeroDailyOverrideMeansNoDailyLimit(t *testing.T) {
	paused := make(map[string]bool)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/quota/paused" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entries":[]}`))
			return
		}
		if r.URL.Path != "/v0/management/quota/pause" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			KeyHash string `json:"key_hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode pause body: %v", err)
		}
		paused[body.KeyHash] = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(upstream.Close)

	fullHash := strings.Repeat("a", 64)
	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 30, Cache: 5},
	}); err != nil {
		t.Fatalf("UpsertSyncedModelPrices failed: %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{
		Enabled: true,
		Overrides: []store.SpendLimitEntry{{
			ApplyTo:     "api-key",
			ApplyValue:  fullHash,
			DailyCents:  0,
			WeeklyCents: 0,
		}},
	}); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{spendLimitEvent("event-zero", fullHash, time.Now())}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}

	CheckAndEnforceLimits(db, newPauseClient(upstream.URL, "management-key"))

	if paused[fullHash[:8]] {
		t.Fatalf("zero daily/weekly override should not pause key %q", fullHash[:8])
	}
}

func TestCheckAndEnforceLimitsZeroDailyOverrideStillEnforcesWeekly(t *testing.T) {
	paused := make(map[string]bool)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/quota/paused" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entries":[]}`))
			return
		}
		if r.URL.Path != "/v0/management/quota/pause" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			KeyHash string `json:"key_hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode pause body: %v", err)
		}
		paused[body.KeyHash] = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(upstream.Close)

	fullHash := strings.Repeat("b", 64)
	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 30, Cache: 5},
	}); err != nil {
		t.Fatalf("UpsertSyncedModelPrices failed: %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{
		Enabled: true,
		Overrides: []store.SpendLimitEntry{{
			ApplyTo:     "api-key",
			ApplyValue:  fullHash,
			DailyCents:  0,
			WeeklyCents: 100,
		}},
	}); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{
		spendLimitEvent("event-weekly", fullHash, time.Now()),
		spendLimitEvent("event-weekly-2", fullHash, time.Now()),
	}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}

	CheckAndEnforceLimits(db, newPauseClient(upstream.URL, "management-key"))

	if !paused[fullHash[:8]] {
		t.Fatalf("weekly override should pause key %q", fullHash[:8])
	}
}

func TestCheckAndEnforceLimitsZeroDailyDefaultStillEnforcesWeekly(t *testing.T) {
	paused := make(map[string]bool)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/quota/paused" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entries":[]}`))
			return
		}
		if r.URL.Path != "/v0/management/quota/pause" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			KeyHash string `json:"key_hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode pause body: %v", err)
		}
		paused[body.KeyHash] = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(upstream.Close)

	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 30, Cache: 5},
	}); err != nil {
		t.Fatalf("UpsertSyncedModelPrices failed: %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{
		Enabled: true,
		Default: store.SpendLimit{DailyCents: 0, WeeklyCents: 100},
	}); err != nil {
		t.Fatalf("SaveSpendLimitConfig failed: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{
		spendLimitEvent("event-default-weekly", "hash-default", time.Now()),
		spendLimitEvent("event-default-weekly-2", "hash-default", time.Now()),
	}); err != nil {
		t.Fatalf("InsertEvents failed: %v", err)
	}

	CheckAndEnforceLimits(db, newPauseClient(upstream.URL, "management-key"))

	if !paused["hash-default"] {
		t.Fatal("weekly default should pause key when daily default is unlimited")
	}
}

func TestReconcileSpendLimitsResumesOnlyAutomaticPauses(t *testing.T) {
	var resumed []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/quota/paused":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entries":[{"key_hash":"automatic","reason":"spend_limit_exceeded"},{"key_hash":"manual","reason":"manual pause"}]}`))
		case "/v0/management/quota/resume":
			var body struct {
				KeyHash        string `json:"key_hash"`
				ExpectedReason string `json:"expected_reason"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode resume body: %v", err)
			}
			if body.ExpectedReason != spendLimitExceededReason {
				t.Fatalf("expected_reason = %q, want %q", body.ExpectedReason, spendLimitExceededReason)
			}
			resumed = append(resumed, body.KeyHash)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	db := newTestStore(t)
	if err := db.SaveSpendLimitConfig(context.Background(), store.SpendLimitConfig{Enabled: false}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := ReconcileSpendLimits(db, newPauseClient(upstream.URL, "management-key")); err != nil {
		t.Fatalf("ReconcileSpendLimits failed: %v", err)
	}
	if len(resumed) != 1 || resumed[0] != "automatic" {
		t.Fatalf("resumed = %#v, want only automatic pause", resumed)
	}
}

func TestReconcileSpendLimitsRestoresAutomaticPausesWithoutCurrentSpend(t *testing.T) {
	var resumed []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/quota/paused":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entries":[{"key_hash":"expired-spend","reason":"spend_limit_exceeded"}]}`))
		case "/v0/management/quota/resume":
			var body struct {
				KeyHash string `json:"key_hash"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode resume body: %v", err)
			}
			resumed = append(resumed, body.KeyHash)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	db := newTestStore(t)
	if err := db.SaveSpendLimitConfig(context.Background(), store.SpendLimitConfig{Enabled: false}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := ReconcileSpendLimits(db, newPauseClient(upstream.URL, "management-key")); err != nil {
		t.Fatalf("ReconcileSpendLimits failed: %v", err)
	}
	if len(resumed) != 1 || resumed[0] != "expired-spend" {
		t.Fatalf("resumed = %#v, want automatic pause without current spend", resumed)
	}
}

func TestManagerWithSpendLimitSyncSerializesReconciliation(t *testing.T) {
	var (
		mu           sync.Mutex
		automatic    bool
		pauseStarted = make(chan struct{})
		releasePause = make(chan struct{})
		pauseOnce    sync.Once
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v0/management/quota/paused":
			w.Header().Set("Content-Type", "application/json")
			if automatic {
				_, _ = w.Write([]byte(`{"entries":[{"key_hash":"sync-key","reason":"spend_limit_exceeded"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"entries":[]}`))
		case "/v0/management/quota/pause":
			pauseOnce.Do(func() { close(pauseStarted) })
			mu.Unlock()
			<-releasePause
			mu.Lock()
			automatic = true
			w.WriteHeader(http.StatusOK)
		case "/v0/management/quota/resume":
			automatic = false
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{"gpt-4": {Prompt: 10, Completion: 30, Cache: 5}}); err != nil {
		t.Fatalf("save prices: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{spendLimitEvent("sync", "sync-key", time.Now())}); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	cfg := config.Config{CPAUpstreamURL: upstream.URL, ManagementKey: "management-key"}
	manager := NewManager(cfg, db, nil, AlertConfig{})

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- manager.WithSpendLimitSync(func() error {
			return db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{Enabled: true, Default: store.SpendLimit{DailyCents: 1}})
		})
	}()
	<-pauseStarted

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- manager.WithSpendLimitSync(func() error {
			return db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{Enabled: false})
		})
	}()
	close(releasePause)
	if err := <-firstDone; err != nil {
		t.Fatalf("restrictive sync failed: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("relaxed sync failed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if automatic {
		t.Fatal("latest relaxed configuration must leave the remote key resumed")
	}
}

func TestReconcileSpendLimitsReturnsRemoteFailures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/quota/paused" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entries":[]}`))
			return
		}
		if r.URL.Path == "/v0/management/quota/pause" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(upstream.Close)

	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{"gpt-4": {Prompt: 10, Completion: 30, Cache: 5}}); err != nil {
		t.Fatalf("save prices: %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{Enabled: true, Default: store.SpendLimit{DailyCents: 1}}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{spendLimitEvent("remote-failure", "key", time.Now())}); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	if err := ReconcileSpendLimits(db, newPauseClient(upstream.URL, "management-key")); err == nil {
		t.Fatal("expected remote pause failure")
	}
}

func TestReconcileSpendLimitsReturnsPausedListAndResumeFailures(t *testing.T) {
	tests := []struct {
		name       string
		pausedCode int
		resumeCode int
	}{
		{name: "paused list", pausedCode: http.StatusServiceUnavailable},
		{name: "resume", resumeCode: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v0/management/quota/paused":
					if tt.pausedCode != 0 {
						http.Error(w, "unavailable", tt.pausedCode)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"entries":[{"key_hash":"automatic","reason":"spend_limit_exceeded"}]}`))
				case "/v0/management/quota/resume":
					http.Error(w, "unavailable", tt.resumeCode)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(upstream.Close)

			db := newTestStore(t)
			if err := db.SaveSpendLimitConfig(context.Background(), store.SpendLimitConfig{Enabled: false}); err != nil {
				t.Fatalf("save config: %v", err)
			}
			if err := ReconcileSpendLimits(db, newPauseClient(upstream.URL, "management-key")); err == nil {
				t.Fatal("expected remote reconciliation failure")
			}
		})
	}
}

func TestSpendLimitExceededUsesShanghaiPeriodBoundary(t *testing.T) {
	// 该时刻在 UTC 仍是周日，但在上海已是周一，验证不受进程本地时区影响。
	now := time.Date(2026, time.July, 26, 16, 0, 0, 0, time.UTC)

	exceeded, expiresAt := spendLimitExceeded(store.KeySpend{TodayCents: 1}, store.SpendLimit{DailyCents: 1}, now)
	if !exceeded || expiresAt.Location() != shanghaiLocation || expiresAt.Day() != 28 {
		t.Fatalf("daily expiry = %s, want 2026-07-28T00:00:00+08:00", expiresAt)
	}

	exceeded, expiresAt = spendLimitExceeded(store.KeySpend{WeekCents: 1}, store.SpendLimit{WeeklyCents: 1}, now)
	if !exceeded || expiresAt.Location() != shanghaiLocation || expiresAt.Day() != 3 || expiresAt.Month() != time.August {
		t.Fatalf("weekly expiry = %s, want 2026-08-03T00:00:00+08:00", expiresAt)
	}
}

func spendLimitEvent(hash, keyHash string, at time.Time) usage.Event {
	return usage.Event{
		EventHash:   hash,
		TimestampMS: at.UnixMilli(),
		Timestamp:   at.UTC().Format(time.RFC3339),
		Model:       "gpt-4",
		APIKeyHash:  keyHash,
		InputTokens: 100000,
		TotalTokens: 100000,
		CreatedAtMS: at.UnixMilli(),
	}
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func testConfig(t *testing.T, mode string) config.Config {
	t.Helper()
	return config.Config{
		DBPath:        filepath.Join(t.TempDir(), "usage.sqlite"),
		CollectorMode: mode,
		Queue:         "usage",
		PopSide:       "right",
		BatchSize:     10,
		PollInterval:  10 * time.Millisecond,
	}
}

func TestQueryAccountQuotaSupportsCamelCaseAndWindowFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/api-call":
			if r.Header.Get("Authorization") != "Bearer management-key" {
				http.Error(w, "bad key", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status_code":200,
				"body":"{\"rateLimit\":{\"primaryWindow\":{\"usedPercent\":100,\"limitWindowSeconds\":604800,\"resetAt\":1751904000},\"secondaryWindow\":{\"usedPercent\":100,\"limitWindowSeconds\":18000,\"resetAt\":1751328000}}}"
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	checker := newPoolQuotaChecker(nil, nil, upstream.URL, "management-key")
	result := checker.queryAccountQuota(context.Background(), "auth-1", "account-1")

	if result.err != "" {
		t.Fatalf("queryAccountQuota returned error: %s", result.err)
	}
	if result.fiveHourUsed != 100 {
		t.Fatalf("fiveHourUsed = %v, want 100", result.fiveHourUsed)
	}
	if result.weeklyUsed != 100 {
		t.Fatalf("weeklyUsed = %v, want 100", result.weeklyUsed)
	}
	if result.fiveHourReset == "" {
		t.Fatal("fiveHourReset should not be empty")
	}
	if result.weeklyReset == "" {
		t.Fatal("weeklyReset should not be empty")
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before deadline")
}
