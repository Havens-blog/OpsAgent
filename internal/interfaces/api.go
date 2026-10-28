// Package interfaces provides REST API handlers for OpsAgent.
package interfaces

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/agent"
	"github.com/Havens-blog/OpsAgent/internal/chat"
	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/observability"
	"github.com/Havens-blog/OpsAgent/internal/rca"
	"github.com/Havens-blog/OpsAgent/internal/rca/engine"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

// APIServer represents the REST API server.
type APIServer struct {
	settings    *config.Settings
	router      *http.ServeMux
	server      *http.Server
	logger      *observability.Logger
	agents      map[string]executableAgent // Registered executable agents
	corsOrigins []string

	// apiAuthToken, when non-empty, is the shared bearer token every
	// /api/v1/* request must present (health is exempt). Empty disables
	// enforcement (progressive-enhancement: secure when configured, open
	// out-of-the-box) and emits a startup warning so the gap is never silent.
	apiAuthToken string

	// EIAM session integration. When eiamSessionKey is non-empty, /api/v1/*
	// requests are authenticated by verifying the EIAM-issued HS256 session
	// JWT (see eiam_auth.go). Empty disables EIAM enforcement and the server
	// falls back to the apiAuthToken bearer check. eiamCookieName is the
	// browser cookie carrying the token when no Authorization header is sent.
	eiamSessionKey string
	eiamCookieName string

	// Engine integrations — wired so the console returns real data instead
	// of placeholders. Each is nil-safe: handlers fall back to empty results
	// when an integration is not configured.
	// topologySource is the single, shared service-topology source (real ARMS
	// APM when configured, mock otherwise). The /api/v1/topology handler reads
	// THIS, not a separate client, so the console shows the same topology the
	// RCA engine reasons over. Previously a separate mock topology.ARMSClient
	// served this endpoint, so real topology never reached the console.
	topologySource sources.TopologySource
	// changeSource is the REAL change-event source shared with the RCA engine
	// (K8s deployment conditions or e-cam k8s-events, whichever is configured
	// and reachable). changeSourceReal mirrors active["changes"]: when false the
	// dimension fell back to the honest empty source and /api/v1/changes MUST
	// return an empty list with a hint — never query the fallback, never
	// fabricate entries.
	changeSource     sources.ChangeSource
	changeSourceReal bool

	// changeCache memoizes the (expensive) real-source query for a short TTL:
	// the upstream event scan takes seconds, so console reloads would otherwise
	// re-pay it on every visit. Keyed by the quantized window + source identity.
	changeCacheMu    sync.Mutex
	changeCacheSrc   sources.ChangeSource
	changeCacheKey   string
	changeCacheAt    time.Time
	changeCacheItems []changeItemDTO
	changeCacheStats map[string]interface{}

	// RCA engine and report store
	rcaEngine *engine.Engine
	rcaStore  rca.RCAReportStore

	// Conversation store for the chat assistant (tenant-scoped, in-memory).
	conversationStore chat.ConversationStore

	// Chat service for conversational assistant (nil-safe).
	// Guarded by chatMu because the config endpoints can rebuild it at runtime
	// while chat requests read it concurrently.
	chatMu      sync.RWMutex
	chatService *chat.Service

	// Approval state shared between tool execution and the approval HTTP
	// endpoints, plus the pending-approval store surfaced by those endpoints.
	approvalMgr      *guardrails.ApprovalManager
	pendingApprovals *PendingApprovalStore

	// Async agent task records (tenant-scoped, in-memory).
	taskStore AgentTaskStore

	// agentMeta holds display metadata for registered agents (keyed by id).
	agentMeta map[string]*agentEntry

	// dataSourceStatus records which RCA dimensions use a real integration
	// (true) versus a mock fallback (false). Populated by initAgents.
	dataSourceStatus map[string]bool

	// auditLog is the bounded in-memory ring of recent config-change records,
	// read back by the security settings page. See audit_hook.go.
	auditLog *recentAuditLog
}

// getChatService returns the current chat service under a read lock.
func (s *APIServer) getChatService() *chat.Service {
	s.chatMu.RLock()
	defer s.chatMu.RUnlock()
	return s.chatService
}

// setChatService replaces the chat service under a write lock.
func (s *APIServer) setChatService(svc *chat.Service) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	s.chatService = svc
}

