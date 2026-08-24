package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/collector"
	"github.com/seakee/cpa-manager/usage-service/internal/config"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

type observedRequest struct {
	path  string
	query string
	auth  string
}

func newTestHandler(t *testing.T, upstreamURL string, saveSetup bool) http.Handler {
	t.Helper()

	cfg := config.Config{
		DBPath:      filepath.Join(t.TempDir(), "usage.sqlite"),
		Queue:       "usage",
		PopSide:     "right",
		CORSOrigins: []string{"*"},
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if saveSetup {
		err := db.SaveSetup(context.Background(), store.Setup{
			CPAUpstreamURL: upstreamURL,
			ManagementKey:  "management-key",
			Queue:          "usage",
			PopSide:        "right",
		})
		if err != nil {
			t.Fatalf("save setup: %v", err)
		}
	}

	manager := collector.NewManager(cfg, db, nil, collector.AlertConfig{})
	return New(cfg, db, manager).Handler()
}

func newTestHandlerWithConfig(t *testing.T, cfg config.Config) http.Handler {
	t.Helper()

	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(t.TempDir(), "usage.sqlite")
	}
	if len(cfg.CORSOrigins) == 0 {
		cfg.CORSOrigins = []string{"*"}
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	manager := collector.NewManager(cfg, db, nil, collector.AlertConfig{})
	return New(cfg, db, manager).Handler()
}

func TestModelListProxyPreservesAuthorization(t *testing.T) {
	for _, path := range []string{"/v1/models", "/models"} {
		t.Run(path, func(t *testing.T) {
			observed := make(chan observedRequest, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observed <- observedRequest{
					path:  r.URL.Path,
					query: r.URL.RawQuery,
					auth:  r.Header.Get("Authorization"),
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
			}))
			t.Cleanup(upstream.Close)

			handler := newTestHandler(t, upstream.URL, true)
			req := httptest.NewRequest(http.MethodGet, path+"?limit=20", nil)
			req.Header.Set("Authorization", "Bearer upstream-key")
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "gpt-4o") {
				t.Fatalf("response body = %s", rr.Body.String())
			}

			var got observedRequest
			select {
			case got = <-observed:
			default:
				t.Fatal("upstream was not called")
			}
			if got.path != path {
				t.Fatalf("proxied path = %q, want %q", got.path, path)
			}
			if got.query != "limit=20" {
				t.Fatalf("proxied query = %q, want limit=20", got.query)
			}
			if got.auth != "Bearer upstream-key" {
				t.Fatalf("proxied authorization = %q", got.auth)
			}
		})
	}
}

func TestUsageImportAcceptsLegacyExportAndSkipsDuplicates(t *testing.T) {
	handler := newTestHandler(t, "http://example.test", true)
	payload := `{
	  "version": 1,
	  "exported_at": "2026-01-02T03:04:05Z",
	  "usage": {
	    "apis": {
	      "POST /v1/chat/completions": {
	        "models": {
	          "gpt-4o": {
	            "details": [
	              {
	                "timestamp": "2026-01-02T03:04:05Z",
	                "source": "alice@example.com",
	                "auth_index": "auth-1",
	                "tokens": {
	                  "input_tokens": 10,
	                  "output_tokens": 20,
	                  "total_tokens": 30
	                },
	                "failed": false
	              }
	            ]
	          }
	        }
	      }
	    }
	  }
	}`

	first := postUsageImport(t, handler, payload)
	if first.Format != "legacy_usage_export" || first.Added != 1 || first.Skipped != 0 || first.Total != 1 {
		t.Fatalf("first import = %#v", first)
	}
	if len(first.Warnings) == 0 {
		t.Fatalf("expected legacy warnings: %#v", first)
	}

	second := postUsageImport(t, handler, payload)
	if second.Format != "legacy_usage_export" || second.Added != 0 || second.Skipped != 1 || second.Total != 1 {
		t.Fatalf("second import = %#v", second)
	}
}

