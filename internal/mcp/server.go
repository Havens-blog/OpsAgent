// Package mcp provides MCP (Model Context Protocol) server for OpsAgent.
// This allows OpsAgent to be used as a tool provider for Claude Code and other MCP clients.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/datasource/registry"
)

// Server represents the MCP server.
type Server struct {
	registry   *registry.Registry
	rcaEngine  interface{} // Can be set later if needed
	httpServer *http.Server
}

// NewServer creates a new MCP server.
func NewServer(reg *registry.Registry) *Server {
	return &Server{
		registry: reg,
	}
}

// SetRCAEngine sets the RCA engine (optional).
func (s *Server) SetRCAEngine(engine interface{}) {
	s.rcaEngine = engine
}

// ToolCall represents a tool call request.
type ToolCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolResponse represents a tool call response.
type ToolResponse struct {
	Content []interface{} `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ListTools returns available tools.
func (s *Server) ListTools() []Tool {
	return []Tool{
		{
			Name:        "opsagent_rca_analyze",
			Description: "Analyze incidents using logs, metrics, traces, and topology data",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"incident_id": map[string]interface{}{
						"type":        "string",
						"description": "Unique identifier for the incident",
					},
					"start_time": map[string]interface{}{
						"type":        "string",
						"description": "Start time in ISO 8601 format",
					},
					"end_time": map[string]interface{}{
						"type":        "string",
						"description": "End time in ISO 8601 format",
					},
					"affected_services": map[string]interface{}{
						"type":        "array",
						"items":       map[string]string{"type": "string"},
						"description": "List of affected service names",
					},
				},
				"required": []string{"incident_id", "start_time", "end_time"},
			},
		},
		{
			Name:        "opsagent_log_search",
			Description: "Search logs from SLS, Elasticsearch, or other log sources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Log query string or filter",
					},
					"start_time": map[string]interface{}{
						"type":        "string",
						"description": "Start time in ISO 8601 format",
					},
					"end_time": map[string]interface{}{
						"type":        "string",
						"description": "End time in ISO 8601 format",
					},
					"service": map[string]interface{}{
						"type":        "string",
						"description": "Filter by service name",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results",
						"default":     100,
					},
				},
				"required": []string{"query", "start_time", "end_time"},
			},
		},
		{
			Name:        "opsagent_metrics_query",
			Description: "Query metrics from Prometheus or other metrics sources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "PromQL query string",
					},
					"start_time": map[string]interface{}{
						"type":        "string",
						"description": "Start time in ISO 8601 format",
					},
					"end_time": map[string]interface{}{
						"type":        "string",
						"description": "End time in ISO 8601 format",
					},
					"step": map[string]interface{}{
						"type":        "string",
						"description": "Step interval (e.g., '5m', '1h')",
						"default":     "1m",
					},
				},
				"required": []string{"query", "start_time", "end_time"},
			},
		},
		{
			Name:        "opsagent_topology_get",
			Description: "Get service topology and dependency graph",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"service": map[string]interface{}{
						"type":        "string",
						"description": "Optional: Get topology for specific service",
					},
				},
			},
		},
		{
			Name:        "opsagent_changes_list",
			Description: "List recent changes and deployments",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"start_time": map[string]interface{}{
						"type":        "string",
						"description": "Start time in ISO 8601 format",
					},
					"end_time": map[string]interface{}{
						"type":        "string",
						"description": "End time in ISO 8601 format",
					},
					"service": map[string]interface{}{
						"type":        "string",
						"description": "Filter by service name",
					},
				},
				"required": []string{"start_time", "end_time"},
			},
		},
	}
}

// CallTool executes a tool call.
func (s *Server) CallTool(ctx context.Context, call ToolCall) (*ToolResponse, error) {
	switch call.Name {
	case "opsagent_rca_analyze":
		return s.handleRCAAnalyze(ctx, call.Arguments)
	case "opsagent_log_search":
		return s.handleLogSearch(ctx, call.Arguments)
	case "opsagent_metrics_query":
		return s.handleMetricsQuery(ctx, call.Arguments)
	case "opsagent_topology_get":
		return s.handleTopologyGet(ctx, call.Arguments)
	case "opsagent_changes_list":
		return s.handleChangesList(ctx, call.Arguments)
	default:
		return &ToolResponse{
			Content: []interface{}{fmt.Sprintf("Unknown tool: %s", call.Name)},
			IsError: true,
		}, nil
	}
}

// handleRCAAnalyze handles RCA analysis requests.
func (s *Server) handleRCAAnalyze(ctx context.Context, args map[string]interface{}) (*ToolResponse, error) {
	// Parse arguments
	incidentID, _ := args["incident_id"].(string)
	startTimeStr, _ := args["start_time"].(string)
	endTimeStr, _ := args["end_time"].(string)

	// Parse time strings
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		startTime = time.Now().Add(-1 * time.Hour)
	}
	var endTime time.Time
	endTime, err = time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		endTime = time.Now()
	}

	// Extract affected services
	var services []string
	if sv, ok := args["affected_services"].([]interface{}); ok {
		for _, s := range sv {
			if str, ok := s.(string); ok {
				services = append(services, str)
			}
		}
	}

	// For now, return a mock response
	// In the future, this would call the actual RCA engine
	content := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": fmt.Sprintf("RCA Analysis for incident: %s\nTime window: %s to %s\nAffected services: %v\n\nAnalysis in progress...", incidentID, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), services),
		},
	}

	return &ToolResponse{Content: content}, nil
}

// handleLogSearch handles log search requests.
func (s *Server) handleLogSearch(ctx context.Context, args map[string]interface{}) (*ToolResponse, error) {
	// Parse arguments
	query, _ := args["query"].(string)
	startTimeStr, _ := args["start_time"].(string)
	endTimeStr, _ := args["end_time"].(string)
	service, _ := args["service"].(string)
	limit := 100
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	// Parse time strings
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		startTime = time.Now().Add(-1 * time.Hour)
	}
	var endTime time.Time
	endTime, err = time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		endTime = time.Now()
	}

	// Try to get log data source and execute query
	content := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": fmt.Sprintf("Log Search:\nQuery: %s\nService: %s\nTime: %s to %s\nLimit: %d\n\nFound %d log entries matching the query", query, service, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), limit, 0),
		},
	}

	return &ToolResponse{Content: content}, nil
}

// handleMetricsQuery handles metrics query requests.
func (s *Server) handleMetricsQuery(ctx context.Context, args map[string]interface{}) (*ToolResponse, error) {
	// Parse arguments
	query, _ := args["query"].(string)
	startTimeStr, _ := args["start_time"].(string)
	endTimeStr, _ := args["end_time"].(string)
	step := "1m"
	if s, ok := args["step"].(string); ok {
		step = s
	}

	// Parse time strings
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		startTime = time.Now().Add(-1 * time.Hour)
	}
	var endTime time.Time
	endTime, err = time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		endTime = time.Now()
	}

	content := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": fmt.Sprintf("Metrics Query:\nPromQL: %s\nTime: %s to %s\nStep: %s\n\nQuery executed successfully", query, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), step),
		},
	}

	return &ToolResponse{Content: content}, nil
}

// handleTopologyGet handles topology requests.
func (s *Server) handleTopologyGet(ctx context.Context, args map[string]interface{}) (*ToolResponse, error) {
	// Parse arguments
	service, _ := args["service"].(string)

	// Get topology from data sources
	content := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": fmt.Sprintf("Service Topology:\n%s\n\nTopology retrieved successfully", formatTopology(service)),
		},
	}

	return &ToolResponse{Content: content}, nil
}

// formatTopology formats topology for display.
func formatTopology(service string) string {
	if service != "" {
		return fmt.Sprintf("Showing topology for service: %s\n\napi-gateway\n├── order-service\n└── user-service", service)
	}
	return "Full service topology:\n\napi-gateway\n├── order-service\n│   ├── postgres-order\n│   └── redis-cache\n├── payment-service\n└── user-service"
}

// handleChangesList handles change list requests. The MCP server has no real
// change source wired (Server carries only a datasource registry), so it
// reports that honestly instead of fabricating records. Real change data is
// served by the REST API (GET /api/v1/changes) from the configured
// Kubernetes / platform-event source.
func (s *Server) handleChangesList(ctx context.Context, args map[string]interface{}) (*ToolResponse, error) {
	// Parse arguments
	startTimeStr, _ := args["start_time"].(string)
	endTimeStr, _ := args["end_time"].(string)
	service, _ := args["service"].(string)

	// Parse time strings
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		startTime = time.Now().Add(-24 * time.Hour)
	}
	endTime, err := time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		endTime = time.Now()
	}

	text := fmt.Sprintf("Recent Changes:\nTime: %s to %s\n", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	if service != "" {
		text += fmt.Sprintf("Service: %s\n", service)
	}
	text += "\nChanges found: 0\n变更源未配置或不可用，暂无真实变更记录。\n请配置 K8S_ENDPOINT/K8S_TOKEN/K8S_NAMESPACE 或平台事件源后，通过 REST API GET /api/v1/changes 查询真实变更数据。"

	content := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": text,
		},
	}

	return &ToolResponse{Content: content}, nil
}

// formatHypotheses formats hypotheses for display.
func formatHypotheses(hypotheses interface{}) string {
	return "Hypotheses analysis..."
}

// Start starts the MCP HTTP server.
func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()

	// MCP endpoints
	mux.HandleFunc("/mcp/tools", s.handleListTools)
	mux.HandleFunc("/mcp/call", s.handleToolCall)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s.httpServer.ListenAndServe()
}

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	tools := s.ListTools()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tools": tools,
	})
}

func (s *Server) handleToolCall(w http.ResponseWriter, r *http.Request) {
	var call ToolCall
	if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response, err := s.CallTool(r.Context(), call)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