// NewAPIServer creates a new API server.
func NewAPIServer(settings *config.Settings) (*APIServer, error) {
	if settings == nil {
		return nil, fmt.Errorf("settings is required")
	}

	logger, err := observability.NewLogger("api-server", settings.Observability.LogLevel, observability.FormatJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Build RCA data sources ONCE with progressive enhancement (real where
	// credentials exist in env, mock otherwise), then share the exact same
	// source set between the HTTP RCA engine (/api/v1/rca/analyze) and the
	// MonitorAgent. Previously these were two independent engines and the HTTP
	// one was hard-wired to mock, so real data never reached /api/v1/rca.
	dsCfg := agent.NewDataSourceConfigFromEnv()
	rcaSources, dsActive := agent.CreateRCASourcesWithFallback(dsCfg)

	// The fallback factory always supplies a trace source (empty when no real
	// tracing integration is configured), so this is just a defensive nil guard
	// — and the guard uses the honest empty source, never a fabricating mock.
	traceSource := rcaSources.TraceSource
	if traceSource == nil {
		traceSource = sources.NewEmptyTraceSource()
	}

	rcaEngine, err := engine.NewEngine(
		engine.WithLogSource(rcaSources.LogSource),
		engine.WithMetricSource(rcaSources.MetricSource),
		engine.WithTraceSource(traceSource),
		engine.WithTopologySource(rcaSources.TopologySource),
		engine.WithChangeSource(rcaSources.ChangeSource),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create RCA engine: %w", err)
	}
	logger.Info("RCA data sources configured",
		observability.Bool("metrics_real", dsActive["metrics"]),
		observability.Bool("topology_real", dsActive["topology"]),
		observability.Bool("changes_real", dsActive["changes"]),
		observability.Bool("logs_real", dsActive["logs"]),
	)

	// Initialize RCA report store
	// Start with an empty report store. Reports appear here only from real
	// analyses run via POST /api/v1/rca/analyze, so the console never shows
	// seeded/demo data masquerading as real results.
	rcaStore := rca.NewMemoryRCAReportStore()

	// Initialize chat service (nil-safe - if LLM not configured, chat endpoints return 503)
	llmConfig := settings.ToLLMConfig()
	chatService, err := chat.NewService(llmConfig)
	if err != nil {
		logger.Warn("Chat service not available",
			observability.Err(err),
			observability.String("hint", "Configure LLM_API_KEY in config/settings.yaml to enable chat assistant"),
		)
		chatService = nil
	} else {
		logger.Info("Chat service initialized",
			observability.String("provider", settings.LLM.Provider),
			observability.String("model", settings.LLM.Model),
		)
	}

	// Approval state and pending-approval store are shared between the chat
	// tool-execution path and the approval HTTP endpoints.
	approvalMgr := guardrails.NewApprovalManager(nil)
	pendingApprovals := NewPendingApprovalStore()
	if chatService != nil {
		provider := newRegistryToolProvider(buildChatToolRegistry(), settings.DataSources, approvalMgr)
		chatService.SetToolProvider(provider)
	}

	// Bearer token for /api/v1/* access. Read from env (single source of
	// truth shared with the frontend proxy). Empty => enforcement disabled.
	apiAuthToken := strings.TrimSpace(os.Getenv("API_AUTH_TOKEN"))

	// EIAM session integration. When the shared session key is configured,
	// /api/v1/* is authenticated by verifying EIAM-issued session JWTs; this
	// takes precedence over the static apiAuthToken (which stays as the
	// service-to-service / no-EIAM fallback).
	eiamSessionKey := strings.TrimSpace(settings.EIAM.SessionKey)

	switch {
	case eiamSessionKey != "":
		logger.Info("EIAM session authentication enabled on /api/v1/* (verifying EIAM-issued HS256 session JWTs)")
	case apiAuthToken != "":
		logger.Info("API authentication enabled (static bearer token required on /api/v1/*)")
	default:
		logger.Warn("API authentication DISABLED",
			observability.String("hint", "set OPSAGENT_SESSION_ENCRYPTED_KEY (EIAM) or API_AUTH_TOKEN to protect /api/v1/*"),
		)
	}

	// Conversation persistence: use the disk-backed store when it can be
	// created, falling back to memory so the server still starts if the data
	// directory is unwritable or the snapshot is corrupt.
	convPath := strings.TrimSpace(os.Getenv("OPSAGENT_CONVERSATIONS_PATH"))
	if convPath == "" {
		convPath = "data/conversations.json"
	}
	conversationStore := chat.ConversationStore(chat.NewMemoryConversationStore())
	if ps, err := chat.NewPersistentConversationStore(convPath); err != nil {
		logger.Warn("Persistent conversation store unavailable, using in-memory fallback",
			observability.String("path", convPath),
			observability.Err(err),
		)
	} else {
		conversationStore = ps
	}

	server := &APIServer{
		settings:       settings,
		router:         http.NewServeMux(),
		logger:         logger,
		apiAuthToken:   apiAuthToken,
		eiamSessionKey: eiamSessionKey,
		eiamCookieName: settings.EIAM.CookieName,
		corsOrigins:    []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://localhost:8080"},

		// Default integrations. In production these would be constructed
		// from settings (credentials, endpoints); the defaults use the
		// in-memory / mock-backed implementations so the API is functional
		// out of the box.
		topologySource:    rcaSources.TopologySource,
		rcaEngine:         rcaEngine,
		rcaStore:          rcaStore,
		conversationStore: conversationStore,
		chatService:       chatService,
		approvalMgr:       approvalMgr,
		pendingApprovals:  pendingApprovals,
		taskStore:         NewMemoryAgentTaskStore(),
		auditLog:          newRecentAuditLog(100),
	}

	// Construct and register the coordinator + specialist agents (nil-safe:
	// a failed construction simply omits that agent).
	server.initAgents()

	server.setupRoutes()

	return server, nil
}

// setupRoutes configures all API routes.
func (s *APIServer) setupRoutes() {
	// Health check (both root and versioned paths for console compatibility)
	s.router.HandleFunc("/health", s.corsMiddleware(s.healthHandler))
	s.router.HandleFunc("/api/v1/health", s.corsMiddleware(s.healthHandler))

	// WatchAlert webhook — machine-to-machine alert ingestion from the
	// platform's alert module. Not bearer-protected (watch-alert is not a
	// user); nginx/shared-secret can gate it in production.
	s.router.HandleFunc("/webhooks/watchalert", s.corsMiddleware(s.watchAlertWebhookHandler))

	// API v1 routes (all bearer-protected via s.protected when a token is set)
	s.router.HandleFunc("/api/v1/rca/analyze", s.protected(s.rcaAnalyzeHandler))
	s.router.HandleFunc("/api/v1/rca", s.protected(s.listRCAHandler)) // GET /api/v1/rca - list all RCA reports
	s.router.HandleFunc("/api/v1/rca/", s.protected(s.getRCAHandler)) // GET /api/v1/rca/{id} - get specific RCA report
	s.router.HandleFunc("/api/v1/incidents", s.protected(s.incidentsListHandler))
	s.router.HandleFunc("/api/v1/incidents/", s.protected(s.incidentDetailHandler))
	s.router.HandleFunc("/api/v1/logs/query", s.protected(s.logQueryHandler))
	s.router.HandleFunc("/api/v1/metrics/query", s.protected(s.metricsQueryHandler))
	s.router.HandleFunc("/api/v1/traces/query", s.protected(s.tracesQueryHandler))
	s.router.HandleFunc("/api/v1/topology", s.protected(s.topologyHandler))

	// Conversational assistant
	s.router.HandleFunc("/api/v1/chat", s.protected(s.chatHandler))
	s.router.HandleFunc("/api/v1/conversations", s.protected(s.conversationsHandler))
	s.router.HandleFunc("/api/v1/conversations/", s.protected(s.conversationHandler))

	// Tool-execution approvals
	s.router.HandleFunc("/api/v1/approvals", s.protected(s.approvalsHandler))
	s.router.HandleFunc("/api/v1/approvals/", s.protected(s.approvalHandler))

	// LLM provider catalog and configuration
	s.router.HandleFunc("/api/v1/providers", s.protected(s.providersHandler))
	s.router.HandleFunc("/api/v1/config/llm", s.protected(s.llmConfigHandler))
	s.router.HandleFunc("/api/v1/config/llm/test", s.protected(s.testLLMHandler))
	s.router.HandleFunc("/api/v1/config/llm/models", s.protected(s.llmModelsHandler))
	s.router.HandleFunc("/api/v1/config/datasources", s.protected(s.dataSourcesHandler))
	s.router.HandleFunc("/api/v1/config/notifications", s.protected(s.notificationConfigHandler))
	s.router.HandleFunc("/api/v1/config/notifications/test", s.protected(s.testNotificationHandler))
	s.router.HandleFunc("/api/v1/config/security", s.protected(s.securityConfigHandler))

	// Agent management
	s.router.HandleFunc("/api/v1/agents", s.protected(s.listAgentsHandler))
	s.router.HandleFunc("/api/v1/agents/", s.protected(s.agentHandler))

	// Async agent task lifecycle
	s.router.HandleFunc("POST /api/v1/agents/{id}/tasks", s.protected(s.submitAgentTaskHandler))
	s.router.HandleFunc("GET /api/v1/tasks/{id}", s.protected(s.getAgentTaskHandler))

	// Inspection endpoints
	s.router.HandleFunc("/api/v1/inspection/run", s.protected(s.inspectionHandler))

	// Change tracking (CI/CD deployments, config changes)
	s.router.HandleFunc("/api/v1/changes", s.protected(s.changesHandler))

	// Catch-all for /api/ routes - return JSON 404
	s.router.HandleFunc("/api/", s.apiNotFoundHandler)
}

// Start starts the API server.
func (s *APIServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.settings.Server.Host, s.settings.Server.Port)

	s.server = &http.Server{
		Addr:    addr,
		Handler: s.router,
		// ReadHeaderTimeout caps slow-header (slowloris) attacks cheaply.
		ReadHeaderTimeout: 15 * time.Second,
		// ReadTimeout covers reading the (small JSON) request body.
		ReadTimeout: 30 * time.Second,
		// WriteTimeout MUST exceed the longest handler budget. LLM-backed
		// endpoints (agent orchestration, chat, RCA) legitimately run for
		// minutes — agentHandler alone budgets a 5-minute context — and the
		// coordinator now adds an LLM routing call before the specialist's
		// own LLM call. A 30s WriteTimeout cut those responses mid-flight;
		// 6 minutes leaves headroom above the 5-minute handler deadline while
		// still bounding a stuck connection.
		WriteTimeout: 6 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	s.logger.Info("API server starting",
		observability.String("addr", addr),
	)

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the API server.
func (s *APIServer) Shutdown(ctx context.Context) error {
	if s.server != nil {
		s.logger.Info("API server shutting down")
		return s.server.Shutdown(ctx)
	}
	return nil
}

// ServeHTTP implements http.Handler interface for testing.
func (s *APIServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Create a custom response writer to capture 404 responses
	rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
	s.router.ServeHTTP(rw, r)

	// If no handler was found (status still 0), return 404 JSON response
	if rw.status == 0 {
		s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Endpoint not found", r.URL.Path)
	}
}

// responseWriter wraps http.ResponseWriter to capture status codes.
type responseWriter struct {
	http.ResponseWriter
	status  int
	written bool
}

func (rw *responseWriter) WriteHeader(status int) {
	if !rw.written {
		rw.status = status
		rw.written = true
		rw.ResponseWriter.WriteHeader(status)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// Middleware

// corsMiddleware adds CORS headers to responses.
func (s *APIServer) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// authMiddleware enforces a shared bearer token on protected endpoints.
// When s.apiAuthToken is empty, enforcement is disabled and requests pass
// through unchanged (the startup warning already surfaced the gap). OPTIONS
// preflight is always allowed so CORS keeps working. The token comparison is
// constant-time to avoid leaking it via response timing.
func (s *APIServer) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.apiAuthToken == "" || r.Method == http.MethodOptions {
			next(w, r)
			return
		}

		const prefix = "Bearer "
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, prefix) {
			s.writeErrorResponse(w, http.StatusUnauthorized, "unauthorized", "Missing or malformed Authorization header", "")
			return
		}
		got := strings.TrimSpace(h[len(prefix):])
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.apiAuthToken)) != 1 {
			s.writeErrorResponse(w, http.StatusUnauthorized, "unauthorized", "Invalid API token", "")
			return
		}

		next(w, r)
	}
}

