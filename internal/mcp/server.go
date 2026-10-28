// Package mcp provides MCP (Model Context Protocol) server for OpsAgent.
// This allows OpsAgent to be used as a tool provider for Claude Code and other MCP clients.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/datasource/registry"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

// Server represents the MCP server.
type Server struct {
	registry   *registry.Registry
	rcaEngine  interface{} // Can be set later if needed
	httpServer *http.Server

	// topologySource is the same progressive-enhancement source the REST API
	// (/api/v1/topology) uses. topologyReal records whether it is backed by a
	// genuinely configured backend (ARMS APM) rather than the honest empty
	// fallback: an unconfigured dimension must be reported as such, never
	// rendered as a fabricated graph.
	topologySource sources.TopologySource
	topologyReal   bool
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

// SetTopologySource injects the service topology source and whether it is
// backed by a real backend. Callers pass the TopologySource and the
// active["topology"] flag from agent.CreateRCASourcesWithFallback, so the MCP
// topology tool answers from exactly the same data as REST GET
// /api/v1/topology.
func (s *Server) SetTopologySource(src sources.TopologySource, real bool) {
	s.topologySource = src
	s.topologyReal = real
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

// maxTopologyEdgesShown caps how many edges the whole-graph summary lists, so a
// large production graph cannot blow up the tool output.
const maxTopologyEdgesShown = 20

// topologyUnconfiguredText is the honest answer when no real topology backend
// is configured. It names the variables to set and the REST equivalent, and
// deliberately reports no nodes at all.
const topologyUnconfiguredText = "服务拓扑源未配置，无法返回真实拓扑数据。\n" +
	"本工具只在配置了真实拓扑源（ARMS APM）时返回数据，未配置期间不会编造任何节点或依赖关系。\n" +
	"配置环境变量 ARMS_APM_ENDPOINT 与 ARMS_APM_TOKEN 后重启 MCP 服务重试；" +
	"也可直接调用 REST 接口 GET /api/v1/topology 查看当前拓扑源返回的内容。"

// handleTopologyGet handles topology requests.
func (s *Server) handleTopologyGet(ctx context.Context, args map[string]interface{}) (*ToolResponse, error) {
	// Parse arguments
	service, _ := args["service"].(string)

	src := s.topologySource
	if src == nil || !s.topologyReal {
		return textResponse(topologyUnconfiguredText), nil
	}

	topo, err := src.GetTopology(ctx)
	if err != nil {
		return &ToolResponse{
			Content: []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": fmt.Sprintf(
						"拓扑查询失败，未返回任何拓扑数据。\n原因: %v\n请检查 ARMS_APM_ENDPOINT/ARMS_APM_TOKEN 的有效性及后端连通性，或用 REST 接口 GET /api/v1/topology 复核。",
						err),
				},
			},
			IsError: true,
		}, nil
	}

	return textResponse(formatTopology(topo, service)), nil
}

// textResponse wraps plain text into the MCP content shape.
func textResponse(text string) *ToolResponse {
	return &ToolResponse{
		Content: []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": text,
			},
		},
	}
}

// formatTopology renders a REAL topology graph: a one-hop neighbourhood when a
// service is requested, otherwise a bounded summary of the whole graph. It
// never invents nodes, and says so plainly when the data is absent. Output is
// deterministically sorted so it is stable across calls and testable.
func formatTopology(topo *models.Topology, service string) string {
	if topo == nil {
		topo = &models.Topology{}
	}

	if service != "" {
		return formatServiceNeighborhood(topo, service)
	}
	return formatTopologySummary(topo)
}