func postUsageImport(t *testing.T, handler http.Handler, payload string) struct {
	Format      string   `json:"format"`
	Added       int      `json:"added"`
	Skipped     int      `json:"skipped"`
	Total       int      `json:"total"`
	Failed      int      `json:"failed"`
	Unsupported int      `json:"unsupported"`
	Warnings    []string `json:"warnings"`
} {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("import status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var response struct {
		Format      string   `json:"format"`
		Added       int      `json:"added"`
		Skipped     int      `json:"skipped"`
		Total       int      `json:"total"`
		Failed      int      `json:"failed"`
		Unsupported int      `json:"unsupported"`
		Warnings    []string `json:"warnings"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}

func TestModelListProxyRequiresSetup(t *testing.T) {
	handler := newTestHandler(t, "", false)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "usage service is not configured") {
		t.Fatalf("response body = %s", rr.Body.String())
	}
}

func TestSetupRejectsDifferentUpstreamWithoutExistingAuthorization(t *testing.T) {
	currentUpstream := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(currentUpstream.Close)

	nextValidationCalled := make(chan struct{}, 1)
	nextUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case nextValidationCalled <- struct{}{}:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(nextUpstream.Close)

	handler := newTestHandler(t, currentUpstream.URL, true)
	req := httptest.NewRequest(
		http.MethodPost,
		"/setup",
		bytes.NewBufferString(`{"cpaBaseUrl":"`+nextUpstream.URL+`","managementKey":"rotated-key"}`),
	)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("setup status = %d, body = %s", rr.Code, rr.Body.String())
	}
	select {
	case <-nextValidationCalled:
		t.Fatal("new upstream should not be validated without existing setup authorization")
	default:
	}
}

func TestSetupAllowsKeyRotationForSameUpstreamWithValidNewKey(t *testing.T) {
	observed := make(chan observedRequest, 10)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/config" {
			observed <- observedRequest{
				path: r.URL.Path,
				auth: r.Header.Get("Authorization"),
			}
		}
		if r.URL.Path == "/v0/management/config" && r.Header.Get("Authorization") == "Bearer rotated-key" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.URL.Path == "/v0/management/usage-statistics-enabled" &&
			r.Method == http.MethodPut &&
			r.Header.Get("Authorization") == "Bearer rotated-key" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)

	handler := newTestHandler(t, upstream.URL, true)
	req := httptest.NewRequest(
		http.MethodPost,
		"/setup",
		bytes.NewBufferString(`{"cpaBaseUrl":"`+upstream.URL+`","managementKey":"rotated-key"}`),
	)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("setup status = %d, body = %s", rr.Code, rr.Body.String())
	}
	got := <-observed
	if got.path != "/v0/management/config" {
		t.Fatalf("validation path = %q", got.path)
	}
	if got.auth != "Bearer rotated-key" {
		t.Fatalf("validation authorization = %q", got.auth)
	}

	req = httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Authorization", "Bearer rotated-key")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status after rotation = %d, body = %s", rr.Code, rr.Body.String())
	}
}

func TestSetupRejectsKeyRotationWhenSetupIsEnvironmentManaged(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/config" && r.Header.Get("Authorization") == "Bearer rotated-key" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)

	handler := newTestHandlerWithConfig(t, config.Config{
		CPAUpstreamURL: upstream.URL,
		ManagementKey:  "env-key",
		Queue:          "usage",
		PopSide:        "right",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/setup",
		bytes.NewBufferString(`{"cpaBaseUrl":"`+upstream.URL+`","managementKey":"rotated-key"}`),
	)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("setup status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "environment") {
		t.Fatalf("response body = %s", rr.Body.String())
	}
}

func TestManagerConfigRejectsPollIntervalAboveRetention(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/config" && r.Header.Get("Authorization") == "Bearer management-key" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"usage-statistics-enabled":true,"redis-usage-queue-retention-seconds":1}`))
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)

	handler := newTestHandler(t, upstream.URL, true)
	body := bytes.NewBufferString(`{"config":{"cpaConnection":{"cpaBaseUrl":"` + upstream.URL + `","managementKey":"management-key"},"collector":{"collectorMode":"auto","queue":"usage","popSide":"right","batchSize":100,"pollIntervalMs":2000,"queryLimit":50000},"externalUsageService":{"enabled":true,"serviceBase":"http://usage.test"}}}`)
	req := httptest.NewRequest(http.MethodPut, "/usage-service/config", body)
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("save status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "pollIntervalMs") {
		t.Fatalf("response body = %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"code":"poll_interval_exceeds_retention"`) {
		t.Fatalf("response body = %s", rr.Body.String())
	}
}

func TestManagerConfigReadsLegacySetup(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/config" && r.Header.Get("Authorization") == "Bearer management-key" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"usage-statistics-enabled":true}`))
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)

	handler := newTestHandler(t, upstream.URL, true)
	req := httptest.NewRequest(http.MethodGet, "/usage-service/config", nil)
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("config status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"source":"db"`) {
		t.Fatalf("response body = %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), upstream.URL) {
		t.Fatalf("response body = %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"enabled":true`) {
		t.Fatalf("response body = %s", rr.Body.String())
	}
}

func TestSetupCanDisableRequestMonitoring(t *testing.T) {
	configCalls := 0
	enableCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/config" && r.Header.Get("Authorization") == "Bearer management-key" {
			configCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"usage-statistics-enabled":false,"redis-usage-queue-retention-seconds":1}`))
			return
		}
		if r.URL.Path == "/v0/management/usage-statistics-enabled" {
			enableCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)

	handler := newTestHandler(t, upstream.URL, false)
	body := bytes.NewBufferString(`{"cpaBaseUrl":"` + upstream.URL + `","managementKey":"management-key","requestMonitoringEnabled":false,"ensureUsageStatisticsEnabled":false}`)
	req := httptest.NewRequest(http.MethodPost, "/setup", body)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("setup status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if configCalls != 1 {
		t.Fatalf("config calls = %d, want 1", configCalls)
	}
	if enableCalls != 0 {
		t.Fatalf("enable calls = %d, want 0", enableCalls)
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/status", nil)
	statusReq.Header.Set("Authorization", "Bearer management-key")
	statusRR := httptest.NewRecorder()
	handler.ServeHTTP(statusRR, statusReq)

	if statusRR.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", statusRR.Code, statusRR.Body.String())
	}
	if !strings.Contains(statusRR.Body.String(), `"collector":"stopped"`) {
		t.Fatalf("status body = %s", statusRR.Body.String())
	}

	configReq := httptest.NewRequest(http.MethodGet, "/usage-service/config", nil)
	configReq.Header.Set("Authorization", "Bearer management-key")
	configRR := httptest.NewRecorder()
	handler.ServeHTTP(configRR, configReq)

	if configRR.Code != http.StatusOK {
		t.Fatalf("config status = %d, body = %s", configRR.Code, configRR.Body.String())
	}
	if !strings.Contains(configRR.Body.String(), `"enabled":false`) {
		t.Fatalf("config body = %s", configRR.Body.String())
	}
}

func TestModelPricesSaveAndLoad(t *testing.T) {
	handler := newTestHandler(t, "http://example.test", true)
	body := bytes.NewBufferString(`{"prices":{"gpt-test":{"prompt":1.25,"completion":2.5,"cache":0.1}}}`)
	req := httptest.NewRequest(http.MethodPut, "/v0/management/model-prices", body)
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("save status = %d, body = %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v0/management/model-prices", nil)
	req.Header.Set("Authorization", "Bearer management-key")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("load status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var response struct {
		Prices map[string]struct {
			Prompt     float64 `json:"prompt"`
			Completion float64 `json:"completion"`
			Cache      float64 `json:"cache"`
		} `json:"prices"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	price, ok := response.Prices["gpt-test"]
	if !ok {
		t.Fatalf("missing saved price: %#v", response.Prices)
	}
	if price.Prompt != 1.25 || price.Completion != 2.5 || price.Cache != 0.1 {
		t.Fatalf("price = %#v", price)
	}
}