// protected composes CORS + auth for a handler. Use for every /api/v1/*
// endpoint except health, which must stay reachable for liveness probes.
//
// Auth precedence: when the EIAM shared session key is configured, requests
// are authenticated by verifying the EIAM-issued session JWT (and the tenant
// is injected into the context). Otherwise it falls back to the static bearer
// token check. Exactly one of the two runs, chosen once at wiring time.
func (s *APIServer) protected(h http.HandlerFunc) http.HandlerFunc {
	if s.eiamSessionKey != "" {
		return s.corsMiddleware(s.eiamAuthMiddleware(h))
	}
	return s.corsMiddleware(s.authMiddleware(h))
}

// isAllowedOrigin checks if the origin is allowed.
func (s *APIServer) isAllowedOrigin(origin string) bool {
	for _, allowed := range s.corsOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

// loggingMiddleware logs HTTP requests.
func (s *APIServer) loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		s.logger.Info("API request",
			observability.String("method", r.Method),
			observability.String("path", r.URL.Path),
			observability.String("remote_addr", r.RemoteAddr),
		)

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w}
		next(wrapped, r)

		duration := time.Since(start)
		s.logger.Info("API response",
			observability.String("method", r.Method),
			observability.String("path", r.URL.Path),
			observability.Int("status", wrapped.status),
			observability.String("duration", duration.String()),
		)
	}
}

