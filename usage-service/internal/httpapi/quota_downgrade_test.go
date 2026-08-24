package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/collector"
	"github.com/seakee/cpa-manager/usage-service/internal/config"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
)

func TestDowngradeConfigValidatesBeforeSave(t *testing.T) {
	validationCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/quota/validate-model" {
			validationCalls++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid fallback model"}`))
			return
		}
		if r.URL.Path == "/v0/management/quota/paused" || r.URL.Path == "/v0/management/quota/downgraded" {
			_, _ = w.Write([]byte(`{"entries":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	cfg := config.Config{
		DBPath:         filepath.Join(t.TempDir(), "usage.sqlite"),
		CPAUpstreamURL: upstream.URL,
		ManagementKey:  "management-key",
		Queue:          "usage",
		PopSide:        "right",
		CORSOrigins:    []string{"*"},
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("store.Open(): %v", err)
	}
	defer db.Close()
	manager := collector.NewManager(cfg, db, nil, collector.AlertConfig{})
	handler := New(cfg, db, manager).Handler()

	req := httptest.NewRequest(http.MethodPut, "/v0/management/quota/downgrade-config", strings.NewReader(`{"enabled":true,"fallback_model":"bad-model"}`))
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid model status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if validationCalls != 1 {
		t.Fatalf("validation calls = %d, want 1", validationCalls)
	}
	if _, ok, err := db.LoadDowngradeSpendLimitConfig(context.Background()); err != nil || ok {
		t.Fatalf("config after rejected validation = ok:%v err:%v", ok, err)
	}
}

func TestPauseAndDowngradeConfigsAreIndependent(t *testing.T) {
	cfg := config.Config{DBPath: filepath.Join(t.TempDir(), "usage.sqlite"), Queue: "usage", PopSide: "right", CORSOrigins: []string{"*"}}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("store.Open(): %v", err)
	}
	defer db.Close()

	if err := db.SavePauseSpendLimitConfig(context.Background(), store.SpendLimitConfig{
		Enabled: true,
		Default: store.SpendLimit{DailyCents: 100},
	}); err != nil {
		t.Fatalf("save pause config: %v", err)
	}
	if err := db.SaveDowngradeSpendLimitConfig(context.Background(), store.SpendLimitConfig{
		Enabled:       true,
		Default:       store.SpendLimit{WeeklyCents: 200},
		FallbackModel: "gpt-5.6-luna",
	}); err != nil {
		t.Fatalf("save downgrade config: %v", err)
	}

	pause, pauseOK, err := db.LoadPauseSpendLimitConfig(context.Background())
	if err != nil || !pauseOK {
		t.Fatalf("load pause config = ok:%v err:%v", pauseOK, err)
	}
	downgrade, downgradeOK, err := db.LoadDowngradeSpendLimitConfig(context.Background())
	if err != nil || !downgradeOK {
		t.Fatalf("load downgrade config = ok:%v err:%v", downgradeOK, err)
	}
	if pause.DefaultLimit().DailyCents != 100 || pause.DefaultLimit().WeeklyCents != 0 {
		t.Fatalf("pause config = %+v", pause)
	}
	if downgrade.DefaultLimit().DailyCents != 0 || downgrade.DefaultLimit().WeeklyCents != 200 {
		t.Fatalf("downgrade config = %+v", downgrade)
	}
}

func TestDowngradeConfigReturnsIndependentDefaults(t *testing.T) {
	cfg := config.Config{DBPath: filepath.Join(t.TempDir(), "usage.sqlite"), Queue: "usage", PopSide: "right", CORSOrigins: []string{"*"}}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("store.Open(): %v", err)
	}
	defer db.Close()
	handler := New(cfg, db, collector.NewManager(cfg, db, nil, collector.AlertConfig{})).Handler()

	req := httptest.NewRequest(http.MethodGet, "/v0/management/quota/downgrade-config", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET downgrade config status = %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"fallback_model":"gpt-5.6-luna"`) || !strings.Contains(body, `"enabled":false`) {
		t.Fatalf("downgrade defaults missing: %s", body)
	}
}