func TestAPIKeyAliasesSaveLoadAndDelete(t *testing.T) {
	handler := newTestHandler(t, "http://example.test", true)
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	body := bytes.NewBufferString(`{"items":[{"apiKeyHash":"` + hash + `","alias":"Team A"}]}`)
	req := httptest.NewRequest(http.MethodPut, "/v0/management/api-key-aliases", body)
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("save status = %d, body = %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v0/management/api-key-aliases", nil)
	req.Header.Set("Authorization", "Bearer management-key")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("load status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var response struct {
		Items []struct {
			APIKeyHash  string `json:"apiKeyHash"`
			Alias       string `json:"alias"`
			UpdatedAtMS int64  `json:"updatedAtMs"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("items = %#v", response.Items)
	}
	if response.Items[0].APIKeyHash != hash || response.Items[0].Alias != "Team A" || response.Items[0].UpdatedAtMS <= 0 {
		t.Fatalf("alias = %#v", response.Items[0])
	}

	const otherHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	req = httptest.NewRequest(
		http.MethodPut,
		"/v0/management/api-key-aliases",
		bytes.NewBufferString(`{"items":[{"apiKeyHash":"`+otherHash+`","alias":" team a "}]}`),
	)
	req.Header.Set("Authorization", "Bearer management-key")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("duplicate status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"code":"api_key_alias_duplicate"`) {
		t.Fatalf("duplicate body = %s", rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v0/management/api-key-aliases/"+hash, nil)
	req.Header.Set("Authorization", "Bearer management-key")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", rr.Code, rr.Body.String())
	}
}

func TestModelPricesSyncFromLiteLLMFormat(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"sample_spec": {},
			"gpt-test": {
				"input_cost_per_token": 0.00000125,
				"output_cost_per_token": 0.0000025,
				"cache_read_input_token_cost": 0.0000001,
				"mode": "chat"
			},
			"image-only": {
				"output_cost_per_image": 0.04,
				"mode": "image_generation"
			}
		}`))
	}))
	t.Cleanup(source.Close)
	oldURL := modelPriceSyncURL
	modelPriceSyncURL = source.URL
	t.Cleanup(func() {
		modelPriceSyncURL = oldURL
	})

	handler := newTestHandler(t, "http://example.test", true)
	req := httptest.NewRequest(
		http.MethodPost,
		"/v0/management/model-prices/sync",
		bytes.NewBufferString(`{"models":["gpt-test"]}`),
	)
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("sync status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var response struct {
		Source   string `json:"source"`
		Imported int    `json:"imported"`
		Skipped  int    `json:"skipped"`
		Prices   map[string]struct {
			Prompt        float64 `json:"prompt"`
			Completion    float64 `json:"completion"`
			Cache         float64 `json:"cache"`
			Source        string  `json:"source"`
			SourceModelID string  `json:"sourceModelId"`
		} `json:"prices"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Source != "litellm" || response.Imported != 1 || response.Skipped != 2 {
		t.Fatalf("sync summary = %#v", response)
	}
	price, ok := response.Prices["gpt-test"]
	if !ok {
		t.Fatalf("missing synced price: %#v", response.Prices)
	}
	if !closeFloat(price.Prompt, 1.25) || !closeFloat(price.Completion, 2.5) || !closeFloat(price.Cache, 0.1) {
		t.Fatalf("price = %#v", price)
	}
	if price.Source != "litellm" || price.SourceModelID != "gpt-test" {
		t.Fatalf("source metadata = %#v", price)
	}
}

func TestQuotaConfigSaveImmediatelyReconciles(t *testing.T) {
	requests := make([]string, 0, 6)
	automaticPaused := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/quota/paused":
			w.Header().Set("Content-Type", "application/json")
			if automaticPaused {
				_, _ = w.Write([]byte(`{"entries":[{"key_hash":"quota-key","reason":"spend_limit_exceeded"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"entries":[]}`))
		case "/v0/management/quota/pause":
			requests = append(requests, "pause")
			automaticPaused = true
			w.WriteHeader(http.StatusOK)
		case "/v0/management/quota/resume":
			requests = append(requests, "resume")
			automaticPaused = false
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	cfg := config.Config{DBPath: filepath.Join(t.TempDir(), "usage.sqlite"), CPAUpstreamURL: upstream.URL, ManagementKey: "management-key", Queue: "usage", PopSide: "right", CORSOrigins: []string{"*"}}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{"gpt-4": {Prompt: 10, Completion: 30, Cache: 5}}); err != nil {
		t.Fatalf("save prices: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{{EventHash: "quota-event", TimestampMS: time.Now().UnixMilli(), Timestamp: time.Now().UTC().Format(time.RFC3339), Model: "gpt-4", APIKeyHash: "quota-key", InputTokens: 100000, TotalTokens: 100000, CreatedAtMS: time.Now().UnixMilli()}}); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	handler := New(cfg, db, collector.NewManager(cfg, db, nil, collector.AlertConfig{})).Handler()

	// 先设置宽松默认值，再验证新增、收紧、放宽、删除和禁用都在 PUT 返回前同步。
	putQuotaConfig(t, handler, `{"enabled":true,"default":{"daily_cents":1000,"weekly_cents":0}}`, http.StatusOK)
	putQuotaConfig(t, handler, `{"overrides":[{"apply_to":"api-key","apply_value":"quota-key","daily_cents":1,"weekly_cents":0}]}`, http.StatusOK)
	putQuotaConfig(t, handler, `{"overrides":[{"apply_to":"api-key","apply_value":"quota-key","daily_cents":1000,"weekly_cents":0}]}`, http.StatusOK)
	putQuotaConfig(t, handler, `{"overrides":[{"apply_to":"api-key","apply_value":"quota-key","daily_cents":1,"weekly_cents":0}]}`, http.StatusOK)
	putQuotaConfig(t, handler, `{"overrides":[]}`, http.StatusOK)
	putQuotaConfig(t, handler, `{"overrides":[{"apply_to":"api-key","apply_value":"quota-key","daily_cents":1,"weekly_cents":0}]}`, http.StatusOK)
	putQuotaConfig(t, handler, `{"enabled":false}`, http.StatusOK)
	if strings.Join(requests, ",") != "pause,resume,pause,resume,pause,resume" {
		t.Fatalf("quota requests = %#v, want each config change reconciled immediately", requests)
	}
}

func TestQuotaConfigSaveReturnsFailureAndPersistsForRetry(t *testing.T) {
	fail := true
	pauseCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/quota/paused":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entries":[]}`))
		case "/v0/management/quota/pause":
			pauseCalls++
			if fail {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	cfg := config.Config{DBPath: filepath.Join(t.TempDir(), "usage.sqlite"), CPAUpstreamURL: upstream.URL, ManagementKey: "management-key", Queue: "usage", PopSide: "right", CORSOrigins: []string{"*"}}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.UpsertSyncedModelPrices(ctx, map[string]store.ModelPrice{"gpt-4": {Prompt: 10, Completion: 30, Cache: 5}}); err != nil {
		t.Fatalf("save prices: %v", err)
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{{EventHash: "retry-event", TimestampMS: time.Now().UnixMilli(), Timestamp: time.Now().UTC().Format(time.RFC3339), Model: "gpt-4", APIKeyHash: "retry-key", InputTokens: 100000, TotalTokens: 100000, CreatedAtMS: time.Now().UnixMilli()}}); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	handler := New(cfg, db, collector.NewManager(cfg, db, nil, collector.AlertConfig{})).Handler()
	body := `{"enabled":true,"default":{"daily_cents":1,"weekly_cents":0}}`
	putQuotaConfig(t, handler, body, http.StatusBadGateway)
	persisted, ok, err := db.LoadSpendLimitConfig(ctx)
	if err != nil || !ok || !persisted.Enabled || persisted.Default.DailyCents != 1 {
		t.Fatalf("persisted config = %#v, ok=%v, err=%v", persisted, ok, err)
	}
	fail = false
	putQuotaConfig(t, handler, body, http.StatusOK)
	if pauseCalls != 2 {
		t.Fatalf("pause calls = %d, want retry", pauseCalls)
	}
}

func TestQuotaConfigSaveReturnsPausedListAndResumeFailuresForRetry(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "paused list", path: "/v0/management/quota/paused"},
		{name: "conditional resume", path: "/v0/management/quota/resume"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fail := true
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v0/management/quota/paused":
					calls++
					if fail && tt.path == r.URL.Path {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					if tt.path == "/v0/management/quota/resume" {
						_, _ = w.Write([]byte(`{"entries":[{"key_hash":"automatic","reason":"spend_limit_exceeded"}]}`))
						return
					}
					_, _ = w.Write([]byte(`{"entries":[]}`))
				case "/v0/management/quota/resume":
					calls++
					var body struct {
						ExpectedReason string `json:"expected_reason"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatalf("decode resume body: %v", err)
					}
					if body.ExpectedReason != "spend_limit_exceeded" {
						t.Fatalf("expected_reason = %q", body.ExpectedReason)
					}
					if fail && tt.path == r.URL.Path {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					w.WriteHeader(http.StatusOK)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(upstream.Close)

			cfg := config.Config{DBPath: filepath.Join(t.TempDir(), "usage.sqlite"), CPAUpstreamURL: upstream.URL, ManagementKey: "management-key", Queue: "usage", PopSide: "right", CORSOrigins: []string{"*"}}
			db, err := store.Open(cfg.DBPath)
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			handler := New(cfg, db, collector.NewManager(cfg, db, nil, collector.AlertConfig{})).Handler()

			putQuotaConfig(t, handler, `{"enabled":false}`, http.StatusBadGateway)
			persisted, ok, err := db.LoadSpendLimitConfig(context.Background())
			if err != nil || !ok || persisted.Enabled {
				t.Fatalf("persisted config = %#v, ok=%v, err=%v", persisted, ok, err)
			}
			fail = false
			putQuotaConfig(t, handler, `{"enabled":false}`, http.StatusOK)
			wantCalls := 3 // 首次 503 会对 paused 列表自动重试一次。
			if tt.path == "/v0/management/quota/resume" {
				wantCalls = 4 // 每次协调都先读取 paused，再尝试条件恢复。
			}
			if calls != wantCalls {
				t.Fatalf("synchronization calls = %d, want %d after retry", calls, wantCalls)
			}
		})
	}
}