// Handlers

// healthHandler handles health check requests.
func (s *APIServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "1.0.0",
		"data": map[string]interface{}{
			"agents_registered": len(s.agents),
		},
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// rcaAnalyzeHandler handles RCA analysis requests.
func (s *APIServer) rcaAnalyzeHandler(w http.ResponseWriter, r *http.Request) {
	var req RCAAnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
		return
	}

	// Validate request
	if req.IncidentID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "missing_incident_id", "Incident ID is required", "")
		return
	}

	startTime, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_start_time", "Invalid start time format", err.Error())
		return
	}

	endTime, err := time.Parse(time.RFC3339, req.EndTime)
	if err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_end_time", "Invalid end time format", err.Error())
		return
	}

	// Create incident
	incident := &models.Incident{
		ID:               req.IncidentID,
		StartTime:        startTime,
		EndTime:          endTime,
		Severity:         req.Severity,
		AffectedServices: req.AffectedServices,
		Description:      req.Description,
		Source:           "api",
		Labels:           map[string]string{"status": "running"},
		DetectedAt:       time.Now(),
	}

	// Validate incident
	if err := incident.Validate(); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_incident", "Invalid incident data", err.Error())
		return
	}

	s.logger.Info("RCA analysis requested",
		observability.String("incident_id", req.IncidentID),
		observability.String("severity", req.Severity),
	)

	// Capture the tenant from the request BEFORE spawning the goroutine: the
	// request context is cancelled once this handler returns, so the analysis
	// runs on a fresh background context. The tenant must survive that handoff
	// or the report would be stored under the wrong (default) tenant.
	tid := tenant.IDFrom(r.Context())

	// Run RCA analysis in background
	go func() {
		ctx := tenant.WithID(context.Background(), tid)
		report, err := s.rcaEngine.Analyze(ctx, incident)
		if err != nil {
			s.logger.Error("RCA analysis failed",
				observability.String("incident_id", req.IncidentID),
				observability.Err(err),
			)
			// Update incident status to failed
			incident.Labels["status"] = "failed"
			_ = s.rcaStore.UpdateStatus(ctx, req.IncidentID, "failed")
			return
		}

		// Store the report
		if err := s.rcaStore.Store(ctx, report); err != nil {
			s.logger.Error("Failed to store RCA report",
				observability.String("incident_id", req.IncidentID),
				observability.Err(err),
			)
			return
		}

		// Flip status running -> completed so the console reflects the
		// finished analysis. Without this the report is stored but the
		// incident label stays "running" forever.
		_ = s.rcaStore.UpdateStatus(ctx, req.IncidentID, "completed")

		s.logger.Info("RCA analysis completed",
			observability.String("incident_id", req.IncidentID),
			observability.Float64("confidence", report.Confidence),
			observability.Int("candidates", len(report.Candidates)),
		)
	}()

	// Return immediate acceptance response
	response := map[string]interface{}{
		"incident_id": req.IncidentID,
		"status":      "accepted",
		"message":     "RCA analysis started",
	}

	s.writeJSONResponse(w, http.StatusAccepted, response)
}

// scoreBreakdownDTO is the flat, snake_case breakdown the console expects.
type scoreBreakdownDTO struct {
	TimeCorrelation float64 `json:"time_correlation"`
	TopologyScore   float64 `json:"topology_score"`
	ChangeProximity float64 `json:"change_proximity"`
	MetricSeverity  float64 `json:"metric_severity"`
	ErrorFrequency  float64 `json:"error_frequency"`
	TraceEvidence   float64 `json:"trace_evidence"`
}

