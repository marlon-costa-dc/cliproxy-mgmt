package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestReconcileAutomaticStateClearsPauseBeforeDowngrade(t *testing.T) {
	calls := make([]string, 0, 3)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/quota/paused":
			_, _ = w.Write([]byte(`{"entries":[{"key_hash":"key-a","reason":"spend_limit_exceeded"}]}`))
		case "/v0/management/quota/downgraded":
			_, _ = w.Write([]byte(`{"entries":[]}`))
		case "/v0/management/quota/resume":
			calls = append(calls, "resume-pause")
			w.WriteHeader(http.StatusOK)
		case "/v0/management/quota/downgrade":
			calls = append(calls, "downgrade")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{"gpt-4": {Prompt: 10, Completion: 10}}); err != nil {
		t.Fatalf("UpsertSyncedModelPrices(): %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{
		Enabled:        true,
		Default:        store.SpendLimit{DailyCents: 1},
		ExceededAction: store.ExceededActionDowngrade,
		FallbackModel:  store.DefaultFallbackModel,
	}); err != nil {
		t.Fatalf("SaveSpendLimitConfig(): %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{spendLimitEvent("switch-event", "key-a", time.Now())}); err != nil {
		t.Fatalf("InsertEvents(): %v", err)
	}

	if err := ReconcileSpendLimits(db, newPauseClient(upstream.URL, "management-key")); err != nil {
		t.Fatalf("ReconcileSpendLimits(): %v", err)
	}
	if len(calls) != 2 || calls[0] != "resume-pause" || calls[1] != "downgrade" {
		t.Fatalf("transition calls = %#v, want [resume-pause downgrade]", calls)
	}
}

func TestReconcileIndependentDowngradeDoesNotCompensatePause(t *testing.T) {
	pauseWrites := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/quota/paused":
			_, _ = w.Write([]byte(`{"entries":[{"key_hash":"key-a","reason":"spend_limit_exceeded"}]}`))
		case "/v0/management/quota/downgraded":
			_, _ = w.Write([]byte(`{"entries":[]}`))
		case "/v0/management/quota/resume":
			w.WriteHeader(http.StatusOK)
		case "/v0/management/quota/downgrade":
			w.WriteHeader(http.StatusBadGateway)
		case "/v0/management/quota/pause":
			pauseWrites++
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{"gpt-4": {Prompt: 10, Completion: 10}}); err != nil {
		t.Fatalf("UpsertSyncedModelPrices(): %v", err)
	}
	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{Enabled: true, Default: store.SpendLimit{DailyCents: 1}, ExceededAction: store.ExceededActionDowngrade}); err != nil {
		t.Fatalf("SaveSpendLimitConfig(): %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{spendLimitEvent("restore-event", "key-a", time.Now())}); err != nil {
		t.Fatalf("InsertEvents(): %v", err)
	}

	if err := ReconcileSpendLimits(db, newPauseClient(upstream.URL, "management-key")); err == nil {
		t.Fatal("ReconcileSpendLimits() error = nil, want downgrade failure")
	}
	if pauseWrites != 0 {
		t.Fatalf("pause compensation writes = %d, want 0", pauseWrites)
	}
}
