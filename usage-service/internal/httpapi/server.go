package httpapi

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/collector"
	"github.com/seakee/cpa-manager/usage-service/internal/config"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/usage"

	"encoding/csv"
)

//go:embed web/management.html
var embeddedPanel embed.FS

type Server struct {
	cfg       config.Config
	store     *store.Store
	collector *collector.Manager
	startedAt int64
}

type setupSource string

const serviceID = "cpa-manager"

const (
	setupSourceNone setupSource = ""
	setupSourceEnv  setupSource = "env"
	setupSourceDB   setupSource = "db"
)

const maxUsageImportBytes int64 = 64 * 1024 * 1024

const modelPriceSyncSource = "litellm"

var modelPriceSyncURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

type setupRequest struct {
	CPAUpstreamURL               string `json:"cpaBaseUrl"`
	ManagementKey                string `json:"managementKey"`
	CollectorMode                string `json:"collectorMode"`
	Queue                        string `json:"queue"`
	PopSide                      string `json:"popSide"`
	BatchSize                    int    `json:"batchSize"`
	PollIntervalMS               int    `json:"pollIntervalMs"`
	QueryLimit                   int    `json:"queryLimit"`
	TLSSkipVerify                bool   `json:"tlsSkipVerify"`
	EnsureUsageStatisticsEnabled *bool  `json:"ensureUsageStatisticsEnabled"`
	RequestMonitoringEnabled     *bool  `json:"requestMonitoringEnabled"`
}

type managerConfigResponse struct {
	Config   store.ManagerConfig `json:"config"`
	Source   string              `json:"source"`
	CPAUsage *cpaUsageConfig     `json:"cpaUsage,omitempty"`
}

type cpaUsageConfig struct {
	UsageStatisticsEnabled          bool `json:"usageStatisticsEnabled"`
	RedisUsageQueueRetentionSeconds int  `json:"redisUsageQueueRetentionSeconds"`
	RetentionSourceDefault          bool `json:"retentionSourceDefault"`
}

type modelPricesRequest struct {
	Prices map[string]store.ModelPrice `json:"prices"`
}

type modelPricesSyncRequest struct {
	Models []string `json:"models"`
}

type apiKeyAliasesRequest struct {
	Items []store.APIKeyAlias `json:"items"`
}

type quotaConfigRequest struct {
	Enabled   *bool                   `json:"enabled"`
	Default   *store.SpendLimit       `json:"default"`
	Overrides []store.SpendLimitEntry `json:"overrides"`
}

type downgradeQuotaConfigRequest struct {
	Enabled       *bool                   `json:"enabled"`
	Default       *store.SpendLimit       `json:"default"`
	Overrides     []store.SpendLimitEntry `json:"overrides"`
	FallbackModel *string                 `json:"fallback_model"`
}

type enterpriseDepartmentsRequest struct {
	Items []store.EnterpriseDepartment `json:"items"`
}

type enterpriseKeyBindingsRequest struct {
	Items []store.EnterpriseKeyBinding `json:"items"`
}

type enterpriseImportHistoryRequest struct {
	Item store.EnterpriseImportHistory `json:"item"`
}

type createEnterpriseKeyBindingRequest struct {
	UserName     string `json:"userName"`
	DepartmentID string `json:"departmentId"`
	APIKey       string `json:"apiKey"`
	Email        string `json:"email,omitempty"`
}

type deleteEnterpriseKeyBindingsRequest struct {
	APIKeys []string `json:"apiKeys"`
}

type updateEnterpriseKeyBindingRequest struct {
	UserName     string `json:"userName"`
	DepartmentID string `json:"departmentId"`
	Email        string `json:"email,omitempty"`
}

type keyBindingGenerateRequest struct {
	CSV      string `json:"csv"`
	FileName string `json:"fileName"`
}

type keyBindingImportRequest struct {
	Items    []usage.KeyGenPreviewItem `json:"items"`
	FileName string                    `json:"fileName"`
}