func TestQuotaConfigSaveReturnsNetworkFailuresForRetry(t *testing.T) {
	tests := []struct {
		name     string
		failPath string
	}{
		{name: "paused list", failPath: "/v0/management/quota/paused"},
		{name: "conditional resume", failPath: "/v0/management/quota/resume"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failing := true
			pausedCalls := 0
			resumeCalls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v0/management/quota/paused":
					pausedCalls++
					if failing && tt.failPath == r.URL.Path {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Errorf("hijack paused response: %v", err)
							return
						}
						_ = conn.Close()
						return
					}
					w.Header().Set("Content-Type", "application/json")
					if tt.failPath == "/v0/management/quota/resume" {
						_, _ = w.Write([]byte(`{"entries":[{"key_hash":"automatic","reason":"spend_limit_exceeded"}]}`))
						return
					}
					_, _ = w.Write([]byte(`{"entries":[]}`))
				case "/v0/management/quota/resume":
					resumeCalls++
					if failing && tt.failPath == r.URL.Path {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Errorf("hijack resume response: %v", err)
							return
						}
						_ = conn.Close()
						return
					}
					w.WriteHeader(http.StatusOK)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(upstream.Close)

			cfg := config.Config{DBPath: filepath.Join(t.TempDir(), "usage.sqlite"), CPAUpstreamURL: upstream.URL, ManagementKey: "management-key", Queue: "usage", PopSide: "right", CORSOrigins: []string{"*"}}
			db, err := store.Open(cfg.DBPath)
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			handler := New(cfg, db, collector.NewManager(cfg, db, nil, collector.AlertConfig{})).Handler()

			body := `{"enabled":false}`
			putQuotaConfig(t, handler, body, http.StatusBadGateway)
			persisted, ok, err := db.LoadSpendLimitConfig(context.Background())
			if err != nil || !ok || persisted.Enabled {
				t.Fatalf("persisted config = %#v, ok=%v, err=%v", persisted, ok, err)
			}
			failing = false
			putQuotaConfig(t, handler, body, http.StatusOK)
			if pausedCalls != 2 {
				t.Fatalf("paused calls = %d, want 2 after retry", pausedCalls)
			}
			if tt.failPath == "/v0/management/quota/resume" && resumeCalls != 2 {
				t.Fatalf("resume calls = %d, want 2 after retry", resumeCalls)
			}
		})
	}
}