// hypothesisDTO flattens models.ScoredHypothesis (nested, Go-cased) into the
// flat snake_case shape the frontend's ScoredHypothesis type consumes.
// The domain model is intentionally left untouched; conversion lives at the
// HTTP boundary so Go-side consumers (dingtalk, monitor) keep working.
type hypothesisDTO struct {
	Type        string            `json:"type"`
	Service     string            `json:"service"`
	Description string            `json:"description"`
	Metric      string            `json:"metric,omitempty"`
	ChangeID    string            `json:"change_id,omitempty"`
	Evidence    []string          `json:"evidence"`
	Confidence  float64           `json:"confidence"`
	Score       float64           `json:"score"`
	Breakdown   scoreBreakdownDTO `json:"breakdown"`
}

// toHypothesisDTO converts a scored hypothesis to its flat API representation.
func toHypothesisDTO(sh models.ScoredHypothesis) hypothesisDTO {
	evidence := sh.Hypothesis.Evidence
	if evidence == nil {
		evidence = []string{}
	}
	return hypothesisDTO{
		Type:        string(sh.Hypothesis.Type),
		Service:     sh.Hypothesis.Service,
		Description: sh.Hypothesis.Description,
		Metric:      sh.Hypothesis.Metric,
		ChangeID:    sh.Hypothesis.ChangeID,
		Evidence:    evidence,
		Confidence:  sh.Confidence,
		Score:       sh.Score,
		Breakdown: scoreBreakdownDTO{
			TimeCorrelation: sh.Breakdown.TimeCorrelation,
			TopologyScore:   sh.Breakdown.TopologyScore,
			ChangeProximity: sh.Breakdown.ChangeProximity,
			MetricSeverity:  sh.Breakdown.MetricSeverity,
			ErrorFrequency:  sh.Breakdown.ErrorFrequency,
			TraceEvidence:   sh.Breakdown.TraceEvidence,
		},
	}
}

// toHypothesisDTOs converts a slice of scored hypotheses.
func toHypothesisDTOs(list []models.ScoredHypothesis) []hypothesisDTO {
	out := make([]hypothesisDTO, 0, len(list))
	for _, sh := range list {
		out = append(out, toHypothesisDTO(sh))
	}
	return out
}

// listRCAHandler handles listing all RCA reports.
func (s *APIServer) listRCAHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed", "")
		return
	}

	ctx := r.Context()
	reports, err := s.rcaStore.List(ctx)
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "store_error", "Failed to list reports", err.Error())
		return
	}

	// Convert to frontend format (flat, snake_case DTO).
	type reportResponse struct {
		IncidentID       string          `json:"incident_id"`
		Severity         string          `json:"severity"`
		Description      string          `json:"description"`
		StartTime        string          `json:"start_time"`
		EndTime          string          `json:"end_time,omitempty"`
		AffectedServices []string        `json:"affected_services"`
		Status           string          `json:"status"`
		Candidates       []hypothesisDTO `json:"candidates"`
		TopCandidate     *hypothesisDTO  `json:"top_candidate,omitempty"`
		Confidence       float64         `json:"confidence"`
		DurationMS       int64           `json:"duration_ms,omitempty"`
		GeneratedAt      string          `json:"generated_at,omitempty"`
	}

	result := make([]reportResponse, 0, len(reports))
	for _, report := range reports {
		if report.Incident == nil {
			continue
		}
		status := "running"
		if report.Incident.Labels != nil {
			if s, ok := report.Incident.Labels["status"]; ok {
				status = s
			}
		}

		var top *hypothesisDTO
		if report.TopCandidate != nil {
			dto := toHypothesisDTO(*report.TopCandidate)
			top = &dto
		}

		result = append(result, reportResponse{
			IncidentID:       report.Incident.ID,
			Severity:         report.Incident.Severity,
			Description:      report.Incident.Description,
			StartTime:        report.Incident.StartTime.Format(time.RFC3339),
			EndTime:          formatTimePtr(report.Incident.EndTime),
			AffectedServices: report.Incident.AffectedServices,
			Status:           status,
			Candidates:       toHypothesisDTOs(report.Candidates),
			TopCandidate:     top,
			Confidence:       report.Confidence,
			DurationMS:       int64(report.Duration.Seconds() * 1000),
			GeneratedAt:      formatTimePtr(report.GeneratedAt),
		})
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"reports": result,
		"count":   len(result),
	})
}

// getRCAHandler handles getting a specific RCA report.
func (s *APIServer) getRCAHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed", "")
		return
	}

	// Extract incident ID from path
	incidentID := strings.TrimPrefix(r.URL.Path, "/api/v1/rca/")
	if incidentID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "missing_id", "Incident ID is required", "")
		return
	}

	ctx := r.Context()
	report, err := s.rcaStore.Get(ctx, incidentID)
	if err != nil {
		s.writeErrorResponse(w, http.StatusNotFound, "not_found", "RCA report not found", err.Error())
		return
	}

	// Convert to frontend format
	status := "running"
	if report.Incident.Labels != nil {
		if s, ok := report.Incident.Labels["status"]; ok {
			status = s
		}
	}

	var top *hypothesisDTO
	if report.TopCandidate != nil {
		dto := toHypothesisDTO(*report.TopCandidate)
		top = &dto
	}

	result := map[string]interface{}{
		"incident_id":       report.Incident.ID,
		"severity":          report.Incident.Severity,
		"description":       report.Incident.Description,
		"start_time":        report.Incident.StartTime.Format(time.RFC3339),
		"end_time":          formatTimePtr(report.Incident.EndTime),
		"affected_services": report.Incident.AffectedServices,
		"status":            status,
		"candidates":        toHypothesisDTOs(report.Candidates),
		"top_candidate":     top,
		"confidence":        report.Confidence,
		"duration_ms":       int64(report.Duration.Seconds() * 1000),
		"generated_at":      formatTimePtr(report.GeneratedAt),
	}

	s.writeJSONResponse(w, http.StatusOK, result)
}

