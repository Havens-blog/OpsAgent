// Package interfaces provides REST API handlers for OpsAgent.
package interfaces

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/observability"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// APIServer represents the REST API server.
type APIServer struct {
	settings   *config.Settings
	router     *http.ServeMux
	server     *http.Server
	logger     *observability.Logger
	agentMap   map[string]interface{} // Registered agents
	corsOrigins []string
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
	}

	server.setupRoutes()

	return server, nil
}

// setupRoutes configures all API routes.
func (s *APIServer) setupRoutes() {
	// Health check
	s.router.HandleFunc("/health", s.corsMiddleware(s.healthHandler))

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
	s.router.ServeHTTP(w, r)
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

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
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
	response := map[string]interface{}{
		"status":  "ok",
		"topology": map[string]interface{}{
			"nodes": []interface{}{},
			"edges": []interface{}{},
		},
	}

	s.writeJSONResponse(w, http.StatusOK, response)
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