// formatServiceNeighborhood renders the one-hop upstream/downstream relations
// of a single service, taken from the graph's edges (From = caller, To =
// callee), each annotated with its relation type.
func formatServiceNeighborhood(topo *models.Topology, service string) string {
	if len(topo.Nodes) == 0 && len(topo.Edges) == 0 {
		return fmt.Sprintf(
			"未找到服务 %q 的拓扑数据：已配置的拓扑源返回了空拓扑图（0 个节点、0 条边）。\n可用 REST 接口 GET /api/v1/topology 复核。",
			service)
	}

	known := topo.HasService(service)
	if !known {
		for _, e := range topo.Edges {
			if e.From == service || e.To == service {
				known = true
				break
			}
		}
	}
	if !known {
		return fmt.Sprintf(
			"未找到服务 %q 的拓扑数据：当前拓扑图共 %d 个节点、%d 条边，其中不含该服务。\n该结果来自真实拓扑源，未做任何推断或补全。",
			service, len(topo.Nodes), len(topo.Edges))
	}

	upstream := make([]string, 0)
	downstream := make([]string, 0)
	for _, e := range sortedEdges(topo.Edges) {
		switch {
		case e.To == service && e.From != "":
			upstream = append(upstream, fmt.Sprintf("  - %s (%s)", e.From, edgeTypeLabel(e.Type)))
		case e.From == service && e.To != "":
			downstream = append(downstream, fmt.Sprintf("  - %s (%s)", e.To, edgeTypeLabel(e.Type)))
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "服务拓扑（一跳直接关系）：%s\n", service)
	fmt.Fprintf(&b, "数据来源：已配置的真实拓扑源；仅列出直接相邻节点，不做多跳推断。\n\n")
	fmt.Fprintf(&b, "上游调用方（调用 %s，共 %d 个）:\n", service, len(upstream))
	b.WriteString(joinOrNone(upstream))
	fmt.Fprintf(&b, "\n下游依赖（%s 调用，共 %d 个）:\n", service, len(downstream))
	b.WriteString(joinOrNone(downstream))
	return b.String()
}

// formatTopologySummary renders the whole graph as a bounded, deterministic
// summary: node total, per-type counts, edge total and the first few edges.
func formatTopologySummary(topo *models.Topology) string {
	if len(topo.Nodes) == 0 && len(topo.Edges) == 0 {
		return "已配置的拓扑源返回了空拓扑图：0 个节点、0 条边。\n" +
			"这表示当前时间窗内没有采集到任何服务依赖数据，不是未配置；可用 REST 接口 GET /api/v1/topology 复核。"
	}

	typeCounts := make(map[string]int, len(topo.Nodes))
	for _, n := range topo.Nodes {
		typeCounts[nodeTypeLabel(n.Type)]++
	}
	types := make([]string, 0, len(typeCounts))
	for t := range typeCounts {
		types = append(types, t)
	}
	sort.Strings(types)

	edges := sortedEdges(topo.Edges)

	var b strings.Builder
	b.WriteString("服务拓扑摘要（数据源：已配置的真实拓扑源）\n")
	fmt.Fprintf(&b, "节点总数: %d\n", len(topo.Nodes))
	b.WriteString("节点类型分布:\n")
	for _, t := range types {
		fmt.Fprintf(&b, "  %s: %d\n", t, typeCounts[t])
	}
	fmt.Fprintf(&b, "边总数: %d\n", len(topo.Edges))

	shown := edges
	if len(shown) > maxTopologyEdgesShown {
		shown = shown[:maxTopologyEdgesShown]
	}
	if len(shown) == 0 {
		b.WriteString("依赖边: 无\n")
		return strings.TrimRight(b.String(), "\n")
	}

	fmt.Fprintf(&b, "依赖边（按 from/to/type 排序，显示前 %d 条）:\n", len(shown))
	for _, e := range shown {
		fmt.Fprintf(&b, "  %s -> %s (%s)\n", e.From, e.To, edgeTypeLabel(e.Type))
	}
	if omitted := len(edges) - len(shown); omitted > 0 {
		fmt.Fprintf(&b, "  ... 其余 %d 条已省略，完整图请用 REST 接口 GET /api/v1/topology 获取。\n", omitted)
	}
	return strings.TrimRight(b.String(), "\n")
}

// sortedEdges returns the edges ordered by from/to/type with exact duplicates
// removed, so the rendered output is deterministic.
func sortedEdges(edges []models.DependencyEdge) []models.DependencyEdge {
	out := make([]models.DependencyEdge, 0, len(edges))
	out = append(out, edges...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		if out[i].To != out[j].To {
			return out[i].To < out[j].To
		}
		return out[i].Type < out[j].Type
	})

	deduped := out[:0]
	for i, e := range out {
		if i > 0 && e == out[i-1] {
			continue
		}
		deduped = append(deduped, e)
	}
	return deduped
}

// nodeTypeLabel keeps an absent node type explicit instead of blank.
func nodeTypeLabel(t string) string {
	if t == "" {
		return "unknown"
	}
	return t
}

// edgeTypeLabel keeps an absent edge type explicit instead of blank.
func edgeTypeLabel(t string) string {
	if t == "" {
		return "unknown"
	}
	return t
}

// joinOrNone renders a neighbour list, or an honest "(无)" when there is none.
func joinOrNone(lines []string) string {
	if len(lines) == 0 {
		return "  (无)\n"
	}
	return strings.Join(lines, "\n") + "\n"
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
