// Package interfaces provides REST API handlers for OpsAgent.
package interfaces

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/change"
	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/observability"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/topology"
)

// APIServer represents the REST API server.
type APIServer struct {
	settings   *config.Settings
	router     *http.ServeMux
	server     *http.Server
	logger     *observability.Logger
	agentMap   map[string]interface{} // Registered agents
	corsOrigins []string

	// Engine integrations — wired so the console returns real data instead
	// of placeholders. Each is nil-safe: handlers fall back to empty results
	// when an integration is not configured.
	topologyClient *topology.ARMSClient
	changeStore    change.ChangeStore
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

	server := &APIServer{
		settings:   settings,
		router:     http.NewServeMux(),
		logger:     logger,
		agentMap:   make(map[string]interface{}),
		corsOrigins: []string{"http://localhost:3000", "http://localhost:8080"},

		// Default integrations. In production these would be constructed
		// from settings (credentials, endpoints); the defaults use the
		// in-memory / mock-backed implementations so the API is functional
		// out of the box.
		topologyClient: topology.NewARMSClient(),
		changeStore:    change.NewMemoryStore(),
	}

	// Seed demonstrative change data so /api/v1/changes returns something
	// meaningful before real collectors are registered.
	seedDemoChanges(server.changeStore)

	server.setupRoutes()

	return server, nil
}

// seedDemoChanges populates the change store with representative deployment
// and config-change records so the console has data on a fresh install.
func seedDemoChanges(store change.ChangeStore) {
	if store == nil {
		return
	}
	ctx := context.Background()
	now := time.Now()

	deployments := []change.DeploymentEvent{
		{
			ID: "gitlab-deploy-001", CICDSystem: change.CICDSystemGitLabCI,
			Project: "myproject/backend", Environment: "production", Service: "order-service",
			CommitSHA: "abc123def456", CommitMessage: "Fix: Database connection pool exhaustion",
			CommitAuthor: "John Doe", Branch: "main", StartedAt: now.Add(-12 * time.Minute),
			CompletedAt: now.Add(-9 * time.Minute), Duration: 3 * time.Minute, Status: change.DeploymentStatusSuccess,
		},
		{
			ID: "github-deploy-002", CICDSystem: change.CICDSystemGitHubActions,
			Project: "myorg/backend", Environment: "production", Service: "payment-service",
			CommitSHA: "def456ghi789", CommitMessage: "feat: Add payment gateway integration",
			CommitAuthor: "Jane Smith", Branch: "main", StartedAt: now.Add(-45 * time.Minute),
			CompletedAt: now.Add(-40 * time.Minute), Duration: 5 * time.Minute, Status: change.DeploymentStatusRolledBack,
		},
		{
			ID: "gitlab-deploy-003", CICDSystem: change.CICDSystemGitLabCI,
			Project: "myproject/backend", Environment: "production", Service: "user-service",
			CommitSHA: "999aaa000bbb", CommitMessage: "chore: bump dependencies",
			CommitAuthor: "CI Bot", Branch: "main", StartedAt: now.Add(-5 * time.Hour),
			Status: change.DeploymentStatusFailed,
		},
	}
	for _, d := range deployments {
		_ = store.StoreDeployment(ctx, d)
	}
}

// setupRoutes configures all API routes.
func (s *APIServer) setupRoutes() {
	// Health check (both root and versioned paths for console compatibility)
	s.router.HandleFunc("/health", s.corsMiddleware(s.healthHandler))
	s.router.HandleFunc("/api/v1/health", s.corsMiddleware(s.healthHandler))

	// API v1 routes
	s.router.HandleFunc("/api/v1/rca/analyze", s.corsMiddleware(s.rcaAnalyzeHandler))
	s.router.HandleFunc("/api/v1/rca/status/", s.corsMiddleware(s.rcaStatusHandler))
	s.router.HandleFunc("/api/v1/logs/query", s.corsMiddleware(s.logQueryHandler))
	s.router.HandleFunc("/api/v1/metrics/query", s.corsMiddleware(s.metricsQueryHandler))
	s.router.HandleFunc("/api/v1/traces/query", s.corsMiddleware(s.tracesQueryHandler))
	s.router.HandleFunc("/api/v1/topology", s.corsMiddleware(s.topologyHandler))

	// Agent management
	s.router.HandleFunc("/api/v1/agents", s.corsMiddleware(s.listAgentsHandler))
	s.router.HandleFunc("/api/v1/agents/", s.corsMiddleware(s.agentHandler))

	// Inspection endpoints
	s.router.HandleFunc("/api/v1/inspection/run", s.corsMiddleware(s.inspectionHandler))

	// Change tracking (CI/CD deployments, config changes)
	s.router.HandleFunc("/api/v1/changes", s.corsMiddleware(s.changesHandler))

	// Catch-all for /api/ routes - return JSON 404
	s.router.HandleFunc("/api/", s.apiNotFoundHandler)
}