// formatTimePtr formats a time pointer or returns empty string if nil.
func formatTimePtr(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// rcaStatusHandler handles RCA status requests.
func (s *APIServer) rcaStatusHandler(w http.ResponseWriter, r *http.Request) {
	incidentID := strings.TrimPrefix(r.URL.Path, "/api/v1/rca/status/")

	if incidentID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "missing_incident_id", "Incident ID is required", "")
		return
	}

	// In real implementation, would query status from store
	response := map[string]interface{}{
		"incident_id": incidentID,
		"status":      "not_found",
		"message":     "Incident not found in active analyses",
	}

	s.writeJSONResponse(w, http.StatusNotFound, response)
}

// logQueryHandler handles log query requests.
func (s *APIServer) logQueryHandler(w http.ResponseWriter, r *http.Request) {
	var req LogQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
		return
	}

	s.logger.Info("Log query requested",
		observability.String("query", req.Query),
	)

	// In real implementation, would query logs
	response := map[string]interface{}{
		"query":  req.Query,
		"status": "accepted",
		"count":  0,
		"logs":   []interface{}{},
	}

	s.writeJSONResponse(w, http.StatusAccepted, response)
}

// metricsQueryHandler handles metrics query requests.
func (s *APIServer) metricsQueryHandler(w http.ResponseWriter, r *http.Request) {
	var req MetricsQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
		return
	}

	s.logger.Info("Metrics query requested",
		observability.String("query", req.Query),
	)

	response := map[string]interface{}{
		"query":  req.Query,
		"status": "accepted",
		"series": []interface{}{},
	}

	s.writeJSONResponse(w, http.StatusAccepted, response)
}

// tracesQueryHandler handles trace query requests.
func (s *APIServer) tracesQueryHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status": "accepted",
		"traces": []interface{}{},
	}

	s.writeJSONResponse(w, http.StatusAccepted, response)
}

// topologyNodeDTO / topologyEdgeDTO are the API-boundary shapes consumed by
// the web console (web/frontend ServiceGraph). They are defined here, at the
// boundary, so the shared rca/models.Topology stays free of transport tags.
type topologyNodeDTO struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Health     string  `json:"health"`
	ErrorRate  float64 `json:"error_rate"`
	Latency    float64 `json:"latency"`
	Throughput float64 `json:"throughput"`
}

type topologyEdgeDTO struct {
	ID         string  `json:"id"`
	FromNodeID string  `json:"from_node_id"`
	ToNodeID   string  `json:"to_node_id"`
	Type       string  `json:"type"`
	ErrorRate  float64 `json:"error_rate"`
	Latency    float64 `json:"latency"`
	Throughput float64 `json:"throughput"`
}

// topologyHandler returns the service dependency graph from the SAME topology
// source the RCA engine uses (real ARMS APM when configured, mock otherwise),
// converted to the console's ServiceGraph shape.
func (s *APIServer) topologyHandler(w http.ResponseWriter, r *http.Request) {
	if s.topologySource == nil {
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"status":   "ok",
			"topology": map[string]interface{}{"nodes": []interface{}{}, "edges": []interface{}{}},
		})
		return
	}

	// Topology discovery and per-service RED metrics are independent, so run
	// them concurrently instead of paying two sequential sets of round-trips.
	// Health is DERIVED from the metrics below rather than fetched separately,
	// which both removes two redundant queries and guarantees the health shown
	// always agrees with the error_rate shown.
	var (
		topo    *models.Topology
		topoErr error
		metrics map[string]sources.ServiceMetrics
		wg      sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		topo, topoErr = s.topologySource.GetTopology(r.Context())
	}()
	// Per-service RED metrics are an OPTIONAL capability: only sources that
	// implement ServiceMetricsProvider (the real ARMS APM source) supply them.
	// The mock source doesn't, so metrics stay zero there rather than faked.
	if mp, ok := s.topologySource.(sources.ServiceMetricsProvider); ok {
		wg.Add(1)
		go func() {
			defer wg.Done()
			metrics, _ = mp.GetServiceMetrics(r.Context())
		}()
	}
	wg.Wait()

	if topoErr != nil {
		s.writeErrorResponse(w, http.StatusBadGateway, "topology_error", "Failed to fetch service topology", topoErr.Error())
		return
	}

	// deriveHealth turns RED metrics into a health label. A node with no live
	// traffic (or an infra node, which has no metrics) is "unknown" rather than
	// optimistically "healthy".
	deriveHealth := func(m sources.ServiceMetrics, ok bool) string {
		if !ok || m.Throughput <= 0 {
			return "unknown"
		}
		if m.ErrorRate > 0 {
			return "degraded"
		}
		return "healthy"
	}

	nodes := make([]topologyNodeDTO, 0, len(topo.Nodes))
	for _, n := range topo.Nodes {
		m, ok := metrics[n.Name]
		nodes = append(nodes, topologyNodeDTO{
			ID:         n.Name,
			Name:       n.Name,
			Type:       n.Type,
			Health:     deriveHealth(m, ok),
			ErrorRate:  m.ErrorRate,
			Latency:    m.LatencyMs,
			Throughput: m.Throughput,
		})
	}

	edges := make([]topologyEdgeDTO, 0, len(topo.Edges))
	for i, e := range topo.Edges {
		edges = append(edges, topologyEdgeDTO{
			ID:         fmt.Sprintf("e%d", i),
			FromNodeID: e.From,
			ToNodeID:   e.To,
			Type:       e.Type,
		})
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"topology": map[string]interface{}{"nodes": nodes, "edges": edges},
	})
}