func New(cfg config.Config, store *store.Store, collector *collector.Manager) *Server {
	return &Server{
		cfg:       cfg,
		store:     store,
		collector: collector,
		startedAt: time.Now().UnixMilli(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.withCORS(s.handleHealth))
	mux.HandleFunc("/status", s.withCORS(s.handleStatus))
	mux.HandleFunc("/usage-service/info", s.withCORS(s.handleInfo))
	mux.HandleFunc("/usage-service/config", s.withCORS(s.handleManagerConfig))
	mux.HandleFunc("/setup", s.withCORS(s.handleSetup))
	mux.HandleFunc("/management.html", s.handlePanel)
	mux.HandleFunc("/", s.handleRoot)
	return mux
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.writeCORS(w, r)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v0/management/model-prices") {
		s.withCORS(s.handleModelPrices)(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v0/management/api-key-aliases") {
		s.withCORS(s.handleAPIKeyAliases)(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v0/management/quota/downgrade-config") {
		s.withCORS(s.handleQuotaDowngradeConfig)(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v0/management/quota/config") {
		s.withCORS(s.handleQuotaConfig)(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v0/management/alert/config") {
		s.withCORS(s.handleAlertConfig)(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v0/management/enterprise/departments") {
		s.withCORS(s.handleEnterpriseDepartments)(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v0/management/enterprise/key-bindings") {
		s.withCORS(s.handleEnterpriseKeyBindings)(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v0/management/enterprise/import-history") {
		s.withCORS(s.handleEnterpriseImportHistory)(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v0/management/enterprise/usage-report") {
		s.withCORS(s.handleEnterpriseUsageReport)(w, r)
		return
	}
	cleanUsagePath := strings.TrimRight(r.URL.Path, "/")
	if cleanUsagePath == "/v0/management/usage" || strings.HasPrefix(cleanUsagePath, "/v0/management/usage/") {
		s.withCORS(s.handleUsage)(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v0/management/") {
		s.withCORS(s.handleProxy)(w, r)
		return
	}
	if isModelListProxyPath(r.URL.Path) {
		s.withCORS(s.handleModelListProxy)(w, r)
		return
	}
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/management.html", http.StatusTemporaryRedirect)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": serviceID})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service":   serviceID,
		"mode":      "embedded",
		"startedAt": s.startedAt,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.authorizeIfConfigured(w, r) {
		return
	}
	events, deadLetters, err := s.store.Counts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	status := s.collector.Status()
	status.DeadLetters = deadLetters
	writeJSON(w, http.StatusOK, map[string]any{
		"service":     serviceID,
		"dbPath":      s.cfg.DBPath,
		"events":      events,
		"deadLetters": deadLetters,
		"collector":   status,
	})
}

func (s *Server) handleManagerConfig(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIfConfigured(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		cfg, source, _, err := s.resolveManagerConfigWithSource(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var cpaUsage *cpaUsageConfig
		if r.URL.Query().Get("includeCpaUsage") != "false" &&
			cfg.CPAConnection.CPABaseURL != "" &&
			cfg.CPAConnection.ManagementKey != "" {
			if usageCfg, err := fetchCPAUsageConfig(
				r.Context(),
				cfg.CPAConnection.CPABaseURL,
				cfg.CPAConnection.ManagementKey,
			); err == nil {
				cpaUsage = &usageCfg
			}
		}
		writeJSON(w, http.StatusOK, managerConfigResponse{
			Config:   cfg,
			Source:   string(source),
			CPAUsage: cpaUsage,
		})
	case http.MethodPut:
		var req struct {
			Config store.ManagerConfig `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		current, source, _, err := s.resolveManagerConfigWithSource(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		next := s.mergeSubmittedManagerConfig(current, req.Config)
		if source == setupSourceEnv && managerConfigConnectionDiffers(current, next) {
			writeError(w, http.StatusConflict, errors.New("connection setup is managed by environment variables"))
			return
		}
		if next.CPAConnection.CPABaseURL != "" || next.CPAConnection.ManagementKey != "" {
			if next.CPAConnection.CPABaseURL == "" || next.CPAConnection.ManagementKey == "" {
				writeError(w, http.StatusBadRequest, errors.New("cpaBaseUrl and managementKey are required"))
				return
			}
			if err := validateManagementAPI(
				r.Context(),
				next.CPAConnection.CPABaseURL,
				next.CPAConnection.ManagementKey,
			); err != nil {
				writeError(w, http.StatusBadGateway, err)
				return
			}
			if managerCollectorEnabled(next) {
				if err := validateCollectorAgainstCPA(r.Context(), next); err != nil {
					writeError(w, http.StatusBadRequest, err)
					return
				}
				if err := setCPAUsageStatisticsEnabled(
					r.Context(),
					next.CPAConnection.CPABaseURL,
					next.CPAConnection.ManagementKey,
					true,
				); err != nil {
					writeError(w, http.StatusBadGateway, err)
					return
				}
			}
		} else if managerCollectorEnabled(next) {
			writeError(w, http.StatusBadRequest, errors.New("cpaBaseUrl and managementKey are required when request monitoring is enabled"))
			return
		}
		if next.CPAConnection.CPABaseURL == "" || next.CPAConnection.ManagementKey == "" {
			if err := s.store.SaveManagerConfig(r.Context(), next); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			s.collector.Stop()
			writeJSON(w, http.StatusOK, managerConfigResponse{
				Config: next,
				Source: string(setupSourceDB),
			})
			return
		}
		if err := s.store.SaveManagerConfig(r.Context(), next); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		setup := setupFromManagerConfig(next)
		if err := s.store.SaveSetup(r.Context(), setup); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if managerCollectorEnabled(next) {
			s.collector.Start(context.Background(), runtimeConfigFromManagerConfig(next))
		} else {
			s.collector.Stop()
		}
		writeJSON(w, http.StatusOK, managerConfigResponse{
			Config: next,
			Source: string(setupSourceDB),
		})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req.CPAUpstreamURL = normalizeBaseURL(req.CPAUpstreamURL)
	req.ManagementKey = strings.TrimSpace(req.ManagementKey)
	req.CollectorMode = collectorMode(req.CollectorMode)
	if req.Queue == "" {
		req.Queue = s.cfg.Queue
	}
	if req.PopSide == "" {
		req.PopSide = s.cfg.PopSide
	}
	req.PopSide = normalizePopSide(req.PopSide, s.cfg.PopSide)
	req.BatchSize = positiveOrDefault(req.BatchSize, s.cfg.BatchSize, 100)
	req.PollIntervalMS = positiveOrDefault(req.PollIntervalMS, int(s.cfg.PollInterval/time.Millisecond), 500)
	req.QueryLimit = positiveOrDefault(req.QueryLimit, s.cfg.QueryLimit, 50000)
	requestMonitoringEnabled := setupRequestMonitoringEnabled(req)
	if req.CPAUpstreamURL == "" || req.ManagementKey == "" {
		writeError(w, http.StatusBadRequest, errors.New("cpaBaseUrl and managementKey are required"))
		return
	}
	managementAPIValidated := false
	if existing, source, ok, err := s.resolveSetupWithSource(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	} else if source == setupSourceEnv && setupDiffers(existing, req) {
		writeError(w, http.StatusConflict, errors.New("setup is managed by environment variables"))
		return
	} else if ok && existing.ManagementKey != "" &&
		!authMatches(r, existing.ManagementKey) &&
		req.ManagementKey != existing.ManagementKey {
		if normalizeBaseURL(existing.CPAUpstreamURL) != req.CPAUpstreamURL {
			writeError(w, http.StatusUnauthorized, errors.New("invalid management key for existing setup"))
			return
		}
		if err := validateManagementAPI(r.Context(), req.CPAUpstreamURL, req.ManagementKey); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		managementAPIValidated = true
	}
	if !managementAPIValidated {
		if err := validateManagementAPI(r.Context(), req.CPAUpstreamURL, req.ManagementKey); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
	}
	managerCfg := s.defaultManagerConfig()
	if existingManagerCfg, _, ok, err := s.resolveManagerConfigWithSource(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	} else if ok {
		managerCfg = existingManagerCfg
	}
	managerCfg.CPAConnection.CPABaseURL = req.CPAUpstreamURL
	managerCfg.CPAConnection.ManagementKey = req.ManagementKey
	managerCfg.Collector.Enabled = boolPtr(requestMonitoringEnabled)
	managerCfg.Collector.CollectorMode = req.CollectorMode
	managerCfg.Collector.Queue = req.Queue
	managerCfg.Collector.PopSide = req.PopSide
	managerCfg.Collector.BatchSize = req.BatchSize
	managerCfg.Collector.PollIntervalMS = req.PollIntervalMS
	managerCfg.Collector.QueryLimit = req.QueryLimit
	managerCfg.Collector.TLSSkipVerify = req.TLSSkipVerify
	if requestMonitoringEnabled {
		if err := validateCollectorAgainstCPA(r.Context(), managerCfg); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	ensureUsageStatisticsEnabled := requestMonitoringEnabled
	if req.EnsureUsageStatisticsEnabled != nil {
		ensureUsageStatisticsEnabled = requestMonitoringEnabled && *req.EnsureUsageStatisticsEnabled
	}
	if ensureUsageStatisticsEnabled {
		if err := setCPAUsageStatisticsEnabled(r.Context(), req.CPAUpstreamURL, req.ManagementKey, true); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
	}
	setup := store.Setup{
		CPAUpstreamURL: req.CPAUpstreamURL,
		ManagementKey:  req.ManagementKey,
		Queue:          req.Queue,
		PopSide:        req.PopSide,
	}
	if err := s.store.SaveSetup(r.Context(), setup); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.store.SaveManagerConfig(r.Context(), managerCfg); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if requestMonitoringEnabled {
		s.collector.Start(context.Background(), runtimeConfigFromManagerConfig(managerCfg))
	} else {
		s.collector.Stop()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "upstream": setup.CPAUpstreamURL})
}

func (s *Server) handleModelPrices(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIfConfigured(w, r) {
		return
	}

	path := strings.TrimRight(r.URL.Path, "/")
	switch {
	case path == "/v0/management/model-prices" && r.Method == http.MethodGet:
		prices, err := s.store.LoadModelPrices(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"prices": prices})
	case path == "/v0/management/model-prices" && r.Method == http.MethodPut:
		var req modelPricesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.Prices == nil {
			writeError(w, http.StatusBadRequest, errors.New("prices are required"))
			return
		}
		if err := s.store.SaveModelPrices(r.Context(), req.Prices); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		prices, err := s.store.LoadModelPrices(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"prices": prices})
	case path == "/v0/management/model-prices/sync" && r.Method == http.MethodPost:
		var req modelPricesSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		remotePrices, skipped, err := fetchLiteLLMModelPrices(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		selectedPrices := selectModelPrices(remotePrices, req.Models)
		result, err := s.store.UpsertSyncedModelPrices(r.Context(), selectedPrices)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		prices, err := s.store.LoadModelPrices(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"source":   modelPriceSyncSource,
			"imported": result.Imported,
			"skipped":  result.Skipped + skipped,
			"prices":   prices,
		})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleAPIKeyAliases(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIfConfigured(w, r) {
		return
	}

	path := strings.TrimRight(r.URL.Path, "/")
	const basePath = "/v0/management/api-key-aliases"
	switch {
	case path == basePath && r.Method == http.MethodGet:
		aliases, err := s.store.LoadAPIKeyAliases(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": aliases})
	case path == basePath && r.Method == http.MethodPut:
		var req apiKeyAliasesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.Items == nil {
			writeError(w, http.StatusBadRequest, errors.New("api key aliases are required"))
			return
		}
		if err := s.store.UpsertAPIKeyAliases(r.Context(), req.Items); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		aliases, err := s.store.LoadAPIKeyAliases(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": aliases})
	case strings.HasPrefix(path, basePath+"/") && r.Method == http.MethodDelete:
		apiKeyHash := strings.TrimPrefix(path, basePath+"/")
		if err := s.store.DeleteAPIKeyAlias(r.Context(), apiKeyHash); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleQuotaConfig(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIfConfigured(w, r) {
		return
	}
	if strings.TrimRight(r.URL.Path, "/") != "/v0/management/quota/config" {
		methodNotAllowed(w)
		return
	}

	switch r.Method {
	case http.MethodGet:
		cfg, _, err := s.loadPauseSpendLimitConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.writeSpendLimitConfig(w, cfg)
	case http.MethodPut:
		current, _, err := s.loadPauseSpendLimitConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var req quotaConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.Enabled != nil {
			current.Enabled = *req.Enabled
		}
		if req.Default != nil {
			current.Default = *req.Default
			current.DailyCents = 0
			current.WeeklyCents = 0
		}
		if req.Overrides != nil {
			current.Overrides = normalizeSpendLimitOverrides(req.Overrides)
		}
		if err := s.saveSpendLimitConfigWithSync(r.Context(), current, false); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		s.writeSpendLimitConfig(w, current)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleQuotaDowngradeConfig(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIfConfigured(w, r) {
		return
	}
	if strings.TrimRight(r.URL.Path, "/") != "/v0/management/quota/downgrade-config" {
		methodNotAllowed(w)
		return
	}

	switch r.Method {
	case http.MethodGet:
		cfg, _, err := s.loadDowngradeSpendLimitConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.writeDowngradeSpendLimitConfig(w, cfg)
	case http.MethodPut:
		current, _, err := s.loadDowngradeSpendLimitConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var req downgradeQuotaConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.Enabled != nil {
			current.Enabled = *req.Enabled
		}
		if req.Default != nil {
			current.Default = *req.Default
			current.DailyCents = 0
			current.WeeklyCents = 0
		}
		if req.Overrides != nil {
			current.Overrides = normalizeSpendLimitOverrides(req.Overrides)
		}
		if req.FallbackModel != nil {
			current.FallbackModel = strings.TrimSpace(*req.FallbackModel)
		}
		if strings.TrimSpace(current.FallbackModel) == "" {
			current.FallbackModel = store.DefaultFallbackModel
		}
		if current.Enabled {
			setup, ok, setupErr := s.resolveSetup(r.Context())
			if setupErr != nil {
				writeError(w, http.StatusInternalServerError, setupErr)
				return
			}
			if !ok || strings.TrimSpace(setup.CPAUpstreamURL) == "" || strings.TrimSpace(setup.ManagementKey) == "" {
				writeError(w, http.StatusServiceUnavailable, errors.New("CPA connection is required for downgrade mode"))
				return
			}
			if validateErr := collector.ValidateFallbackModel(setup.CPAUpstreamURL, setup.ManagementKey, current.FallbackModel); validateErr != nil {
				status := http.StatusBadGateway
				if remoteStatus, isRemote := collector.QuotaHTTPStatus(validateErr); isRemote && remoteStatus >= 400 && remoteStatus < 500 {
					status = http.StatusBadRequest
				}
				writeError(w, status, validateErr)
				return
			}
		}
		if err := s.saveSpendLimitConfigWithSync(r.Context(), current, true); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		s.writeDowngradeSpendLimitConfig(w, current)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) saveSpendLimitConfigWithSync(ctx context.Context, cfg store.SpendLimitConfig, downgrade bool) error {
	if s.collector == nil {
		if downgrade {
			return s.store.SaveDowngradeSpendLimitConfig(ctx, cfg)
		}
		return s.store.SavePauseSpendLimitConfig(ctx, cfg)
	}
	saved := false
	err := s.collector.WithSpendLimitSync(func() error {
		var err error
		if downgrade {
			err = s.store.SaveDowngradeSpendLimitConfig(ctx, cfg)
		} else {
			err = s.store.SavePauseSpendLimitConfig(ctx, cfg)
		}
		if err == nil {
			saved = true
		}
		return err
	})
	if err != nil {
		if saved {
			return fmt.Errorf("quota synchronization failed: %w", err)
		}
		return err
	}
	return nil
}

// handleAlertConfig handles GET/PUT /v0/management/alert/config for SMTP and alert settings.
func (s *Server) handleAlertConfig(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIfConfigured(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		cfg, _, err := s.store.LoadAlertConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var req store.AlertConfigStored
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		// Preserve existing password if masked value or empty (no change requested)
		if req.SMTPPassword == "******" || req.SMTPPassword == "" {
			existing, ok, err := s.store.LoadAlertConfigWithPassword(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			if ok {
				req.SMTPPassword = existing.SMTPPassword
			}
		}
		if err := s.store.SaveAlertConfig(r.Context(), req); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		// Return with masked password
		resp := req
		if resp.SMTPPassword != "" {
			resp.SMTPPassword = "******"
		}
		writeJSON(w, http.StatusOK, resp)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) loadPauseSpendLimitConfig(ctx context.Context) (store.SpendLimitConfig, bool, error) {
	cfg, ok, err := s.store.LoadPauseSpendLimitConfig(ctx)
	if err != nil {
		return store.SpendLimitConfig{}, false, err
	}
	if !ok {
		cfg.Default = store.SpendLimit{}
		cfg.Overrides = []store.SpendLimitEntry{}
	}
	return cfg, ok, nil
}

func (s *Server) loadDowngradeSpendLimitConfig(ctx context.Context) (store.SpendLimitConfig, bool, error) {
	cfg, ok, err := s.store.LoadDowngradeSpendLimitConfig(ctx)
	if err != nil {
		return store.SpendLimitConfig{}, false, err
	}
	if !ok {
		cfg.Default = store.SpendLimit{}
		cfg.Overrides = []store.SpendLimitEntry{}
		cfg.FallbackModel = store.DefaultFallbackModel
	}
	return cfg, ok, nil
}

func (s *Server) writeSpendLimitConfig(w http.ResponseWriter, cfg store.SpendLimitConfig) {
	overrides := cfg.Overrides
	if overrides == nil {
		overrides = []store.SpendLimitEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":   cfg.Enabled,
		"db_path":   s.cfg.DBPath,
		"default":   cfg.DefaultLimit(),
		"overrides": overrides,
	})
}

func (s *Server) writeDowngradeSpendLimitConfig(w http.ResponseWriter, cfg store.SpendLimitConfig) {
	overrides := cfg.Overrides
	if overrides == nil {
		overrides = []store.SpendLimitEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":        cfg.Enabled,
		"db_path":        s.cfg.DBPath,
		"default":        cfg.DefaultLimit(),
		"overrides":      overrides,
		"fallback_model": cfg.EffectiveFallbackModel(),
	})
}

func normalizeSpendLimitOverrides(entries []store.SpendLimitEntry) []store.SpendLimitEntry {
	out := make([]store.SpendLimitEntry, 0, len(entries))
	for _, entry := range entries {
		applyTo := strings.TrimSpace(entry.ApplyTo)
		applyValue := strings.ToLower(strings.TrimSpace(entry.ApplyValue))
		if applyTo != "api-key" || applyValue == "" {
			continue
		}
		out = append(out, store.SpendLimitEntry{
			ApplyTo:     applyTo,
			ApplyValue:  applyValue,
			DailyCents:  entry.DailyCents,
			WeeklyCents: entry.WeeklyCents,
		})
	}
	return out
}

func fetchLiteLLMModelPrices(ctx context.Context) (map[string]store.ModelPrice, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelPriceSyncURL, nil)
	if err != nil {
		return nil, 0, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, 0, errors.New("model price sync failed: " + res.Status)
	}

	var payload map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, 0, err
	}

	prices := map[string]store.ModelPrice{}
	skipped := 0
	for model, raw := range payload {
		if model == "" || model == "sample_spec" {
			skipped++
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(raw, &entry); err != nil {
			skipped++
			continue
		}

		prompt, hasPrompt := readFloat(entry, "input_cost_per_token")
		completion, hasCompletion := readFloat(entry, "output_cost_per_token")
		cache, hasCache := readFloat(entry, "cache_read_input_token_cost")
		if !hasCache {
			cache, hasCache = readFloat(entry, "cache_read_cost_per_token")
		}
		if !hasPrompt && !hasCompletion {
			skipped++
			continue
		}
		if !hasPrompt {
			prompt = 0
		}
		if !hasCompletion {
			completion = 0
		}
		if !hasCache {
			cache = prompt
		}

		prices[model] = store.ModelPrice{
			Prompt:        prompt * 1_000_000,
			Completion:    completion * 1_000_000,
			Cache:         cache * 1_000_000,
			Source:        modelPriceSyncSource,
			SourceModelID: model,
			RawJSON:       string(raw),
		}
	}
	return prices, skipped, nil
}

func selectModelPrices(prices map[string]store.ModelPrice, models []string) map[string]store.ModelPrice {
	wanted := make([]string, 0, len(models))
	seen := map[string]struct{}{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		wanted = append(wanted, model)
	}
	if len(wanted) == 0 {
		return prices
	}

	selected := map[string]store.ModelPrice{}
	for _, model := range wanted {
		if price, ok := prices[model]; ok {
			selected[model] = price
			continue
		}
		if price, ok := findSuffixModelPrice(prices, model); ok {
			selected[model] = price
		}
	}
	return selected
}

func findSuffixModelPrice(prices map[string]store.ModelPrice, model string) (store.ModelPrice, bool) {
	suffix := "/" + model
	var match store.ModelPrice
	matchedKey := ""
	for key, price := range prices {
		if !strings.HasSuffix(key, suffix) {
			continue
		}
		if matchedKey == "" || len(key) < len(matchedKey) {
			matchedKey = key
			match = price
		}
	}
	return match, matchedKey != ""
}

func readFloat(entry map[string]any, key string) (float64, bool) {
	value, ok := entry[key]
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIfConfigured(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		if strings.HasSuffix(r.URL.Path, "/export") {
			s.handleUsageExport(w, r)
			return
		}
		filters, err := readUsageFilters(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		events, err := s.store.RecentEventsFiltered(
			r.Context(),
			s.cfg.QueryLimit,
			filters.FromMS,
			filters.ToMS,
			filters.APIKeyHash,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, usage.BuildPayload(events))
	case http.MethodPost:
		if strings.HasSuffix(r.URL.Path, "/import") {
			s.handleUsageImport(w, r)
			return
		}
		methodNotAllowed(w)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleEnterpriseDepartments(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIfConfigured(w, r) {
		return
	}

	path := strings.TrimRight(r.URL.Path, "/")
	const basePath = "/v0/management/enterprise/departments"
	switch {
	case path == basePath && r.Method == http.MethodGet:
		items, err := s.store.LoadEnterpriseDepartments(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case path == basePath && r.Method == http.MethodPut:
		var req enterpriseDepartmentsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.Items == nil {
			writeError(w, http.StatusBadRequest, errors.New("enterprise departments are required"))
			return
		}
		if err := s.store.UpsertEnterpriseDepartments(r.Context(), req.Items); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		items, err := s.store.LoadEnterpriseDepartments(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case strings.HasPrefix(path, basePath+"/") && r.Method == http.MethodDelete:
		departmentID := strings.TrimPrefix(path, basePath+"/")
		if strings.TrimSpace(departmentID) == "" {
			writeError(w, http.StatusBadRequest, errors.New("department id is required"))
			return
		}
		if err := s.store.DeleteEnterpriseDepartment(r.Context(), departmentID); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		items, err := s.store.LoadEnterpriseDepartments(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleEnterpriseKeyBindings(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIfConfigured(w, r) {
		return
	}

	path := strings.TrimRight(r.URL.Path, "/")
	const basePath = "/v0/management/enterprise/key-bindings"
	switch {
	case path == basePath && r.Method == http.MethodGet:
		setup, ok, err := s.resolveSetup(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if ok {
			baseURL := normalizeBaseURL(setup.CPAUpstreamURL)
			managementKey := strings.TrimSpace(setup.ManagementKey)
			if baseURL != "" && managementKey != "" {
				if err := s.mergeCPAAPIKeysIntoEnterpriseStore(r.Context(), baseURL, managementKey); err != nil {
					writeError(w, http.StatusBadGateway, err)
					return
				}
			}
		}
		items, err := s.store.LoadEnterpriseKeyBindings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case path == basePath && r.Method == http.MethodPut:
		var req enterpriseKeyBindingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.Items == nil {
			writeError(w, http.StatusBadRequest, errors.New("enterprise key bindings are required"))
			return
		}
		if err := s.store.UpsertEnterpriseKeyBindings(r.Context(), req.Items); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.syncEnterpriseKeysToCPAConfig(r.Context()); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		items, err := s.store.LoadEnterpriseKeyBindings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case path == basePath && r.Method == http.MethodPost:
		s.handleCreateEnterpriseKeyBinding(w, r)
	case path == basePath+"/generate" && r.Method == http.MethodPost:
		s.handleGenerateEnterpriseKeyBindings(w, r)
	case path == basePath+"/import" && r.Method == http.MethodPost:
		s.handleImportEnterpriseKeyBindings(w, r)
	case path == basePath && r.Method == http.MethodDelete:
		s.handleBatchDeleteEnterpriseKeyBindings(w, r)
	case strings.HasPrefix(path, basePath+"/") && r.Method == http.MethodPatch:
		s.handleUpdateEnterpriseKeyBinding(w, r)
	case strings.HasPrefix(path, basePath+"/") && r.Method == http.MethodDelete:
		apiKey, err := url.PathUnescape(strings.TrimPrefix(path, basePath+"/"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.store.DeleteEnterpriseKeyBinding(r.Context(), apiKey); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.syncEnterpriseKeysToCPAConfig(r.Context()); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleCreateEnterpriseKeyBinding(w http.ResponseWriter, r *http.Request) {
	var req createEnterpriseKeyBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.UserName) == "" || strings.TrimSpace(req.DepartmentID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("userName and departmentId are required"))
		return
	}
	departments, err := s.store.LoadEnterpriseDepartments(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var selected *store.EnterpriseDepartment
	for i := range departments {
		if departments[i].ID == strings.TrimSpace(req.DepartmentID) {
			selected = &departments[i]
			break
		}
	}
	if selected == nil {
		writeError(w, http.StatusBadRequest, errors.New("department not found"))
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if key == "" {
		key, err = usage.GenerateAPIKeyForUser(selected.Prefix, req.UserName)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	item := store.EnterpriseKeyBinding{
		APIKey:               key,
		UserName:             strings.TrimSpace(req.UserName),
		DepartmentID:         strings.TrimSpace(req.DepartmentID),
		Email:                strings.TrimSpace(req.Email),
		Source:               "manual",
		DepartmentResolvedBy: "manual",
	}
	if err := s.store.UpsertEnterpriseKeyBindings(r.Context(), []store.EnterpriseKeyBinding{item}); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.syncEnterpriseKeysToCPAConfig(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	items, err := s.store.LoadEnterpriseKeyBindings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, binding := range items {
		if binding.APIKey == key {
			writeJSON(w, http.StatusOK, binding)
			return
		}
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleBatchDeleteEnterpriseKeyBindings(w http.ResponseWriter, r *http.Request) {
	var req deleteEnterpriseKeyBindingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.APIKeys) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("apiKeys are required"))
		return
	}
	if err := s.store.DeleteEnterpriseKeyBindings(r.Context(), req.APIKeys); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.syncEnterpriseKeysToCPAConfig(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleUpdateEnterpriseKeyBinding(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimRight(r.URL.Path, "/")
	const basePath = "/v0/management/enterprise/key-bindings"
	apiKey, err := url.PathUnescape(strings.TrimPrefix(path, basePath+"/"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(apiKey) == "" {
		writeError(w, http.StatusBadRequest, errors.New("apiKey is required"))
		return
	}
	var req updateEnterpriseKeyBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.UserName) == "" || strings.TrimSpace(req.DepartmentID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("userName and departmentId are required"))
		return
	}
	items, err := s.store.LoadEnterpriseKeyBindings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var current *store.EnterpriseKeyBinding
	for i := range items {
		if items[i].APIKey == strings.TrimSpace(apiKey) {
			current = &items[i]
			break
		}
	}
	if current == nil {
		writeError(w, http.StatusNotFound, errors.New("api key not found"))
		return
	}
	departments, err := s.store.LoadEnterpriseDepartments(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	departmentExists := false
	for _, dept := range departments {
		if dept.ID == strings.TrimSpace(req.DepartmentID) {
			departmentExists = true
			break
		}
	}
	if !departmentExists {
		writeError(w, http.StatusBadRequest, errors.New("department not found"))
		return
	}
	updated := *current
	updated.UserName = strings.TrimSpace(req.UserName)
	updated.DepartmentID = strings.TrimSpace(req.DepartmentID)
	updated.Email = strings.TrimSpace(req.Email)
	updated.Source = "manual"
	updated.DepartmentResolvedBy = "manual"
	updated.UpdatedAtMS = time.Now().UnixMilli()
	if err := s.store.UpsertEnterpriseKeyBindings(r.Context(), []store.EnterpriseKeyBinding{updated}); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.syncEnterpriseKeysToCPAConfig(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	fresh, err := s.store.LoadEnterpriseKeyBindings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, binding := range fresh {
		if binding.APIKey == strings.TrimSpace(apiKey) {
			writeJSON(w, http.StatusOK, binding)
			return
		}
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleGenerateEnterpriseKeyBindings(w http.ResponseWriter, r *http.Request) {
	csvContent, _, err := readKeyBindingCSVRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	departments, err := s.store.LoadEnterpriseDepartments(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	lites := make([]usage.DepartmentLite, 0, len(departments))
	for _, dept := range departments {
		lites = append(lites, usage.DepartmentLite{ID: dept.ID, Name: dept.Name, Prefix: dept.Prefix})
	}
	items, err := usage.ParseCSVPreview(csvContent, lites)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleImportEnterpriseKeyBindings(w http.ResponseWriter, r *http.Request) {
	var req keyBindingImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Items == nil {
		writeError(w, http.StatusBadRequest, errors.New("items are required"))
		return
	}

	existing, err := s.store.LoadEnterpriseKeyBindings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		apiKey := strings.TrimSpace(item.APIKey)
		if apiKey == "" {
			continue
		}
		existingSet[apiKey] = struct{}{}
	}

	passedRows := 0
	warningRows := 0
	errorRows := 0
	type importErrorDetail struct {
		Row    int    `json:"row"`
		Reason string `json:"reason"`
	}
	errorDetails := make([]importErrorDetail, 0)
	toUpsert := make([]store.EnterpriseKeyBinding, 0, len(req.Items))
	for idx, item := range req.Items {
		if item.Status != "ok" {
			errorRows++
			reason := strings.TrimSpace(item.ErrorReason)
			if reason == "" {
				reason = "item status is not ok"
			}
			errorDetails = append(errorDetails, importErrorDetail{Row: idx + 1, Reason: reason})
			continue
		}
		apiKey := strings.TrimSpace(item.GeneratedKey)
		if apiKey == "" {
			errorRows++
			errorDetails = append(errorDetails, importErrorDetail{Row: idx + 1, Reason: "generatedKey is required for ok item"})
			continue
		}
		if _, exists := existingSet[apiKey]; exists {
			warningRows++
			continue
		}
		toUpsert = append(toUpsert, store.EnterpriseKeyBinding{
			APIKey:               apiKey,
			UserName:             strings.TrimSpace(item.UserName),
			Email:                strings.TrimSpace(item.Email),
			DepartmentID:         strings.TrimSpace(item.DepartmentID),
			Source:               "import",
			DepartmentResolvedBy: "csv",
		})
		existingSet[apiKey] = struct{}{}
		passedRows++
	}
	if len(toUpsert) > 0 {
		if err := s.store.UpsertEnterpriseKeyBindings(r.Context(), toUpsert); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.syncEnterpriseKeysToCPAConfig(r.Context()); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
	}

	taskID := fmt.Sprintf("import-%d", time.Now().UnixMilli())
	status := "done"
	if errorRows > 0 && passedRows == 0 {
		status = "partial"
	}
	errorDetailsJSON := ""
	if len(errorDetails) > 0 {
		raw, marshalErr := json.Marshal(errorDetails)
		if marshalErr != nil {
			writeError(w, http.StatusInternalServerError, marshalErr)
			return
		}
		errorDetailsJSON = string(raw)
	}
	if err := s.store.AppendEnterpriseImportHistory(r.Context(), store.EnterpriseImportHistory{
		TaskID:       taskID,
		CSVFileName:  strings.TrimSpace(req.FileName),
		TotalRows:    len(req.Items),
		PassedRows:   passedRows,
		WarningRows:  warningRows,
		ErrorRows:    errorRows,
		ErrorDetails: errorDetailsJSON,
		Status:       status,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"taskId":      taskID,
		"totalRows":   len(req.Items),
		"passedRows":  passedRows,
		"warningRows": warningRows,
		"errorRows":   errorRows,
	})
}

func readKeyBindingCSVRequest(r *http.Request) ([]byte, string, error) {
	contentType := r.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.HasPrefix(mediaType, "multipart/") {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			return nil, "", err
		}
		if file, header, err := r.FormFile("file"); err == nil {
			defer file.Close()
			data, readErr := io.ReadAll(file)
			return data, header.Filename, readErr
		}
		csvText := r.FormValue("csv")
		if strings.TrimSpace(csvText) == "" {
			return nil, "", errors.New("csv is required")
		}
		return []byte(csvText), strings.TrimSpace(r.FormValue("fileName")), nil
	}
	var req keyBindingGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(req.CSV) == "" {
		return nil, "", errors.New("csv is required")
	}
	return []byte(req.CSV), strings.TrimSpace(req.FileName), nil
}

func (s *Server) handleEnterpriseImportHistory(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIfConfigured(w, r) {
		return
	}

	path := strings.TrimRight(r.URL.Path, "/")
	const basePath = "/v0/management/enterprise/import-history"
	switch {
	case path == basePath && r.Method == http.MethodGet:
		limit := 100
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if value, err := strconv.Atoi(raw); err == nil && value > 0 {
				limit = value
			}
		}
		items, err := s.store.LoadEnterpriseImportHistory(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case path == basePath && r.Method == http.MethodPost:
		var req enterpriseImportHistoryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := s.store.AppendEnterpriseImportHistory(r.Context(), req.Item); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

type usageFilters struct {
	FromMS     *int64
	ToMS       *int64
	APIKeyHash string
}

func readUsageFilters(r *http.Request) (usageFilters, error) {
	query := r.URL.Query()
	fromMS, err := parseOptionalInt64Query(query.Get("fromMs"), "fromMs")
	if err != nil {
		return usageFilters{}, err
	}
	toMS, err := parseOptionalInt64Query(query.Get("toMs"), "toMs")
	if err != nil {
		return usageFilters{}, err
	}
	if fromMS != nil && toMS != nil && *fromMS > *toMS {
		return usageFilters{}, errors.New("fromMs must be less than or equal to toMs")
	}
	return usageFilters{
		FromMS:     fromMS,
		ToMS:       toMS,
		APIKeyHash: strings.TrimSpace(query.Get("apiKeyHash")),
	}, nil
}

func parseOptionalInt64Query(raw string, field string) (*int64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer", field)
	}
	return &value, nil
}

func (s *Server) handleEnterpriseUsageReport(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIfConfigured(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	fromMs, err := parseRequiredInt64Query("fromMs", r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	toMs, err := parseRequiredInt64Query("toMs", r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if fromMs > toMs {
		writeError(w, http.StatusBadRequest, errors.New("fromMs must be less than or equal to toMs"))
		return
	}

	format := strings.TrimSpace(r.URL.Query().Get("format"))
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		writeError(w, http.StatusBadRequest, errors.New("format must be json or csv"))
		return
	}

	rows, err := s.store.UsageReport(r.Context(), fromMs, toMs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if format == "csv" {
		s.writeUsageReportCSV(w, rows)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fromMs": fromMs, "toMs": toMs, "items": rows})
}

func (s *Server) writeUsageReportCSV(w http.ResponseWriter, rows []store.UsageReportKeyRow) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="usage-report.csv"`)

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{
		"department_name", "user_name", "email", "api_key_hash", "model",
		"total_tokens", "total_requests", "failed_requests",
		"cached_tokens", "total_cache_tokens", "cache_hit_rate",
	})

	for _, row := range rows {
		for _, m := range row.Models {
			_ = writer.Write([]string{
				row.DepartmentName,
				row.UserName,
				row.Email,
				row.APIKey,
				m.Model,
				strconv.FormatInt(m.TotalTokens, 10),
				strconv.FormatInt(m.Requests, 10),
				strconv.FormatInt(m.FailedRequests, 10),
				strconv.FormatInt(m.CachedTokens, 10),
				strconv.FormatInt(m.TotalCacheTokens, 10),
				strconv.FormatFloat(m.CacheHitRate, 'f', -1, 64),
			})
		}
	}
}

func parseRequiredInt64Query(field string, r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(field))
	if raw == "" {
		return 0, fmt.Errorf("%s is required", field)
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", field)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be a positive integer", field)
	}
	return value, nil
}

func (s *Server) handleUsageExport(w http.ResponseWriter, r *http.Request) {
	data, err := s.store.ExportJSONL(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="usage-events.jsonl"`)
	_, _ = w.Write(data)
}

func (s *Server) handleUsageImport(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxUsageImportBytes)
	data, err := io.ReadAll(body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, err)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}

	parsed, err := usage.ParseImportPayload(data)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":       err.Error(),
			"format":      parsed.Format,
			"failed":      parsed.Failed,
			"unsupported": parsed.Unsupported,
			"warnings":    parsed.Warnings,
		})
		return
	}

	result, err := s.store.InsertEvents(r.Context(), parsed.Events)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"format":      parsed.Format,
		"added":       result.Inserted,
		"skipped":     result.Skipped,
		"total":       len(parsed.Events),
		"failed":      parsed.Failed,
		"unsupported": parsed.Unsupported,
		"warnings":    parsed.Warnings,
	})
}

func isModelListProxyPath(path string) bool {
	cleaned := strings.TrimRight(path, "/")
	return cleaned == "/v1/models" || cleaned == "/models"
}

func (s *Server) handleModelListProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	setup, ok, err := s.resolveSetup(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeError(w, http.StatusPreconditionRequired, errors.New("usage service is not configured"))
		return
	}
	target, err := url.Parse(setup.CPAUpstreamURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		writeError(w, http.StatusBadGateway, err)
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	setup, ok, err := s.resolveSetup(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeError(w, http.StatusPreconditionRequired, errors.New("usage service is not configured"))
		return
	}
	if !authMatches(r, setup.ManagementKey) {
		writeError(w, http.StatusUnauthorized, errors.New("invalid management key"))
		return
	}
	target, err := url.Parse(setup.CPAUpstreamURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host
		req.Header.Set("Authorization", "Bearer "+setup.ManagementKey)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		writeError(w, http.StatusBadGateway, err)
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) handlePanel(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PanelPath != "" {
		if file, err := os.Open(s.cfg.PanelPath); err == nil {
			defer file.Close()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.Copy(w, file)
			return
		}
	}
	data, err := embeddedPanel.ReadFile("web/management.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", mime.TypeByExtension(".html"))
	_, _ = w.Write(data)
}

func (s *Server) resolveSetup(ctx context.Context) (store.Setup, bool, error) {
	setup, _, ok, err := s.resolveSetupWithSource(ctx)
	return setup, ok, err
}

func (s *Server) resolveSetupWithSource(ctx context.Context) (store.Setup, setupSource, bool, error) {
	if s.cfg.CPAUpstreamURL != "" && s.cfg.ManagementKey != "" {
		return store.Setup{
			CPAUpstreamURL: normalizeBaseURL(s.cfg.CPAUpstreamURL),
			ManagementKey:  s.cfg.ManagementKey,
			Queue:          s.cfg.Queue,
			PopSide:        s.cfg.PopSide,
		}, setupSourceEnv, true, nil
	}
	if managerCfg, _, ok, err := s.resolveManagerConfigWithSource(ctx); err != nil {
		return store.Setup{}, setupSourceNone, false, err
	} else if ok && managerCfg.CPAConnection.CPABaseURL != "" && managerCfg.CPAConnection.ManagementKey != "" {
		return setupFromManagerConfig(managerCfg), setupSourceDB, true, nil
	}
	setup, ok, err := s.store.LoadSetup(ctx)
	if !ok || err != nil {
		return setup, setupSourceNone, ok, err
	}
	return setup, setupSourceDB, true, nil
}

func (s *Server) resolveManagerConfigWithSource(ctx context.Context) (store.ManagerConfig, setupSource, bool, error) {
	cfg := s.defaultManagerConfig()
	source := setupSourceNone
	found := false

	if saved, ok, err := s.store.LoadManagerConfig(ctx); err != nil {
		return cfg, source, false, err
	} else if ok {
		cfg = s.mergeSubmittedManagerConfig(cfg, saved)
		source = setupSourceDB
		found = true
	}

	if setup, ok, err := s.store.LoadSetup(ctx); err != nil {
		return cfg, source, false, err
	} else if ok && cfg.CPAConnection.CPABaseURL == "" && cfg.CPAConnection.ManagementKey == "" {
		cfg.CPAConnection.CPABaseURL = normalizeBaseURL(setup.CPAUpstreamURL)
		cfg.CPAConnection.ManagementKey = setup.ManagementKey
		cfg.Collector.Queue = valueOr(setup.Queue, cfg.Collector.Queue)
		cfg.Collector.PopSide = normalizePopSide(setup.PopSide, cfg.Collector.PopSide)
		source = setupSourceDB
		found = true
	}

	if s.cfg.CPAUpstreamURL != "" && s.cfg.ManagementKey != "" {
		cfg.CPAConnection.CPABaseURL = normalizeBaseURL(s.cfg.CPAUpstreamURL)
		cfg.CPAConnection.ManagementKey = s.cfg.ManagementKey
		cfg.Collector.CollectorMode = collectorMode(s.cfg.CollectorMode)
		cfg.Collector.Queue = valueOr(s.cfg.Queue, cfg.Collector.Queue)
		cfg.Collector.PopSide = normalizePopSide(s.cfg.PopSide, cfg.Collector.PopSide)
		cfg.Collector.BatchSize = positiveOrDefault(s.cfg.BatchSize, cfg.Collector.BatchSize, 100)
		cfg.Collector.PollIntervalMS = positiveOrDefault(int(s.cfg.PollInterval/time.Millisecond), cfg.Collector.PollIntervalMS, 500)
		cfg.Collector.QueryLimit = positiveOrDefault(s.cfg.QueryLimit, cfg.Collector.QueryLimit, 50000)
		cfg.Collector.TLSSkipVerify = s.cfg.TLSSkipVerify
		source = setupSourceEnv
		found = true
	}

	return cfg, source, found, nil
}

func setupDiffers(existing store.Setup, req setupRequest) bool {
	return normalizeBaseURL(existing.CPAUpstreamURL) != req.CPAUpstreamURL ||
		existing.ManagementKey != req.ManagementKey ||
		existing.Queue != req.Queue ||
		existing.PopSide != req.PopSide
}

func setupFromManagerConfig(cfg store.ManagerConfig) store.Setup {
	return store.Setup{
		CPAUpstreamURL: cfg.CPAConnection.CPABaseURL,
		ManagementKey:  cfg.CPAConnection.ManagementKey,
		Queue:          cfg.Collector.Queue,
		PopSide:        cfg.Collector.PopSide,
	}
}

func runtimeConfigFromManagerConfig(cfg store.ManagerConfig) collector.RuntimeConfig {
	return collector.RuntimeConfig{
		CPAUpstreamURL: cfg.CPAConnection.CPABaseURL,
		ManagementKey:  cfg.CPAConnection.ManagementKey,
		CollectorMode:  cfg.Collector.CollectorMode,
		Queue:          cfg.Collector.Queue,
		PopSide:        cfg.Collector.PopSide,
		BatchSize:      cfg.Collector.BatchSize,
		PollInterval:   time.Duration(cfg.Collector.PollIntervalMS) * time.Millisecond,
		TLSSkipVerify:  cfg.Collector.TLSSkipVerify,
	}
}

func (s *Server) defaultManagerConfig() store.ManagerConfig {
	pollIntervalMS := int(s.cfg.PollInterval / time.Millisecond)
	return store.ManagerConfig{
		Collector: store.ManagerCollectorConfig{
			Enabled:        boolPtr(true),
			CollectorMode:  collectorMode(s.cfg.CollectorMode),
			Queue:          valueOr(s.cfg.Queue, "usage"),
			PopSide:        normalizePopSide(s.cfg.PopSide, "right"),
			BatchSize:      positiveOrDefault(s.cfg.BatchSize, 100, 100),
			PollIntervalMS: positiveOrDefault(pollIntervalMS, 500, 500),
			QueryLimit:     positiveOrDefault(s.cfg.QueryLimit, 50000, 50000),
			TLSSkipVerify:  s.cfg.TLSSkipVerify,
		},
	}
}

func (s *Server) mergeSubmittedManagerConfig(base store.ManagerConfig, submitted store.ManagerConfig) store.ManagerConfig {
	next := base

	if submitted.CPAConnection.CPABaseURL != "" || submitted.CPAConnection.ManagementKey != "" {
		next.CPAConnection.CPABaseURL = normalizeBaseURL(submitted.CPAConnection.CPABaseURL)
		next.CPAConnection.ManagementKey = strings.TrimSpace(submitted.CPAConnection.ManagementKey)
	}

	if submitted.Collector.Enabled != nil {
		next.Collector.Enabled = boolPtr(*submitted.Collector.Enabled)
	}
	next.Collector.CollectorMode = collectorMode(valueOr(submitted.Collector.CollectorMode, next.Collector.CollectorMode))
	next.Collector.Queue = valueOr(strings.TrimSpace(submitted.Collector.Queue), next.Collector.Queue)
	next.Collector.PopSide = normalizePopSide(submitted.Collector.PopSide, next.Collector.PopSide)
	next.Collector.BatchSize = positiveOrDefault(submitted.Collector.BatchSize, next.Collector.BatchSize, 100)
	next.Collector.PollIntervalMS = positiveOrDefault(submitted.Collector.PollIntervalMS, next.Collector.PollIntervalMS, 500)
	next.Collector.QueryLimit = positiveOrDefault(submitted.Collector.QueryLimit, next.Collector.QueryLimit, 50000)
	next.Collector.TLSSkipVerify = submitted.Collector.TLSSkipVerify

	next.ExternalUsageService.Enabled = submitted.ExternalUsageService.Enabled
	next.ExternalUsageService.ServiceBase = normalizeBaseURL(submitted.ExternalUsageService.ServiceBase)
	if !next.ExternalUsageService.Enabled {
		next.ExternalUsageService.ServiceBase = ""
	}

	return next
}

func managerConfigConnectionDiffers(left store.ManagerConfig, right store.ManagerConfig) bool {
	return normalizeBaseURL(left.CPAConnection.CPABaseURL) != normalizeBaseURL(right.CPAConnection.CPABaseURL) ||
		left.CPAConnection.ManagementKey != right.CPAConnection.ManagementKey ||
		managerCollectorEnabled(left) != managerCollectorEnabled(right) ||
		left.Collector.CollectorMode != right.Collector.CollectorMode ||
		left.Collector.Queue != right.Collector.Queue ||
		left.Collector.PopSide != right.Collector.PopSide ||
		left.Collector.BatchSize != right.Collector.BatchSize ||
		left.Collector.PollIntervalMS != right.Collector.PollIntervalMS ||
		left.Collector.TLSSkipVerify != right.Collector.TLSSkipVerify
}

func positiveOrDefault(value int, fallback int, hardDefault int) int {
	if value > 0 {
		return value
	}
	if fallback > 0 {
		return fallback
	}
	return hardDefault
}

func valueOr(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func normalizePopSide(value string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "left", "right":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		if strings.ToLower(strings.TrimSpace(fallback)) == "left" {
			return "left"
		}
		return "right"
	}
}

func collectorMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "http", "resp":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "auto"
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func managerCollectorEnabled(cfg store.ManagerConfig) bool {
	return cfg.Collector.Enabled == nil || *cfg.Collector.Enabled
}

func setupRequestMonitoringEnabled(req setupRequest) bool {
	if req.RequestMonitoringEnabled == nil {
		return true
	}
	return *req.RequestMonitoringEnabled
}

func (s *Server) authorizeIfConfigured(w http.ResponseWriter, r *http.Request) bool {
	setup, ok, err := s.resolveSetup(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return false
	}
	if !ok || setup.ManagementKey == "" {
		return true
	}
	if authMatches(r, setup.ManagementKey) {
		return true
	}
	writeError(w, http.StatusUnauthorized, errors.New("invalid management key"))
	return false
}

func authMatches(r *http.Request, managementKey string) bool {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" || managementKey == "" {
		return false
	}
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	return strings.TrimSpace(header[len(prefix):]) == managementKey
}

func (s *Server) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.writeCORS(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (s *Server) writeCORS(w http.ResponseWriter, r *http.Request) {
	if len(s.cfg.CORSOrigins) == 0 {
		return
	}
	origin := r.Header.Get("Origin")
	allowed := s.cfg.CORSOrigins[0]
	for _, candidate := range s.cfg.CORSOrigins {
		if candidate == "*" || candidate == origin {
			allowed = candidate
			break
		}
	}
	if allowed == "*" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else if origin != "" && allowed == origin {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
}

func (s *Server) syncEnterpriseKeysToCPAConfig(ctx context.Context) error {
	setup, ok, err := s.resolveSetup(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("usage service is not configured")
	}
	baseURL := normalizeBaseURL(setup.CPAUpstreamURL)
	managementKey := strings.TrimSpace(setup.ManagementKey)
	if baseURL == "" || managementKey == "" {
		return errors.New("cpaBaseUrl and managementKey are required")
	}

	bindings, err := s.store.LoadEnterpriseKeyBindings(ctx)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	apiKeys := make([]string, 0, len(bindings))
	for _, item := range bindings {
		key := strings.TrimSpace(item.APIKey)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		apiKeys = append(apiKeys, key)
	}
	sort.Strings(apiKeys)

	payload, err := json.Marshal(apiKeys)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, baseURL+"/v0/management/api-keys", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+managementKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	if len(body) > 0 {
		return fmt.Errorf("sync api-keys to CPA failed: %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	return fmt.Errorf("sync api-keys to CPA failed: %s", res.Status)
}

func (s *Server) mergeCPAAPIKeysIntoEnterpriseStore(ctx context.Context, baseURL string, managementKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v0/management/api-keys", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+managementKey)
	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		if len(body) > 0 {
			return fmt.Errorf("fetch api-keys from CPA failed: %s: %s", res.Status, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("fetch api-keys from CPA failed: %s", res.Status)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024))
	if err != nil {
		return err
	}
	keys, err := parseAPIKeysPayload(body)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}

	existing, err := s.store.LoadEnterpriseKeyBindings(ctx)
	if err != nil {
		return err
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		key := strings.TrimSpace(item.APIKey)
		if key == "" {
			continue
		}
		existingSet[key] = struct{}{}
	}

	missing := make([]store.EnterpriseKeyBinding, 0)
	now := time.Now().UnixMilli()
	for _, key := range keys {
		if _, ok := existingSet[key]; ok {
			continue
		}
		missing = append(missing, store.EnterpriseKeyBinding{
			APIKey:               key,
			UserName:             "",
			DepartmentID:         "",
			Source:               "sync",
			DepartmentResolvedBy: "manual",
			CreatedAtMS:          now,
			UpdatedAtMS:          now,
		})
	}
	if len(missing) == 0 {
		return nil
	}
	return s.store.UpsertEnterpriseKeyBindings(ctx, missing)
}

func parseAPIKeysPayload(raw []byte) ([]string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, nil
	}

	var list []string
	if err := json.Unmarshal([]byte(trimmed), &list); err == nil {
		return normalizeAPIKeys(list), nil
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return nil, err
	}
	for _, key := range []string{"api-keys", "apiKeys", "items"} {
		value, ok := obj[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case []any:
			next := make([]string, 0, len(typed))
			for _, item := range typed {
				text := strings.TrimSpace(fmt.Sprint(item))
				if text != "" {
					next = append(next, text)
				}
			}
			return normalizeAPIKeys(next), nil
		case []string:
			return normalizeAPIKeys(typed), nil
		}
	}
	return nil, nil
}

func normalizeAPIKeys(keys []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func validateCollectorAgainstCPA(ctx context.Context, cfg store.ManagerConfig) error {
	usageCfg, err := fetchCPAUsageConfig(ctx, cfg.CPAConnection.CPABaseURL, cfg.CPAConnection.ManagementKey)
	if err != nil {
		return err
	}
	retentionMS := usageCfg.RedisUsageQueueRetentionSeconds * 1000
	if retentionMS <= 0 {
		return errors.New("CPA redis-usage-queue-retention-seconds must be greater than 0")
	}
	if cfg.Collector.PollIntervalMS > retentionMS {
		return fmt.Errorf(
			"pollIntervalMs must be less than or equal to CPA redis-usage-queue-retention-seconds (%d seconds)",
			usageCfg.RedisUsageQueueRetentionSeconds,
		)
	}
	return nil
}

func validateManagementAPI(ctx context.Context, baseURL string, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v0/management/config", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	return errors.New("management API validation failed: " + res.Status)
}

func fetchCPAUsageConfig(ctx context.Context, baseURL string, key string) (cpaUsageConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalizeBaseURL(baseURL)+"/v0/management/config", nil)
	if err != nil {
		return cpaUsageConfig{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return cpaUsageConfig{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return cpaUsageConfig{}, errors.New("management API config request failed: " + res.Status)
	}

	var raw map[string]any
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return cpaUsageConfig{}, err
	}
	usageEnabled := readBoolField(raw, "usage-statistics-enabled", "usageStatisticsEnabled")
	retention, hasRetention := readIntField(raw, "redis-usage-queue-retention-seconds", "redisUsageQueueRetentionSeconds")
	if !hasRetention {
		retention = 60
	}
	return cpaUsageConfig{
		UsageStatisticsEnabled:          usageEnabled,
		RedisUsageQueueRetentionSeconds: retention,
		RetentionSourceDefault:          !hasRetention,
	}, nil
}

func setCPAUsageStatisticsEnabled(ctx context.Context, baseURL string, key string, enabled bool) error {
	payload := map[string]bool{"value": enabled}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPut,
		normalizeBaseURL(baseURL)+"/v0/management/usage-statistics-enabled",
		strings.NewReader(string(data)),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	return errors.New("enable CPA usage statistics failed: " + res.Status)
}

func readBoolField(raw map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			normalized := strings.ToLower(strings.TrimSpace(typed))
			return normalized == "1" || normalized == "true" || normalized == "yes" || normalized == "on"
		}
	}
	return false
}

func readIntField(raw map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return int(typed), true
		case int:
			return typed, true
		case json.Number:
			parsed, err := strconv.Atoi(typed.String())
			return parsed, err == nil
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(typed))
			return parsed, err == nil
		}
	}
	return 0, false
}

func normalizeBaseURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	value = strings.TrimRight(value, "/")
	value = strings.TrimSuffix(value, "/v0/management")
	value = strings.TrimSuffix(value, "/v0")
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error(), "code": usageServiceErrorCode(err)})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
}

func usageServiceErrorCode(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "connection setup is managed by environment variables"):
		return "connection_env_managed"
	case strings.Contains(message, "cpaBaseUrl and managementKey are required when request monitoring is enabled"):
		return "cpa_connection_required_for_monitoring"
	case strings.Contains(message, "cpaBaseUrl and managementKey are required"):
		return "cpa_connection_required"
	case strings.Contains(message, "setup is managed by environment variables"):
		return "setup_env_managed"
	case strings.Contains(message, "invalid management key for existing setup"):
		return "invalid_existing_management_key"
	case strings.Contains(message, "invalid management key"):
		return "invalid_management_key"
	case strings.Contains(message, "usage service is not configured"):
		return "usage_service_not_configured"
	case strings.Contains(message, "CPA redis-usage-queue-retention-seconds must be greater than 0"):
		return "cpa_usage_retention_invalid"
	case strings.Contains(message, "pollIntervalMs must be less than or equal"):
		return "poll_interval_exceeds_retention"
	case strings.Contains(message, "management API validation failed"):
		return "management_api_validation_failed"
	case strings.Contains(message, "management API config request failed"):
		return "management_api_config_failed"
	case strings.Contains(message, "enable CPA usage statistics failed"):
		return "enable_cpa_usage_statistics_failed"
	case strings.Contains(message, "prices are required"):
		return "prices_required"
	case strings.Contains(message, "api key aliases are required"):
		return "api_key_aliases_required"
	case strings.Contains(message, "api key alias already exists"):
		return "api_key_alias_duplicate"
	case strings.Contains(message, "model price sync failed"):
		return "model_price_sync_failed"
	case strings.Contains(message, "method not allowed"):
		return "method_not_allowed"
	default:
		return "request_failed"
	}
}