func putQuotaConfig(t *testing.T, handler http.Handler, body string, wantStatus int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v0/management/quota/config", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != wantStatus {
		t.Fatalf("quota config status = %d, body = %s", rr.Code, rr.Body.String())
	}
}

func TestUsageQueryFiltersByRangeAndAPIKeyHash(t *testing.T) {
	cfg := config.Config{
		DBPath:      filepath.Join(t.TempDir(), "usage.sqlite"),
		Queue:       "usage",
		PopSide:     "right",
		CORSOrigins: []string{"*"},
		QueryLimit:  1000,
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	err = db.SaveSetup(context.Background(), store.Setup{
		CPAUpstreamURL: "http://example.test",
		ManagementKey:  "management-key",
		Queue:          "usage",
		PopSide:        "right",
	})
	if err != nil {
		t.Fatalf("save setup: %v", err)
	}

	_, err = db.InsertEvents(context.Background(), []usage.Event{
		{
			EventHash:   "e-1",
			TimestampMS: 1_700_000_000_000,
			Timestamp:   "2023-11-14T22:13:20Z",
			Model:       "gpt-a",
			Endpoint:    "POST /v1/chat/completions",
			APIKeyHash:  "hash-a",
			TotalTokens: 10,
			CreatedAtMS: 1_700_000_000_001,
		},
		{
			EventHash:   "e-2",
			TimestampMS: 1_700_100_000_000,
			Timestamp:   "2023-11-16T02:00:00Z",
			Model:       "gpt-b",
			Endpoint:    "POST /v1/chat/completions",
			APIKeyHash:  "hash-b",
			TotalTokens: 20,
			CreatedAtMS: 1_700_100_000_001,
		},
	})
	if err != nil {
		t.Fatalf("insert events: %v", err)
	}

	handler := New(cfg, db, collector.NewManager(cfg, db, nil, collector.AlertConfig{})).Handler()

	req := httptest.NewRequest(
		http.MethodGet,
		"/v0/management/usage?fromMs=1700050000000&toMs=1700200000000&apiKeyHash=hash-b",
		nil,
	)
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var payload struct {
		TotalRequests int `json:"total_requests"`
		TotalTokens   int `json:"total_tokens"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.TotalRequests != 1 || payload.TotalTokens != 20 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestEnterpriseKeyBindingsImportPersistsErrorDetails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/v0/management/api-keys" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	handler := newTestHandler(t, upstream.URL, true)

	putDepartmentsReq := httptest.NewRequest(
		http.MethodPut,
		"/v0/management/enterprise/departments",
		bytes.NewBufferString(`{"items":[{"id":"dept_sh","name":"上海","prefix":"sh","sortOrder":1,"enabled":true,"system":false}]}`),
	)
	putDepartmentsReq.Header.Set("Authorization", "Bearer management-key")
	putDepartmentsRR := httptest.NewRecorder()
	handler.ServeHTTP(putDepartmentsRR, putDepartmentsReq)
	if putDepartmentsRR.Code != http.StatusOK {
		t.Fatalf("put departments status = %d, body = %s", putDepartmentsRR.Code, putDepartmentsRR.Body.String())
	}

	importReq := httptest.NewRequest(
		http.MethodPost,
		"/v0/management/enterprise/key-bindings/import",
		bytes.NewBufferString(`{
			"fileName":"employees.csv",
			"items":[
				{"userName":"张三","departmentName":"上海","departmentId":"dept_sh","generatedKey":"sh-zs-ABCDEFGHIJKLMNOPQRSTUVWXYZ1234","status":"ok"},
				{"userName":"李四","departmentName":"未知","departmentId":"","status":"error","errorReason":"department not found: 未知"}
			]
		}`),
	)
	importReq.Header.Set("Authorization", "Bearer management-key")
	importRR := httptest.NewRecorder()
	handler.ServeHTTP(importRR, importReq)
	if importRR.Code != http.StatusOK {
		t.Fatalf("import status = %d, body = %s", importRR.Code, importRR.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/v0/management/enterprise/import-history?limit=1", nil)
	historyReq.Header.Set("Authorization", "Bearer management-key")
	historyRR := httptest.NewRecorder()
	handler.ServeHTTP(historyRR, historyReq)
	if historyRR.Code != http.StatusOK {
		t.Fatalf("history status = %d, body = %s", historyRR.Code, historyRR.Body.String())
	}

	var historyResp struct {
		Items []struct {
			CSVFileName  string `json:"csvFileName"`
			TotalRows    int    `json:"totalRows"`
			PassedRows   int    `json:"passedRows"`
			WarningRows  int    `json:"warningRows"`
			ErrorRows    int    `json:"errorRows"`
			ErrorDetails string `json:"errorDetails"`
		} `json:"items"`
	}
	if err := json.Unmarshal(historyRR.Body.Bytes(), &historyResp); err != nil {
		t.Fatalf("decode history response: %v", err)
	}
	if len(historyResp.Items) != 1 {
		t.Fatalf("history items = %#v", historyResp.Items)
	}
	item := historyResp.Items[0]
	if item.CSVFileName != "employees.csv" || item.TotalRows != 2 || item.PassedRows != 1 || item.WarningRows != 0 || item.ErrorRows != 1 {
		t.Fatalf("history summary = %#v", item)
	}

	var details []struct {
		Row    int    `json:"row"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(item.ErrorDetails), &details); err != nil {
		t.Fatalf("decode error details: %v, raw=%s", err, item.ErrorDetails)
	}
	if len(details) != 1 || details[0].Row != 2 || details[0].Reason != "department not found: 未知" {
		t.Fatalf("error details = %#v", details)
	}
}

func TestEnterpriseKeyBindingsCreateAndUpdateEmail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/v0/management/api-keys" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	handler := newTestHandler(t, upstream.URL, true)

	putDepartmentsReq := httptest.NewRequest(
		http.MethodPut,
		"/v0/management/enterprise/departments",
		bytes.NewBufferString(`{"items":[{"id":"dept_sh","name":"上海","prefix":"sh","sortOrder":1,"enabled":true,"system":false}]}`),
	)
	putDepartmentsReq.Header.Set("Authorization", "Bearer management-key")
	putDepartmentsRR := httptest.NewRecorder()
	handler.ServeHTTP(putDepartmentsRR, putDepartmentsReq)
	if putDepartmentsRR.Code != http.StatusOK {
		t.Fatalf("put departments status = %d, body = %s", putDepartmentsRR.Code, putDepartmentsRR.Body.String())
	}

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/v0/management/enterprise/key-bindings",
		bytes.NewBufferString(`{"userName":"张三","departmentId":"dept_sh","apiKey":"sh-zs-fixed","email":"  zs@example.com  "}`),
	)
	createReq.Header.Set("Authorization", "Bearer management-key")
	createRR := httptest.NewRecorder()
	handler.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", createRR.Code, createRR.Body.String())
	}

	var created struct {
		APIKey       string `json:"apiKey"`
		DepartmentID string `json:"departmentId"`
		Email        string `json:"email"`
	}
	if err := json.Unmarshal(createRR.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.APIKey != "sh-zs-fixed" || created.DepartmentID != "dept_sh" || created.Email != "zs@example.com" {
		t.Fatalf("created binding = %#v", created)
	}

	updateReq := httptest.NewRequest(
		http.MethodPatch,
		"/v0/management/enterprise/key-bindings/sh-zs-fixed",
		bytes.NewBufferString(`{"userName":"张三","departmentId":"dept_sh","email":"updated@example.com"}`),
	)
	updateReq.Header.Set("Authorization", "Bearer management-key")
	updateRR := httptest.NewRecorder()
	handler.ServeHTTP(updateRR, updateReq)
	if updateRR.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updateRR.Code, updateRR.Body.String())
	}

	var updated struct {
		APIKey       string `json:"apiKey"`
		DepartmentID string `json:"departmentId"`
		Email        string `json:"email"`
	}
	if err := json.Unmarshal(updateRR.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.APIKey != "sh-zs-fixed" || updated.DepartmentID != "dept_sh" || updated.Email != "updated@example.com" {
		t.Fatalf("updated binding = %#v", updated)
	}
}