// changeItemDTO is the wire format for one real change record returned by
// /api/v1/changes. It is a faithful projection of models.Change — no invented
// fields (no fabricated author/status/correlation).
type changeItemDTO struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"`
	System  string            `json:"system"`
	Service string            `json:"service"`
	Message string            `json:"message"`
	Time    string            `json:"time"`
	Details map[string]string `json:"details,omitempty"`
}

// changeSystem derives the originating-system label for a change record. Both
// real change sources (K8s deployment conditions, e-cam k8s-events) belong to
// the Kubernetes family; an explicit "system" detail wins when a future source
// provides one, otherwise it is honestly reported as "kubernetes".
func changeSystem(c models.Change) string {
	if c.Details != nil {
		if sys := strings.TrimSpace(c.Details["system"]); sys != "" {
			return sys
		}
	}
	return "kubernetes"
}

// changesHandler lists REAL change events from the configured change source
// (K8s deployment conditions / e-cam k8s-events). Query params: explicit
// start/end (RFC3339, take precedence) or hours (default 24, capped at 168).
//
// Hard contract: when no real change source is active (changeSourceReal false,
// i.e. the dimension fell back to the honest empty source), the handler returns
// an empty list with an explanatory hint. It NEVER queries the mock/empty
// fallback and NEVER fabricates records. A real query failure returns 502 with
// the upstream error — never a silent fallback to demo data.
func (s *APIServer) changesHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := time.Now()
	var start, end time.Time

	startStr := strings.TrimSpace(q.Get("start"))
	endStr := strings.TrimSpace(q.Get("end"))
	if startStr != "" || endStr != "" {
		var err error
		if startStr != "" {
			start, err = time.Parse(time.RFC3339, startStr)
			if err != nil {
				s.writeErrorResponse(w, http.StatusBadRequest, "invalid_start", "start must be RFC3339 (e.g. 2006-01-02T15:04:05Z)", err.Error())
				return
			}
		} else {
			start = now.Add(-24 * time.Hour)
		}
		if endStr != "" {
			end, err = time.Parse(time.RFC3339, endStr)
			if err != nil {
				s.writeErrorResponse(w, http.StatusBadRequest, "invalid_end", "end must be RFC3339 (e.g. 2006-01-02T15:04:05Z)", err.Error())
				return
			}
		} else {
			end = now
		}
	} else {
		hours := 24
		if h := strings.TrimSpace(q.Get("hours")); h != "" {
			parsed, perr := strconv.Atoi(h)
			if perr != nil || parsed <= 0 {
				s.writeErrorResponse(w, http.StatusBadRequest, "invalid_hours", "hours must be a positive integer", h)
				return
			}
			hours = parsed
		}
		if hours > 168 {
			hours = 168
		}
		end = now
		start = end.Add(-time.Duration(hours) * time.Hour)
	}

	limit := 200
	if l := strings.TrimSpace(q.Get("limit")); l != "" {
		parsed, perr := strconv.Atoi(l)
		if perr != nil || parsed <= 0 {
			s.writeErrorResponse(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer", l)
			return
		}
		limit = parsed
	}
	if limit > 500 {
		limit = 500
	}

	// No real change source → honest empty list, never the mock/empty fallback.
	if !s.changeSourceReal || s.changeSource == nil {
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"changes": []changeItemDTO{},
			"count":   0,
			"source": map[string]interface{}{
				"real": false,
				"type": "",
				"hint": "Real change source not configured (K8S_ENDPOINT/K8S_TOKEN/K8S_NAMESPACE or platform event source)",
			},
		})
		return
	}

	// Quantize both ends so repeated "last N hours" loads share an entry, and
	// bind the key to the source instance so a swapped source never serves
	// another source's records.
	cacheKey := fmt.Sprintf("%d|%d",
		start.Truncate(30*time.Second).Unix(), end.Truncate(30*time.Second).Unix())
	items, stats, cached := s.changeCacheGet(cacheKey)
	if !cached {
		changes, err := s.changeSource.QueryChanges(r.Context(), start, end)
		if err != nil {
			s.writeErrorResponse(w, http.StatusBadGateway, "change_source_error", "Failed to query the real change source", err.Error())
			return
		}

		items = make([]changeItemDTO, 0, len(changes))
		for _, c := range changes {
			items = append(items, changeItemDTO{
				ID:      c.ID,
				Kind:    c.Type,
				System:  changeSystem(c),
				Service: c.Service,
				Message: c.Description,
				Time:    c.Timestamp.Format(time.RFC3339),
				Details: c.Details,
			})
		}
		// Time descending (newest first); RFC3339 strings sort lexicographically.
		sort.Slice(items, func(i, j int) bool { return items[i].Time > items[j].Time })
		stats = changeStats(items)
		s.changeCacheSet(cacheKey, items, stats)
	}

	total := len(items)
	truncated := total > limit
	if truncated {
		items = items[:limit]
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"changes":   items,
		"count":     len(items),
		"total":     total,
		"truncated": truncated,
		"stats":     stats,
		"source":    map[string]interface{}{"real": true, "type": "kubernetes"},
		"window": map[string]interface{}{
			"start": start.Format(time.RFC3339),
			"end":   end.Format(time.RFC3339),
		},
	})
}

