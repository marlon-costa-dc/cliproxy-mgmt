package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestReconcileSpendLimitsDowngradesAndResumes(t *testing.T) {
	var downgradeCalls []map[string]any
	var resumeCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/quota/paused":
			_, _ = w.Write([]byte(`{"entries":[]}`))
		case "/v0/management/quota/downgraded":
			if len(downgradeCalls) == 0 {
				_, _ = w.Write([]byte(`{"entries":[]}`))
			} else {
				_, _ = w.Write([]byte(`{"entries":[{"key_hash":"key-a","reason":"spend_limit_exceeded","fallback_model":"gpt-5.6-luna"}]}`))
			}
		case "/v0/management/quota/downgrade":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode downgrade request: %v", err)
			}
			downgradeCalls = append(downgradeCalls, body)
			w.WriteHeader(http.StatusOK)
		case "/v0/management/quota/downgrade/resume":
			resumeCalls++
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{
		"gpt-4": {Prompt: 10, Completion: 10, Cache: 0},
	}); err != nil {
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
	if _, err := db.InsertEvents(ctx, []usage.Event{spendLimitEvent("event-a", "key-a", time.Now())}); err != nil {
		t.Fatalf("InsertEvents(): %v", err)
	}

	if err := ReconcileSpendLimits(db, newPauseClient(upstream.URL, "management-key")); err != nil {
		t.Fatalf("downgrade reconciliation: %v", err)
	}
	if len(downgradeCalls) != 1 {
		t.Fatalf("downgrade calls = %d, want 1", len(downgradeCalls))
	}
	if got := downgradeCalls[0]["fallback_model"]; got != store.DefaultFallbackModel {
		t.Fatalf("fallback_model = %v", got)
	}

	if err := db.SaveSpendLimitConfig(ctx, store.SpendLimitConfig{Enabled: false}); err != nil {
		t.Fatalf("disable config: %v", err)
	}
	if err := ReconcileSpendLimits(db, newPauseClient(upstream.URL, "management-key")); err != nil {
		t.Fatalf("resume reconciliation: %v", err)
	}
	if resumeCalls != 1 {
		t.Fatalf("downgrade resume calls = %d, want 1", resumeCalls)
	}
}

func TestSpendLimitConfigDefaultsRemainBackwardCompatible(t *testing.T) {
	cfg := store.SpendLimitConfig{}
	if got := cfg.EffectiveExceededAction(); got != store.ExceededActionPause {
		t.Fatalf("default action = %q", got)
	}
	if got := cfg.EffectiveFallbackModel(); got != store.DefaultFallbackModel {
		t.Fatalf("default model = %q", got)
	}
	if got := (store.SpendLimitConfig{ExceededAction: "invalid"}).EffectiveExceededAction(); got != store.ExceededActionPause {
		t.Fatalf("invalid action = %q, want pause", got)
	}
}