func TestEnterpriseKeyBindingsGenerateReturnsEmail(t *testing.T) {
	handler := newTestHandler(t, "http://example.test", true)

	putDepartmentsReq := httptest.NewRequest(
		http.MethodPut,
		"/v0/management/enterprise/departments",
		bytes.NewBufferString(`{"items":[{"id":"dept_sh","name":"上海","prefix":"sh","sortOrder":1,"enabled":true,"system":false}]}`),
	)
	putDepartmentsReq.Header.Set("Authorization", "Bearer management-key")
	putDepartmentsRR := httptest.NewRecorder()
	handler.ServeHTTP(putDepartmentsRR, putDepartmentsReq)
	if putDepartmentsRR.Code != http.StatusOK {
		t.Fatalf("put departments status = %d, body = %s", putDepartmentsRR.Code, putDepartmentsRR.Body.String())
	}

	generateReq := httptest.NewRequest(
		http.MethodPost,
		"/v0/management/enterprise/key-bindings/generate",
		bytes.NewBufferString(`{"csv":"用户名,邮箱,部门\n张三,zs@example.com,上海\n","fileName":"employees.csv"}`),
	)
	generateReq.Header.Set("Authorization", "Bearer management-key")
	generateReq.Header.Set("Content-Type", "application/json")
	generateRR := httptest.NewRecorder()
	handler.ServeHTTP(generateRR, generateReq)
	if generateRR.Code != http.StatusOK {
		t.Fatalf("generate status = %d, body = %s", generateRR.Code, generateRR.Body.String())
	}

	var response struct {
		Items []struct {
			UserName       string `json:"userName"`
			DepartmentName string `json:"departmentName"`
			DepartmentID   string `json:"departmentId"`
			Email          string `json:"email"`
			Status         string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(generateRR.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode generate response: %v", err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("generate items = %#v", response.Items)
	}
	item := response.Items[0]
	if item.UserName != "张三" || item.DepartmentName != "上海" || item.DepartmentID != "dept_sh" || item.Email != "zs@example.com" || item.Status != "ok" {
		t.Fatalf("generate item = %#v", item)
	}
}

func closeFloat(left float64, right float64) bool {
	return math.Abs(left-right) < 0.0000001
}

func TestEnterpriseUsageReportJSONResponse(t *testing.T) {
	cfg := config.Config{
		DBPath:      filepath.Join(t.TempDir(), "usage.sqlite"),
		Queue:       "usage",
		PopSide:     "right",
		CORSOrigins: []string{"*"},
		QueryLimit:  1000,
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if err := db.SaveSetup(ctx, store.Setup{
		CPAUpstreamURL: "http://example.test",
		ManagementKey:  "management-key",
		Queue:          "usage",
		PopSide:        "right",
	}); err != nil {
		t.Fatalf("save setup: %v", err)
	}
	if err := db.UpsertEnterpriseDepartments(ctx, []store.EnterpriseDepartment{
		{ID: "dept_sh", Name: "上海总部", Prefix: "sh", SortOrder: 1, Enabled: true},
	}); err != nil {
		t.Fatalf("upsert departments: %v", err)
	}
	if err := db.UpsertEnterpriseKeyBindings(ctx, []store.EnterpriseKeyBinding{
		{
			APIKey:               "key-zhangsan",
			UserName:             "zhangsan",
			DepartmentID:         "dept_sh",
			Email:                "zs@example.com",
			Source:               "manual",
			DepartmentResolvedBy: "manual",
		},
	}); err != nil {
		t.Fatalf("upsert key bindings: %v", err)
	}
	bindings, err := db.LoadEnterpriseKeyBindings(ctx)
	if err != nil {
		t.Fatalf("load bindings: %v", err)
	}
	var hashA string
	for _, b := range bindings {
		hashA = b.APIKeyHash
		break
	}
	if hashA == "" {
		t.Fatalf("could not resolve key hash")
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{
		{
			EventHash:   "report-e-1",
			TimestampMS: 1_700_000_000_000,
			Timestamp:   "2023-11-14T22:13:20Z",
			Model:       "gpt-4",
			Endpoint:    "POST /v1/chat/completions",
			APIKeyHash:  hashA,
			TotalTokens: 100,
			CreatedAtMS: 1_700_000_000_001,
		},
		{
			EventHash:   "report-e-2",
			TimestampMS: 1_700_000_000_100,
			Timestamp:   "2023-11-14T22:13:20.100Z",
			Model:       "gpt-4",
			Endpoint:    "POST /v1/chat/completions",
			APIKeyHash:  hashA,
			TotalTokens: 0,
			Failed:      true,
			CreatedAtMS: 1_700_000_000_101,
		},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	handler := New(cfg, db, collector.NewManager(cfg, db, nil, collector.AlertConfig{})).Handler()

	req := httptest.NewRequest(
		http.MethodGet,
		"/v0/management/enterprise/usage-report?fromMs=1600000000000&toMs=1800000000000",
		nil,
	)
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var response struct {
		FromMs int64 `json:"fromMs"`
		ToMs   int64 `json:"toMs"`
		Items  []struct {
			APIKeyHash       string  `json:"apiKey"`
			UserName         string  `json:"userName"`
			DepartmentName   string  `json:"departmentName"`
			Email            string  `json:"email"`
			TotalTokens      int64   `json:"totalTokens"`
			TotalRequests    int64   `json:"totalRequests"`
			FailedRequests   int64   `json:"failedRequests"`
			CachedTokens     int64   `json:"cachedTokens"`
			TotalCacheTokens int64   `json:"totalCacheTokens"`
			CacheHitRate     float64 `json:"cacheHitRate"`
			Models           []struct {
				Model            string  `json:"model"`
				TotalTokens      int64   `json:"totalTokens"`
				Requests         int64   `json:"requests"`
				FailedRequests   int64   `json:"failedRequests"`
				CachedTokens     int64   `json:"cachedTokens"`
				TotalCacheTokens int64   `json:"totalCacheTokens"`
				CacheHitRate     float64 `json:"cacheHitRate"`
			} `json:"models"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.FromMs != 1600000000000 {
		t.Fatalf("fromMs = %d", response.FromMs)
	}
	if response.ToMs != 1800000000000 {
		t.Fatalf("toMs = %d", response.ToMs)
	}
	if len(response.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(response.Items))
	}
	item := response.Items[0]
	if item.UserName != "zhangsan" || item.Email != "zs@example.com" {
		t.Fatalf("user/email = %q/%q", item.UserName, item.Email)
	}
	if len(item.Models) != 1 || item.Models[0].Model != "gpt-4" {
		t.Fatalf("models = %+v", item.Models)
	}
	m := item.Models[0]
	if m.TotalTokens != 100 || m.Requests != 2 {
		t.Fatalf("model stats = %+v", m)
	}
	if m.FailedRequests != 1 {
		t.Fatalf("model failedRequests = %d, want 1", m.FailedRequests)
	}
	if item.FailedRequests != 1 {
		t.Fatalf("item failedRequests = %d, want 1", item.FailedRequests)
	}
}

func TestEnterpriseUsageReportCSVResponse(t *testing.T) {
	cfg := config.Config{
		DBPath:      filepath.Join(t.TempDir(), "usage.sqlite"),
		Queue:       "usage",
		PopSide:     "right",
		CORSOrigins: []string{"*"},
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if err := db.SaveSetup(ctx, store.Setup{
		CPAUpstreamURL: "http://example.test",
		ManagementKey:  "management-key",
		Queue:          "usage",
		PopSide:        "right",
	}); err != nil {
		t.Fatalf("save setup: %v", err)
	}
	if err := db.UpsertEnterpriseDepartments(ctx, []store.EnterpriseDepartment{
		{ID: "dept_sh", Name: "上海总部", Prefix: "sh", SortOrder: 1, Enabled: true},
	}); err != nil {
		t.Fatalf("upsert departments: %v", err)
	}
	if err := db.UpsertEnterpriseKeyBindings(ctx, []store.EnterpriseKeyBinding{
		{
			APIKey:               "key-test",
			UserName:             "testuser",
			DepartmentID:         "dept_sh",
			Email:                "test@example.com",
			Source:               "manual",
			DepartmentResolvedBy: "manual",
		},
	}); err != nil {
		t.Fatalf("upsert key bindings: %v", err)
	}
	bindings, err := db.LoadEnterpriseKeyBindings(ctx)
	if err != nil {
		t.Fatalf("load bindings: %v", err)
	}
	var hash string
	for _, b := range bindings {
		hash = b.APIKeyHash
		break
	}
	if hash == "" {
		t.Fatalf("could not resolve key hash")
	}
	if _, err := db.InsertEvents(ctx, []usage.Event{
		{
			EventHash:   "csv-e-1",
			TimestampMS: 1_700_000_000_000,
			Timestamp:   "2023-11-14T22:13:20Z",
			Model:       "gpt-4",
			Endpoint:    "POST /v1/chat/completions",
			APIKeyHash:  hash,
			TotalTokens: 100,
			CreatedAtMS: 1_700_000_000_001,
		},
		{
			EventHash:   "csv-e-2",
			TimestampMS: 1_700_000_000_001,
			Timestamp:   "2023-11-14T22:13:21Z",
			Model:       "gpt-3.5-turbo",
			Endpoint:    "POST /v1/chat/completions",
			APIKeyHash:  hash,
			TotalTokens: 50,
			CreatedAtMS: 1_700_000_000_002,
		},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	handler := New(cfg, db, collector.NewManager(cfg, db, nil, collector.AlertConfig{})).Handler()
	req := httptest.NewRequest(
		http.MethodGet,
		"/v0/management/enterprise/usage-report?fromMs=1600000000000&toMs=1800000000000&format=csv",
		nil,
	)
	req.Header.Set("Authorization", "Bearer management-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/csv") {
		t.Fatalf("Content-Type = %q, want text/csv", ct)
	}
	if rr.Header().Get("Content-Disposition") == "" {
		t.Fatalf("missing Content-Disposition header")
	}
	body := rr.Body.String()
	if !strings.Contains(body, "department_name") {
		t.Fatalf("CSV missing header row: %s", body)
	}
	if !strings.Contains(body, "gpt-4") || !strings.Contains(body, "gpt-3.5-turbo") {
		t.Fatalf("CSV missing model rows: %s", body)
	}
	if !strings.Contains(body, "test@example.com") {
		t.Fatalf("CSV missing email: %s", body)
	}
}

func TestEnterpriseUsageReportValidation(t *testing.T) {
	handler := newTestHandler(t, "http://example.test", true)

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantErr    string
	}{
		{
			name:       "missing fromMs",
			query:      "toMs=1800000000000",
			wantStatus: http.StatusBadRequest,
			wantErr:    "fromMs is required",
		},
		{
			name:       "missing toMs",
			query:      "fromMs=1600000000000",
			wantStatus: http.StatusBadRequest,
			wantErr:    "toMs is required",
		},
		{
			name:       "invalid fromMs",
			query:      "fromMs=abc&toMs=1800000000000",
			wantStatus: http.StatusBadRequest,
			wantErr:    "fromMs must be an integer",
		},
		{
			name:       "fromMs greater than toMs",
			query:      "fromMs=1800000000000&toMs=1600000000000",
			wantStatus: http.StatusBadRequest,
			wantErr:    "fromMs must be less than or equal to toMs",
		},
		{
			name:       "invalid format",
			query:      "fromMs=1600000000000&toMs=1800000000000&format=pdf",
			wantStatus: http.StatusBadRequest,
			wantErr:    "format must be json or csv",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v0/management/enterprise/usage-report?"+tt.query, nil)
			req.Header.Set("Authorization", "Bearer management-key")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), tt.wantErr) {
				t.Fatalf("body = %s, want error containing %q", rr.Body.String(), tt.wantErr)
			}
		})
	}
}

func TestEnterpriseUsageReportAuthFailure(t *testing.T) {
	handler := newTestHandler(t, "http://example.test", true)
	req := httptest.NewRequest(
		http.MethodGet,
		"/v0/management/enterprise/usage-report?fromMs=1600000000000&toMs=1800000000000",
		nil,
	)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s, want 401", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid management key") {
		t.Fatalf("body = %s, want auth error", rr.Body.String())
	}
}