// changeStats aggregates window-wide statistics over the FULL source result
// (before any limit), so console stats stay honest even when the payload is
// truncated for render safety.
func changeStats(items []changeItemDTO) map[string]interface{} {
	byKind := map[string]int{}
	services := map[string]int{}
	for _, it := range items {
		kind := it.Kind
		if kind == "" {
			kind = "unknown"
		}
		byKind[kind]++
		if it.Service != "" {
			services[it.Service]++
		}
	}
	type svcCount struct {
		name  string
		count int
	}
	top := make([]svcCount, 0, len(services))
	for name, count := range services {
		top = append(top, svcCount{name: name, count: count})
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].count != top[j].count {
			return top[i].count > top[j].count
		}
		return top[i].name < top[j].name
	})
	topNames := make([]string, 0, 3)
	for _, sc := range top {
		if len(topNames) == 3 {
			break
		}
		topNames = append(topNames, sc.name)
	}
	return map[string]interface{}{
		"total":        len(items),
		"by_kind":      byKind,
		"services":     len(services),
		"top_services": topNames,
	}
}

const changeCacheTTL = 60 * time.Second

func (s *APIServer) changeCacheGet(key string) ([]changeItemDTO, map[string]interface{}, bool) {
	s.changeCacheMu.Lock()
	defer s.changeCacheMu.Unlock()
	if s.changeCacheSrc != s.changeSource || s.changeCacheKey != key ||
		time.Since(s.changeCacheAt) > changeCacheTTL {
		return nil, nil, false
	}
	return s.changeCacheItems, s.changeCacheStats, true
}

func (s *APIServer) changeCacheSet(key string, items []changeItemDTO, stats map[string]interface{}) {
	s.changeCacheMu.Lock()
	defer s.changeCacheMu.Unlock()
	s.changeCacheSrc = s.changeSource
	s.changeCacheKey = key
	s.changeCacheAt = time.Now()
	s.changeCacheItems = items
	s.changeCacheStats = stats
}

// listAgentsHandler handles agent listing requests. It returns both a plain
// id list (for backward compatibility) and rich metadata for each agent.
func (s *APIServer) listAgentsHandler(w http.ResponseWriter, r *http.Request) {
	ids := make([]string, 0, len(s.agents))
	details := make([]*agentEntry, 0, len(s.agents))
	for id := range s.agents {
		ids = append(ids, id)
		if meta, ok := s.agentMeta[id]; ok {
			details = append(details, meta)
		}
	}

	response := map[string]interface{}{
		"agents":  ids,
		"count":   len(ids),
		"details": details,
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// agentHandler handles individual agent requests (GET info, POST execute).
func (s *APIServer) agentHandler(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/")

	if agentID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "missing_agent_id", "Agent ID is required", "")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getAgentInfo(w, agentID)
	case http.MethodPost:
		s.executeAgentTask(w, r, agentID)
	default:
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
	}
}

// inspectionHandler handles inspection requests.
func (s *APIServer) inspectionHandler(w http.ResponseWriter, r *http.Request) {
	var req InspectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
		return
	}

	response := map[string]interface{}{
		"inspection_id": fmt.Sprintf("insp-%d", time.Now().UnixNano()),
		"status":        "accepted",
		"type":          req.Type,
	}

	s.writeJSONResponse(w, http.StatusAccepted, response)
}

// apiNotFoundHandler handles API requests that don't match any registered route.
func (s *APIServer) apiNotFoundHandler(w http.ResponseWriter, r *http.Request) {
	s.writeErrorResponse(w, http.StatusNotFound, "not_found", "API endpoint not found", r.URL.Path)
}

// Response helpers

// writeJSONResponse writes a JSON response.
func (s *APIServer) writeJSONResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode response",
			observability.Err(err),
		)
	}
}

// writeErrorResponse writes an error response.
func (s *APIServer) writeErrorResponse(w http.ResponseWriter, status int, code, message, details string) {
	response := map[string]interface{}{
		"success": false,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
			"details": details,
		},
	}

	s.writeJSONResponse(w, status, response)
}

// Request/Response types

type RCAAnalysisRequest struct {
	IncidentID       string   `json:"incident_id"`
	StartTime        string   `json:"start_time"`
	EndTime          string   `json:"end_time"`
	Severity         string   `json:"severity"`
	AffectedServices []string `json:"affected_services"`
	Description      string   `json:"description,omitempty"`
}

type LogQueryRequest struct {
	Query     string `json:"query"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Limit     int    `json:"limit,omitempty"`
	Service   string `json:"service,omitempty"`
	Level     string `json:"level,omitempty"`
}

type MetricsQueryRequest struct {
	Query     string            `json:"query"`
	StartTime string            `json:"start_time"`
	EndTime   string            `json:"end_time"`
	Step      string            `json:"step,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type InspectionRequest struct {
	Type         string   `json:"type"`
	Providers    []string `json:"providers,omitempty"`
	Services     []string `json:"services,omitempty"`
	ClusterURL   string   `json:"cluster_url,omitempty"`
	ComplianceID string   `json:"compliance_id,omitempty"`
}