// Start starts the API server.
func (s *APIServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.settings.Server.Host, s.settings.Server.Port)

	s.server = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
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
	status int
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
			"agents_registered": len(s.agentMap),
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
		ID:              req.IncidentID,
		StartTime:       startTime,
		EndTime:         endTime,
		Severity:        req.Severity,
		AffectedServices: req.AffectedServices,
		Description:     req.Description,
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

	// In real implementation, would delegate to RCA engine
	// For now, return acceptance response
	response := map[string]interface{}{
		"incident_id": req.IncidentID,
		"status":      "accepted",
		"message":     "RCA analysis queued for processing",
		"estimated_duration": "2-5 minutes",
	}

	s.writeJSONResponse(w, http.StatusAccepted, response)
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

// topologyHandler handles topology requests.
func (s *APIServer) topologyHandler(w http.ResponseWriter, r *http.Request) {
	if s.topologyClient == nil {
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"status":   "ok",
			"topology": map[string]interface{}{"nodes": []interface{}{}, "edges": []interface{}{}},
		})
		return
	}

	// Default to the last hour when no time range is supplied.
	window := topology.TimeWindow{
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
	}

	graph, err := s.topologyClient.GetServiceGraph(r.Context(), window)
	if err != nil {
		s.writeErrorResponse(w, http.StatusBadGateway, "topology_error", "Failed to fetch service topology", err.Error())
		return
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"topology": graph,
	})
}

// changesHandler handles change-event listing requests (CI/CD deployments).
func (s *APIServer) changesHandler(w http.ResponseWriter, r *http.Request) {
	if s.changeStore == nil {
		s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
			"deployments": []interface{}{},
			"count":       0,
		})
		return
	}

	filter := change.DeploymentFilter{Limit: 50}
	deployments, err := s.changeStore.ListDeployments(r.Context(), filter)
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "change_error", "Failed to list changes", err.Error())
		return
	}

	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"deployments": deployments,
		"count":       len(deployments),
	})
}

// listAgentsHandler handles agent listing requests.
func (s *APIServer) listAgentsHandler(w http.ResponseWriter, r *http.Request) {
	agents := make([]string, 0, len(s.agentMap))
	for name := range s.agentMap {
		agents = append(agents, name)
	}

	response := map[string]interface{}{
		"agents": agents,
		"count":  len(agents),
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// agentHandler handles individual agent requests.
func (s *APIServer) agentHandler(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/")

	if agentID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "missing_agent_id", "Agent ID is required", "")
		return
	}

	switch r.Method {
	case "GET":
		s.getAgentInfo(w, agentID)
	case "POST":
		s.executeAgentTask(w, agentID)
	default:
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
	}
}

// getAgentInfo returns information about an agent.
func (s *APIServer) getAgentInfo(w http.ResponseWriter, agentID string) {
	response := map[string]interface{}{
		"agent_id": agentID,
		"status":   "not_found",
	}

	s.writeJSONResponse(w, http.StatusNotFound, response)
}

// executeAgentTask executes a task on an agent.
func (s *APIServer) executeAgentTask(w http.ResponseWriter, agentID string) {
	response := map[string]interface{}{
		"agent_id": agentID,
		"status":   "not_implemented",
	}

	s.writeJSONResponse(w, http.StatusNotImplemented, response)
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
	w.Header().Set("Content-Type", "application/json")
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
	Query     string `json:"query"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Step      string `json:"step,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type InspectionRequest struct {
	Type            string   `json:"type"`
	Providers       []string `json:"providers,omitempty"`
	Services        []string `json:"services,omitempty"`
	ClusterURL      string   `json:"cluster_url,omitempty"`
	ComplianceID    string   `json:"compliance_id,omitempty"`
}
