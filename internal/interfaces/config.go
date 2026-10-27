// Package interfaces provides HTTP handlers for the OpsAgent API.
package interfaces

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/chat"
	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/llm"
	"github.com/Havens-blog/OpsAgent/internal/observability"
)

// providerDTO describes a selectable LLM provider for the console.
type providerDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	BaseURL      string   `json:"base_url"`
	DefaultModel string   `json:"default_model"`
	Models       []string `json:"models"`
}

// llmConfigDTO is the masked LLM configuration returned to the console.
// The API key is never returned in plaintext — only a masked hint and a flag.
type llmConfigDTO struct {
	Provider     string  `json:"provider"`
	BaseURL      string  `json:"base_url"`
	Model        string  `json:"model"`
	HasAPIKey    bool    `json:"has_api_key"`
	APIKeyMasked string  `json:"api_key_masked"`
	MaxTokens    int     `json:"max_tokens"`
	Temperature  float64 `json:"temperature"`
	Timeout      int     `json:"timeout"`
	Available    bool    `json:"available"`
}

// llmConfigRequest is the payload accepted when saving/testing LLM config.
// An empty APIKey means "keep the existing key" so the console never has to
// echo the secret back to save unrelated changes.
type llmConfigRequest struct {
	Provider    string  `json:"provider"`
	BaseURL     string  `json:"base_url"`
	Model       string  `json:"model"`
	APIKey      string  `json:"api_key"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
	Timeout     int     `json:"timeout"`
}

// notificationConfigDTO is the IM notification configuration returned to the
// console. The webhook secret is masked, never echoed in plaintext.
type notificationConfigDTO struct {
	Platform      string   `json:"platform"`
	WebhookURL    string   `json:"webhook_url"`
	HasSecret     bool     `json:"has_secret"`
	SecretMasked  string   `json:"secret_masked"`
	Severities    []string `json:"severities"`
	StartTime     string   `json:"start_time"`
	EndTime       string   `json:"end_time"`
}

// notificationConfigRequest is the payload accepted when saving IM config.
// An empty Secret means "keep the existing secret".
type notificationConfigRequest struct {
	Platform   string   `json:"platform"`
	WebhookURL string   `json:"webhook_url"`
	Secret     string   `json:"secret"`
	Severities []string `json:"severities"`
	StartTime  string   `json:"start_time"`
	EndTime    string   `json:"end_time"`
}

// securityConfigDTO is the security configuration returned to the console.
type securityConfigDTO struct {
	EnableAuditLog    bool         `json:"enable_audit_log"`
	AuditLogPath      string       `json:"audit_log_path"`
	AuditRetentionDays int        `json:"audit_retention_days"`
	IPWhitelist       string       `json:"ip_whitelist"`
	RateLimit         int          `json:"rate_limit"`
	AuditLogs         []auditEntry `json:"audit_logs"`
}

// securityConfigRequest is the payload accepted when saving security config.
type securityConfigRequest struct {
	EnableAuditLog     bool   `json:"enable_audit_log"`
	AuditLogPath       string `json:"audit_log_path"`
	AuditRetentionDays int    `json:"audit_retention_days"`
	IPWhitelist        string `json:"ip_whitelist"`
	RateLimit          int    `json:"rate_limit"`
}

// maskAPIKey returns a privacy-preserving hint for a secret, e.g. "sk-****wxyz".
func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	runes := []rune(key)
	n := len(runes)
	if n <= 8 {
		return "****"
	}
	return string(runes[:4]) + "****" + string(runes[n-4:])
}

// isConfiguredKey reports whether the key is a real (non-placeholder) secret.
func isConfiguredKey(key string) bool {
	if key == "" {
		return false
	}
	// Mirror chat.isPlaceholderKey heuristics for a few known placeholders.
	switch key {
	case "sk-your-api-key-here", "your-api-key", "your-api-key-here",
		"placeholder", "none", "<your-api-key>":
		return false
	}
	return true
}

// providersHandler lists all known LLM providers and their suggested models.
func (s *APIServer) providersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed", "")
		return
	}

	out := make([]providerDTO, 0, len(llm.Providers))
	seen := make(map[string]bool)
	appendProvider := func(id string) {
		p, ok := llm.Providers[id]
		if !ok || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, providerDTO{
			ID:           id,
			Name:         p.Name,
			BaseURL:      p.BaseURL,
			DefaultModel: p.DefaultModel,
			Models:       p.Models,
		})
	}
	// Stable, curated order first, then any others.
	for _, id := range llm.ProviderOrder {
		appendProvider(id)
	}
	for id := range llm.Providers {
		appendProvider(id)
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"providers": out,
	})
}

// dataSourcesHandler reports which RCA dimensions are backed by a real
// integration versus a mock fallback. This makes the progressive-enhancement
// state observable: an operator can confirm at a glance whether metrics /
// topology / changes are live or still mock.
func (s *APIServer) dataSourcesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed", "")
		return
	}

	status := s.dataSourceStatus
	if status == nil {
		status = map[string]bool{}
	}

	// A dimension-by-dimension view with the env var that activates each.
	dims := []map[string]interface{}{
		{"dimension": "metrics", "source": "Prometheus / ARMS", "real": status["metrics"],
			"env": []string{"PROMETHEUS_ENDPOINT", "ARMS_PROMETHEUS_BEARER_TOKEN"}},
		{"dimension": "topology", "source": "ARMS APM (Prometheus)", "real": status["topology"],
			"env": []string{"ARMS_APM_ENDPOINT", "ARMS_APM_TOKEN"}},
		{"dimension": "changes", "source": "Kubernetes", "real": status["changes"],
			"env": []string{"K8S_ENDPOINT", "K8S_TOKEN", "K8S_NAMESPACE"}},
		{"dimension": "logs", "source": "SLS / Elasticsearch", "real": status["logs"],
			"env": []string{"(not yet integrated)"}},
	}

	realCount := 0
	for _, d := range dims {
		if d["real"] == true {
			realCount++
		}
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"dimensions": dims,
		"real_count": realCount,
		"total":      len(dims),
		"all_mock":   realCount == 0,
	})
}

// llmConfigHandler returns (GET) or updates (PUT/POST) the LLM configuration.
func (s *APIServer) llmConfigHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getLLMConfig(w, r)
	case http.MethodPut, http.MethodPost:
		// is_admin write gate — replicates the legacy ops Agent, whose only
		// RBAC control is "non-admins cannot mutate settings". Only enforced
		// when EIAM is active: without EIAM there is no admin concept, so
		// gating would lock every static-token deployment out of config
		// writes (progressive enhancement).
		if s.eiamSessionKey != "" && !EIAMIsAdmin(r.Context()) {
			s.writeErrorResponse(w, http.StatusForbidden, "forbidden",
				"仅管理员可修改配置", "")
			return
		}
		s.saveLLMConfig(w, r)
	default:
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET/PUT/POST allowed", "")
	}
}

// getLLMConfig returns the current (masked) LLM configuration.
func (s *APIServer) getLLMConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.settings.ToLLMConfig()
	s.writeJSONResponse(w, http.StatusOK, s.llmConfigDTO(cfg))
}

// llmConfigDTO builds the masked DTO from an LLMConfig.
func (s *APIServer) llmConfigDTO(cfg config.LLMConfig) llmConfigDTO {
	configured := isConfiguredKey(cfg.APIKey)
	return llmConfigDTO{
		Provider:     cfg.Provider,
		BaseURL:      cfg.BaseURL,
		Model:        cfg.Model,
		HasAPIKey:    configured,
		APIKeyMasked: maskAPIKey(cfg.APIKey),
		MaxTokens:    cfg.MaxTokens,
		Temperature:  cfg.Temperature,
		Timeout:      cfg.Timeout,
		Available:    s.getChatService() != nil && configured,
	}
}

// mergeLLMConfig applies a request onto the existing config, preserving the
// stored API key when the request leaves it blank, and filling base_url/model
// from the provider catalog when omitted.
func mergeLLMConfig(existing config.LLMConfig, req llmConfigRequest) config.LLMConfig {
	merged := existing
	if req.Provider != "" {
		merged.Provider = req.Provider
	}
	merged.BaseURL = req.BaseURL
	merged.Model = req.Model
	if req.APIKey != "" {
		merged.APIKey = req.APIKey
	}
	if req.MaxTokens > 0 {
		merged.MaxTokens = req.MaxTokens
	}
	if req.Temperature >= 0 {
		merged.Temperature = req.Temperature
	}
	if req.Timeout > 0 {
		merged.Timeout = req.Timeout
	}

	// Fill base_url / model defaults from the provider catalog.
	if provider, ok := llm.GetProvider(merged.Provider); ok {
		if merged.BaseURL == "" {
			merged.BaseURL = provider.BaseURL
		}
		if merged.Model == "" {
			merged.Model = provider.DefaultModel
		}
	}
	if merged.MaxTokens == 0 {
		merged.MaxTokens = 4096
	}
	if merged.Timeout == 0 {
		merged.Timeout = 120
	}
	return merged
}

// saveLLMConfig persists a new LLM configuration, rebuilds the chat service,
// and returns the masked result.
func (s *APIServer) saveLLMConfig(w http.ResponseWriter, r *http.Request) {
	var req llmConfigRequest
	req.Temperature = -1 // sentinel so 0 is distinguishable only via omission
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
		return
	}
	if req.Temperature < 0 {
		req.Temperature = 0.7
	}

	merged := mergeLLMConfig(s.settings.ToLLMConfig(), req)

	if !isConfiguredKey(merged.APIKey) {
		s.writeErrorResponse(w, http.StatusBadRequest, "missing_api_key",
			"API key is required (and must not be a placeholder)", "")
		return
	}

	// Apply in-memory so the running process uses it immediately.
	s.settings.SetLLM(merged)

	// Rebuild the chat service with the new config.
	newSvc, err := chat.NewService(s.settings.ToLLMConfig())
	if err != nil {
		s.logger.Warn("Chat service rebuild failed after config save",
			observability.Err(err),
		)
		s.setChatService(nil)
	} else {
		s.setChatService(newSvc)
	}

	// Persist to disk so the config survives a restart.
	persisted := true
	var persistErr string
	saveErr := s.settings.SaveConfig()
	if saveErr != nil {
		persisted = false
		persistErr = saveErr.Error()
		s.logger.Error("Failed to persist settings",
			observability.String("path", s.settings.SettingsFilePath()),
			observability.Err(saveErr),
		)
	} else {
		s.logger.Info("LLM configuration saved",
			observability.String("provider", merged.Provider),
			observability.String("model", merged.Model),
			observability.String("path", s.settings.SettingsFilePath()),
		)
	}
	s.recordConfigChange(r, "修改模型配置", saveErr)

	dto := s.llmConfigDTO(s.settings.ToLLMConfig())
	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"saved":         true,
		"persisted":     persisted,
		"persist_error": persistErr,
		"config":        dto,
	})
}

// llmModelsHandler returns the model catalog for a provider. It tries a LIVE
// fetch from the provider's /models endpoint using the proposed-or-stored
// config, and falls back to the curated suggestion list when the live call is
// unavailable (no key, provider without /models, auth error, etc.).
func (s *APIServer) llmModelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET/POST allowed", "")
		return
	}

	var req llmConfigRequest
	req.Temperature = -1
	if r.Method == http.MethodPost && r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Temperature < 0 {
		req.Temperature = 0.7
	}

	cfg := mergeLLMConfig(s.settings.ToLLMConfig(), req)

	// Curated fallback from the static catalog.
	var fallback []string
	if p, ok := llm.GetProvider(cfg.Provider); ok {
		fallback = p.Models
	}

	writeFallback := func(reason string) {
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"models": fallback,
			"source": "fallback",
			"error":  reason,
		})
	}

	if !isConfiguredKey(cfg.APIKey) {
		writeFallback("未配置 API Key，显示建议模型列表")
		return
	}

	client := llm.NewClient(&llm.Config{
		Provider: cfg.Provider,
		BaseURL:  cfg.BaseURL,
		APIKey:   cfg.APIKey,
		Model:    cfg.Model,
		Timeout:  15 * time.Second,
	})
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	models, err := client.GetModels(ctx)
	if err != nil {
		writeFallback(err.Error())
		return
	}
	if len(models) == 0 {
		writeFallback("提供商未返回模型列表")
		return
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"models": models,
		"source": "provider",
	})
}

// testLLMHandler performs a live connectivity test against the LLM provider.
// It tests the proposed config when provided, otherwise the stored config.
func (s *APIServer) testLLMHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed", "")
		return
	}

	var req llmConfigRequest
	req.Temperature = -1
	// Body is optional — an empty body tests the stored config.
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Temperature < 0 {
		req.Temperature = 0.7
	}

	cfg := mergeLLMConfig(s.settings.ToLLMConfig(), req)

	if !isConfiguredKey(cfg.APIKey) {
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "API key is not configured",
		})
		return
	}

	svc, err := chat.NewService(cfg)
	if err != nil {
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()
	reply, err := svc.Chat(ctx, []chat.Message{{Role: "user", Content: "ping"}})
	latency := time.Since(start)
	if err != nil {
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"model":   svc.GetModel(),
		})
		return
	}

	preview := []rune(reply)
	if len(preview) > 80 {
		preview = preview[:80]
	}
	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"model":      svc.GetModel(),
		"latency_ms": latency.Milliseconds(),
		"reply":      string(preview),
	})
}

// notificationConfigHandler returns (GET) or saves (PUT/POST) the IM
// notification configuration. Mirrors llmConfigHandler's admin write gate.
func (s *APIServer) notificationConfigHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.settings.ToNotificationConfig()
		s.writeJSONResponse(w, http.StatusOK, notificationConfigDTO{
			Platform:     cfg.Platform,
			WebhookURL:   cfg.WebhookURL,
			HasSecret:    cfg.Secret != "",
			SecretMasked: maskAPIKey(cfg.Secret),
			Severities:   cfg.Severities,
			StartTime:    cfg.StartTime,
			EndTime:      cfg.EndTime,
		})
	case http.MethodPut, http.MethodPost:
		if s.eiamSessionKey != "" && !EIAMIsAdmin(r.Context()) {
			s.writeErrorResponse(w, http.StatusForbidden, "forbidden",
				"仅管理员可修改配置", "")
			return
		}
		var req notificationConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
			return
		}
		existing := s.settings.ToNotificationConfig()
		merged := config.NotificationConfig{
			Platform:   req.Platform,
			WebhookURL:  req.WebhookURL,
			Severities:  req.Severities,
			StartTime:   req.StartTime,
			EndTime:     req.EndTime,
			Secret:      existing.Secret, // keep existing secret when request omits it
		}
		if req.Secret != "" {
			merged.Secret = req.Secret
		}
		if merged.Platform == "" {
			merged.Platform = existing.Platform
		}
		s.settings.SetNotifications(merged)

		persisted := true
		var persistErr string
		saveErr := s.settings.SaveConfig()
		if saveErr != nil {
			persisted = false
			persistErr = saveErr.Error()
			s.logger.Error("Failed to persist notification config",
				observability.Err(saveErr),
				observability.String("path", s.settings.SettingsFilePath()),
			)
		}
		s.recordConfigChange(r, "修改IM通知配置", saveErr)
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"status":    "saved",
			"persisted": persisted,
			"config": notificationConfigDTO{
				Platform:     merged.Platform,
				WebhookURL:   merged.WebhookURL,
				HasSecret:    merged.Secret != "",
				SecretMasked: maskAPIKey(merged.Secret),
				Severities:   merged.Severities,
				StartTime:    merged.StartTime,
				EndTime:      merged.EndTime,
			},
			"error": persistErr,
		})
	default:
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET/PUT/POST allowed", "")
	}
}

// securityConfigHandler returns (GET) or saves (PUT/POST) the security
// configuration. Mirrors llmConfigHandler's admin write gate.
func (s *APIServer) securityConfigHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.settings.ToSecurityConfig()
		s.writeJSONResponse(w, http.StatusOK, securityConfigDTO{
			EnableAuditLog:     cfg.EnableAuditLog,
			AuditLogPath:       cfg.AuditLogPath,
			AuditRetentionDays: cfg.AuditRetentionDays,
			IPWhitelist:        cfg.IPWhitelist,
			RateLimit:          cfg.RateLimit,
			AuditLogs:          s.auditLog.recent(50),
		})
	case http.MethodPut, http.MethodPost:
		if s.eiamSessionKey != "" && !EIAMIsAdmin(r.Context()) {
			s.writeErrorResponse(w, http.StatusForbidden, "forbidden",
				"仅管理员可修改配置", "")
			return
		}
		var req securityConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
			return
		}
		merged := config.SecurityConfig{
			EnableAuditLog:     req.EnableAuditLog,
			AuditLogPath:       req.AuditLogPath,
			AuditRetentionDays: req.AuditRetentionDays,
			IPWhitelist:        req.IPWhitelist,
			RateLimit:          req.RateLimit,
			VaultAddr:          s.settings.ToSecurityConfig().VaultAddr,
			VaultToken:         s.settings.ToSecurityConfig().VaultToken,
			VaultPath:          s.settings.ToSecurityConfig().VaultPath,
		}
		// Record BEFORE applying so the pre-change gate governs: the final
		// "disable auditing" change is still captured for forensics.
		s.recordConfigChange(r, "修改安全配置", nil)
		s.settings.SetSecurity(merged)

		persisted := true
		var persistErr string
		if err := s.settings.SaveConfig(); err != nil {
			persisted = false
			persistErr = err.Error()
			s.logger.Error("Failed to persist security config",
				observability.Err(err),
				observability.String("path", s.settings.SettingsFilePath()),
			)
		}
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"status":    "saved",
			"persisted": persisted,
			"config": securityConfigDTO{
				EnableAuditLog:     merged.EnableAuditLog,
				AuditLogPath:       merged.AuditLogPath,
				AuditRetentionDays: merged.AuditRetentionDays,
				IPWhitelist:        merged.IPWhitelist,
				RateLimit:          merged.RateLimit,
				AuditLogs:          s.auditLog.recent(50),
			},
			"error": persistErr,
		})
	default:
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET/PUT/POST allowed", "")
	}
}
