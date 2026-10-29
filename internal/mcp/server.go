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

// rcaAnalyzer is the narrow contract the MCP server needs from the RCA engine.
// Declaring it here (rather than depending on internal/rca/engine) keeps the
// dependency one-way and makes the engine trivially fakeable in tests. The
// concrete *engine.Engine returned by engine.NewEngine satisfies it.
//
// Because the server holds this INTERFACE, a nil engine is detectable at the
// call site: the RCA tool then reports "engine not wired" instead of inventing
// an analysis.
type rcaAnalyzer interface {
	Analyze(ctx context.Context, incident *models.Incident) (*models.RCAReport, error)
}

// Server represents the MCP server.
type Server struct {
	registry   *registry.Registry
	rcaEngine  rcaAnalyzer
	httpServer *http.Server

	// topologySource is the same progressive-enhancement source the REST API
	// (/api/v1/topology) uses. topologyReal records whether it is backed by a
	// genuinely configured backend (ARMS APM) rather than the honest empty
	// fallback: an unconfigured dimension must be reported as such, never
	// rendered as a fabricated graph.
	topologySource sources.TopologySource
	topologyReal   bool

	// metricSource/logSource are the SAME sources the RCA engine and the REST
	// API are built from (agent.CreateRCASourcesWithFallback), so the MCP tools
	// answer from exactly the data the rest of the system reasons over.
	// metricsReal/logsReal mirror active["metrics"]/active["logs"]: when false
	// the dimension is backed by the honest empty source, and the tools MUST
	// say so rather than query it or fabricate a result.
	metricSource sources.MetricSource
	metricsReal  bool

	logSource sources.LogSource
	logsReal  bool

	// changeSource is the SAME source /api/v1/changes serves from (built by
	// agent.CreateRCASourcesWithFallback). changesReal mirrors
	// active["changes"]: when false the dimension is backed by the honest empty
	// source, and the tool MUST report that instead of querying it or counting
	// anything — an unconfigured dimension has no records to count.
	changeSource sources.ChangeSource
	changesReal  bool
}

// NewServer creates a new MCP server.
func NewServer(reg *registry.Registry) *Server {
	return &Server{
		registry: reg,
	}
}

// SetRCAEngine injects the RCA engine used by the opsagent_rca_analyze tool.
// The parameter is the narrow Analyze-only interface, so any real engine
// (*engine.Engine) can be passed unchanged and a nil argument keeps the
// "engine not wired" behaviour. Calling it is optional: without an engine the
// tool reports that honestly and points at the REST endpoint instead.
func (s *Server) SetRCAEngine(engine rcaAnalyzer) {
	s.rcaEngine = engine
}

// SetMetricSource injects the metric source and whether it is backed by a real
// backend. Callers pass rcaSources.MetricSource together with
// active["metrics"] from agent.CreateRCASourcesWithFallback, so this tool
// answers from exactly the same metrics the RCA engine reads.
func (s *Server) SetMetricSource(src sources.MetricSource, real bool) {
	s.metricSource = src
	s.metricsReal = real
}

// SetLogSource injects the log source and whether it is backed by a real
// backend (rcaSources.LogSource + active["logs"]).
func (s *Server) SetLogSource(src sources.LogSource, real bool) {
	s.logSource = src
	s.logsReal = real
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

// SetChangeSource injects the change source and whether it is backed by a real
// backend. Callers pass rcaSources.ChangeSource together with
// active["changes"] from agent.CreateRCASourcesWithFallback, so this tool lists
// exactly the change records REST GET /api/v1/changes lists.
func (s *Server) SetChangeSource(src sources.ChangeSource, real bool) {
	s.changeSource = src
	s.changesReal = real
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

// rcaEngineMissingText is the honest answer when no RCA engine is wired into
// the MCP server. It names the REST endpoint that does hold a real engine
// instead of pretending an analysis was started.
const rcaEngineMissingText = "RCA 引擎未接入 MCP，无法执行根因分析。\n" +
	"本工具不会返回任何分析结果、候选根因或占位文本。\n" +
	"请改用 REST 接口 POST /api/v1/rca/analyze（API 服务持有真实 RCA 引擎）。"

// handleRCAAnalyze handles RCA analysis requests. It builds a models.Incident
// from the tool arguments, runs the injected engine and renders the REAL
// report it returns. Every default the MCP layer filled in (time window,
// detected-at, source, empty fields) is echoed in the output so the caller can
// tell which values were requested and which were assumed.
func (s *Server) handleRCAAnalyze(ctx context.Context, args map[string]interface{}) (*ToolResponse, error) {
	engine := s.rcaEngine
	if engine == nil {
		return errorResponse(rcaEngineMissingText), nil
	}

	incidentID := strings.TrimSpace(stringArg(args, "incident_id"))
	if incidentID == "" {
		return errorResponse("RCA 分析失败：缺少 incident_id 参数，未执行任何分析。\n请提供 incident_id（以及 start_time/end_time）后重试。"), nil
	}

	startTime, endTime, notes := parseWindow(args, defaultLookback)
	if msg, bad := windowProblem(startTime, endTime); bad {
		return errorResponse("RCA 分析失败：" + msg + "，未执行任何分析。"), nil
	}

	services := stringSliceArg(args, "affected_services")
	if len(services) == 0 {
		notes = append(notes, "affected_services 未提供（或为空数组），Incident.AffectedServices 留空：引擎只按时间窗与已配置数据源分析，不会推断受影响服务。")
	}

	detectedAt := time.Now()
	notes = append(notes,
		fmt.Sprintf("DetectedAt 由 MCP 服务在本次调用时生成：%s", detectedAt.Format(time.RFC3339)),
		"Severity 留空（本工具 schema 无 severity 参数），Source 置为 \"mcp\"，Labels={\"status\": \"running\"}。",
	)

	incident := &models.Incident{
		ID:               incidentID,
		DetectedAt:       detectedAt,
		StartTime:        startTime,
		EndTime:          endTime,
		AffectedServices: services,
		Source:           "mcp",
		Labels:           map[string]string{"status": "running"},
	}

	report, err := engine.Analyze(ctx, incident)
	if err != nil {
		return errorResponse(fmt.Sprintf(
			"RCA 分析失败，未产出任何报告。\nIncident ID: %s\n时间窗: %s ~ %s\n原因: %v",
			incidentID, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), err)), nil
	}
	if report == nil {
		return errorResponse(fmt.Sprintf(
			"RCA 分析未产出报告：引擎返回了空结果（nil report，err=nil），因此没有任何可展示的根因、置信度或证据。\nIncident ID: %s\n时间窗: %s ~ %s",
			incidentID, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))), nil
	}

	return textResponse(formatRCAReport(incident, report, notes)), nil
}

// logsUnconfiguredText is the honest answer when no real log backend is
// configured. It names the variables that would enable one and reports no
// entries at all.
const logsUnconfiguredText = "日志源未配置，无法返回真实日志数据。\n" +
	"本工具只在配置了真实日志源（SLS_ENDPOINT/SLS_PROJECT/SLS_LOGSTORE 或 TLS_ENDPOINT/TLS_TOPIC_ID，或 ECAM_LOGQUERY_ENDPOINT）时返回数据，未配置期间不会编造任何日志条目，也不会报告任何条目数量。\n" +
	"配置上述环境变量后重启 MCP 服务重试。"

// handleLogSearch handles log search requests. The RCA LogSource interface only
// offers intent-scoped queries (by service, errors-only, count) — there is NO
// free-text query method — so the tool queries by service and, when a `query`
// string is supplied, filters the RETURNED entries locally and says so.
func (s *Server) handleLogSearch(ctx context.Context, args map[string]interface{}) (*ToolResponse, error) {
	query := strings.TrimSpace(stringArg(args, "query"))
	service := strings.TrimSpace(stringArg(args, "service"))
	limit := intArg(args, "limit", defaultLogLimit)
	if limit <= 0 {
		limit = defaultLogLimit
	}

	src := s.logSource
	if src == nil || !s.logsReal {
		return textResponse(logsUnconfiguredText), nil
	}

	if service == "" {
		return errorResponse("日志查询失败：缺少 service 参数，未查询任何日志源。\n原因：RCA 日志源接口只提供按服务查询（QueryErrors / QueryLogsByService / GetLogCount），不支持自由文本查询，所以没有 service 就无法定位真实日志。\n请带上 service 参数后重试。"), nil
	}

	startTime, endTime, notes := parseWindow(args, defaultLookback)
	if msg, bad := windowProblem(startTime, endTime); bad {
		return errorResponse("日志查询失败：" + msg + "，未查询任何日志源。"), nil
	}

	entries, err := src.QueryLogsByService(ctx, service, startTime, endTime)
	if err != nil {
		return errorResponse(fmt.Sprintf(
			"日志查询失败，未返回任何日志条目。\nService: %s\n时间窗: %s ~ %s\n原因: %v",
			service, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), err)), nil
	}

	matched := entries
	if query != "" {
		matched = filterLogEntriesBySubstring(entries, query)
	}

	sorted := sortedLogEntries(matched)
	shown := sorted
	if len(shown) > limit {
		shown = shown[:limit]
	}
	if len(shown) > maxLogEntriesShown {
		shown = shown[:maxLogEntriesShown]
	}

	var b strings.Builder
	b.WriteString("日志查询结果（数据源：已配置的真实日志源）\n")
	fmt.Fprintf(&b, "Service: %s\n", service)
	fmt.Fprintf(&b, "时间窗: %s ~ %s\n", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	writeParamNotes(&b, notes)
	if query == "" {
		b.WriteString("本地过滤: 未提供 query，返回该服务在时间窗内的全部日志条目（未做任何过滤）。\n")
	} else {
		fmt.Fprintf(&b, "本地过滤 query: %s\n", query)
		b.WriteString("说明：query 为本地过滤，日志源不支持自由文本查询——真实查询是按 service + 时间窗发起的，query 只在这批已返回的条目上做大小写不敏感的子串匹配（匹配范围：消息正文、级别、标签值）。\n")
	}
	fmt.Fprintf(&b, "日志源返回条目数: %d\n", len(entries))
	if query == "" {
		fmt.Fprintf(&b, "参与展示条目数: %d\n", len(matched))
	} else {
		fmt.Fprintf(&b, "本地过滤后条目数: %d\n", len(matched))
	}

	if len(sorted) == 0 {
		if query == "" {
			b.WriteString("未显示任何条目：日志源在该时间窗内返回了 0 条日志（真实返回结果，不是省略显示）。\n")
		} else {
			b.WriteString("未显示任何条目：日志源返回的条目中没有一条在本地匹配该 query（真实过滤结果，不是查询失败）。\n")
		}
		return textResponse(strings.TrimRight(b.String(), "\n")), nil
	}

	fmt.Fprintf(&b, "展示前 %d 条（按 时间/服务/级别/正文 排序）:\n", len(shown))
	for i, e := range shown {
		fmt.Fprintf(&b, "  [%d] %s\n", i+1, formatLogEntryLine(e))
	}
	if omitted := len(sorted) - len(shown); omitted > 0 {
		fmt.Fprintf(&b, "  ... 其余 %d 条已省略（展示上限 = min(limit=%d, 常量 %d)）。\n", omitted, limit, maxLogEntriesShown)
	}
	return textResponse(strings.TrimRight(b.String(), "\n")), nil
}

// metricsUnconfiguredText is the honest answer when no real metric backend is
// configured. It reports no series and no query outcome at all.
const metricsUnconfiguredText = "指标源未配置，无法返回真实指标数据。\n" +
	"本工具只在配置了真实指标源（PROMETHEUS_ENDPOINT + ARMS_PROMETHEUS_BEARER_TOKEN，或 ARMS APM 指标源）时返回数据，未配置期间不会编造任何指标序列或查询结果。\n" +
	"配置上述环境变量后重启 MCP 服务重试。"

// handleMetricsQuery handles metrics query requests by executing the caller's
// PromQL against the injected real metric source and rendering the series that
// came back. Nothing is reported that the source did not return.
func (s *Server) handleMetricsQuery(ctx context.Context, args map[string]interface{}) (*ToolResponse, error) {
	query := strings.TrimSpace(stringArg(args, "query"))
	step := strings.TrimSpace(stringArg(args, "step"))
	if step == "" {
		step = "1m"
	}

	src := s.metricSource
	if src == nil || !s.metricsReal {
		return textResponse(metricsUnconfiguredText), nil
	}

	if query == "" {
		return errorResponse("指标查询失败：缺少 query 参数（PromQL 表达式），未查询任何指标源。\n本工具不会在缺少查询条件时返回任何结果。"), nil
	}

	startTime, endTime, notes := parseWindow(args, defaultLookback)
	if msg, bad := windowProblem(startTime, endTime); bad {
		return errorResponse("指标查询失败：" + msg + "，未查询任何指标源。"), nil
	}

	// The metrics tool wants EVERY series the PromQL matches, and it wants the
	// caller's step to actually reach the backend. QueryMetric returns only the
	// first series and has no step parameter, so use the optional
	// MetricRangeSource extension when the injected source supports it.
	rangeSrc, ok := src.(sources.MetricRangeSource)
	if !ok {
		return errorResponse("指标查询失败：已配置的指标源不支持范围查询（未实现 MetricRangeSource），无法返回多序列。\n请检查注入的 MetricSource 是否为真实实现（DSMetricSource / PrometheusMetricSource）。"), nil
	}

	series, err := rangeSrc.QueryMetricRange(ctx, query, startTime, endTime, step)
	if err != nil {
		return errorResponse(fmt.Sprintf(
			"指标查询失败，未返回任何指标数据。\nPromQL: %s\n时间窗: %s ~ %s\nstep: %s\n原因: %v\n说明：真实指标源在“该 PromQL 在时间窗内无匹配序列”时同样返回 error，因此这个错误也可能是查询结果为空。",
			query, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), step, err)), nil
	}

	return textResponse(formatMetricsResult(query, step, startTime, endTime, notes, series)), nil
}

// defaultLookback is the window applied when a tool call omits or misformats
// start_time/end_time. The fallback is always reported in the output.
const defaultLookback = time.Hour

// maxLogEntriesShown caps the number of log lines rendered per call, so one
// noisy service cannot dump thousands of entries into the tool result. The
// caller can lower it with `limit` but never raise it above this constant.
const maxLogEntriesShown = 50

// defaultLogLimit is the display cap applied when `limit` is absent.
const defaultLogLimit = 100

// maxLogMessageChars truncates a single (possibly huge) log message.
const maxLogMessageChars = 300

// maxMetricSeriesShown caps how many metric series the metrics tool renders,
// so a high-cardinality query cannot dump thousands of series into the result.
const maxMetricSeriesShown = 20

// maxRCACandidatesShown caps how many scored hypotheses the RCA tool renders.
const maxRCACandidatesShown = 10

// maxRCAEvidenceShown caps the evidence lines rendered per hypothesis.
const maxRCAEvidenceShown = 5

// errorResponse wraps an honest failure message with IsError set, so MCP
// clients can tell a failed call from a successful empty one.
func errorResponse(text string) *ToolResponse {
	return &ToolResponse{
		Content: []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": text,
			},
		},
		IsError: true,
	}
}

// stringArg reads a string tool argument; a missing key or non-string value
// yields "" rather than a fabricated default.
func stringArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

// intArg reads an integer tool argument, returning def when absent. JSON
// numbers arrive as float64; other numeric shapes are tolerated defensively.
func intArg(args map[string]interface{}, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	}
	return def
}

// stringSliceArg reads an array-of-strings tool argument, dropping empty and
// non-string elements.
func stringSliceArg(args map[string]interface{}, key string) []string {
	if ss, ok := args[key].([]string); ok {
		out := make([]string, 0, len(ss))
		for _, s := range ss {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}

	raw, ok := args[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// parseWindow extracts start_time/end_time (RFC3339) from the tool arguments.
// An absent or unparseable value falls back to [now-lookback, now], and EVERY
// fallback is returned in notes so the output always states which window was
// really queried instead of silently substituting one.
func parseWindow(args map[string]interface{}, lookback time.Duration) (time.Time, time.Time, []string) {
	now := time.Now()
	startTime := now.Add(-lookback)
	endTime := now

	var notes []string

	rawStart := strings.TrimSpace(stringArg(args, "start_time"))
	switch {
	case rawStart == "":
		notes = append(notes, fmt.Sprintf("start_time 未提供，已默认使用 %s（本次调用时间前 %s）", startTime.Format(time.RFC3339), lookback))
	default:
		if t, err := time.Parse(time.RFC3339, rawStart); err != nil {
			notes = append(notes, fmt.Sprintf("start_time=%q 不是合法 RFC3339，已默认使用 %s", rawStart, startTime.Format(time.RFC3339)))
		} else {
			startTime = t
		}
	}

	rawEnd := strings.TrimSpace(stringArg(args, "end_time"))
	switch {
	case rawEnd == "":
		notes = append(notes, fmt.Sprintf("end_time 未提供，已默认使用 %s（本次调用时间）", endTime.Format(time.RFC3339)))
	default:
		if t, err := time.Parse(time.RFC3339, rawEnd); err != nil {
			notes = append(notes, fmt.Sprintf("end_time=%q 不是合法 RFC3339，已默认使用 %s", rawEnd, endTime.Format(time.RFC3339)))
		} else {
			endTime = t
		}
	}

	return startTime, endTime, notes
}

// windowProblem reports an inverted window, which is a caller error that must
// not be papered over with a silent default.
func windowProblem(startTime, endTime time.Time) (string, bool) {
	if endTime.Before(startTime) {
		return fmt.Sprintf("时间窗非法（end_time %s 早于 start_time %s）",
			endTime.Format(time.RFC3339), startTime.Format(time.RFC3339)), true
	}
	return "", false
}

// writeParamNotes renders the "which defaults were applied" block. It is only
// emitted when at least one default was actually applied.
func writeParamNotes(b *strings.Builder, notes []string) {
	if len(notes) == 0 {
		return
	}
	b.WriteString("请求参数说明（缺省值由 MCP 服务填充）:\n")
	for _, n := range notes {
		fmt.Fprintf(b, "  - %s\n", n)
	}
}

// formatMetricsResult renders every series the metric source actually returned.
// It never invents a series, a data point or a success claim.
func formatMetricsResult(query, step string, startTime, endTime time.Time, notes []string, series []models.MetricSeries) string {
	var b strings.Builder
	b.WriteString("指标查询结果（数据源：已配置的真实指标源）\n")
	fmt.Fprintf(&b, "PromQL: %s\n", query)
	fmt.Fprintf(&b, "时间窗: %s ~ %s\n", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	fmt.Fprintf(&b, "step 参数: %s\n", step)
	b.WriteString("说明：step 已作为底层范围查询的采样间隔真实下发（不再只是记录文案）。\n")
	writeParamNotes(&b, notes)

	sorted := sortedMetricSeries(series)
	fmt.Fprintf(&b, "指标源返回序列数: %d\n", len(series))

	if len(sorted) == 0 {
		b.WriteString("未显示任何序列：指标源返回了 0 条序列（真实返回结果，不是省略显示、也不是查询失败）。\n")
		return strings.TrimRight(b.String(), "\n")
	}

	shown := sorted
	if len(shown) > maxMetricSeriesShown {
		shown = shown[:maxMetricSeriesShown]
	}
	fmt.Fprintf(&b, "展示前 %d 条（按 name 确定性排序）:\n", len(shown))
	for i, s := range shown {
		fmt.Fprintf(&b, "  [%d] name=%s 标签=%s 点数=%d 最新值=%s\n",
			i+1, metricNameOrUnknown(s.Name), formatLabels(s.Labels), len(s.DataPoints), latestValue(s.DataPoints))
		if s.Unit != "" {
			fmt.Fprintf(&b, "      单位: %s\n", s.Unit)
		}
	}
	if omitted := len(sorted) - len(shown); omitted > 0 {
		fmt.Fprintf(&b, "  ... 其余 %d 条已省略（单次展示上限 = %d）。\n", omitted, maxMetricSeriesShown)
	}
	return strings.TrimRight(b.String(), "\n")
}

// sortedMetricSeries orders the returned series by name (then label set) so the
// rendered output is deterministic across repeated calls.
func sortedMetricSeries(series []models.MetricSeries) []models.MetricSeries {
	out := make([]models.MetricSeries, 0, len(series))
	out = append(out, series...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return formatLabels(out[i].Labels) < formatLabels(out[j].Labels)
	})
	return out
}

// metricNameOrUnknown keeps an unnamed series explicit instead of blank.
func metricNameOrUnknown(name string) string {
	if strings.TrimSpace(name) == "" {
		return "unknown_metric"
	}
	return name
}

// formatLabels renders a label set as {k=v, k2=v2}, deterministically sorted.
func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, labels[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// latestValue renders the most recent data point, or an explicit absence. Ties
// keep the earliest index so the result is deterministic.
func latestValue(points []models.DataPoint) string {
	if len(points) == 0 {
		return "无（该序列没有任何数据点）"
	}
	best := points[0]
	for _, p := range points[1:] {
		if p.Timestamp.After(best.Timestamp) {
			best = p
		}
	}
	if best.Timestamp.IsZero() {
		return fmt.Sprintf("%g（时间戳为零值，数据源未提供时间）", best.Value)
	}
	return fmt.Sprintf("%g @ %s", best.Value, best.Timestamp.Format(time.RFC3339))
}

// filterLogEntriesBySubstring applies the local, case-insensitive substring
// filter the log tool advertises: message body, level and label values.
func filterLogEntriesBySubstring(entries []models.LogEntry, query string) []models.LogEntry {
	needle := strings.ToLower(query)
	out := make([]models.LogEntry, 0, len(entries))
	for _, e := range entries {
		if logEntryMatches(e, needle) {
			out = append(out, e)
		}
	}
	return out
}

// logEntryMatches reports whether one entry contains the (lower-cased) needle.
func logEntryMatches(e models.LogEntry, needle string) bool {
	if strings.Contains(strings.ToLower(e.Message), needle) ||
		strings.Contains(strings.ToLower(e.Level), needle) {
		return true
	}
	for _, v := range e.Labels {
		if strings.Contains(strings.ToLower(v), needle) {
			return true
		}
	}
	return false
}

// sortedLogEntries returns the entries in a deterministic total order
// (timestamp, service, level, message, trace id) so repeated calls render the
// same text for the same input.
func sortedLogEntries(entries []models.LogEntry) []models.LogEntry {
	out := make([]models.LogEntry, 0, len(entries))
	out = append(out, entries...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Timestamp.Equal(b.Timestamp) {
			return a.Timestamp.Before(b.Timestamp)
		}
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		if a.Level != b.Level {
			return a.Level < b.Level
		}
		if a.Message != b.Message {
			return a.Message < b.Message
		}
		return a.TraceID < b.TraceID
	})
	return out
}

// formatLogEntryLine renders one REAL log entry as a single output line.
func formatLogEntryLine(e models.LogEntry) string {
	ts := "时间未知"
	if !e.Timestamp.IsZero() {
		ts = e.Timestamp.Format(time.RFC3339)
	}
	level := e.Level
	if strings.TrimSpace(level) == "" {
		level = "级别未知"
	}
	service := e.Service
	if strings.TrimSpace(service) == "" {
		service = "(源未提供)"
	}

	line := fmt.Sprintf("%s [%s] service=%s %s", ts, level, service, truncateMessage(e.Message))
	if e.IsError {
		line += " (IsError=true)"
	}
	if e.TraceID != "" {
		line += fmt.Sprintf(" trace=%s", e.TraceID)
	}
	if len(e.Labels) > 0 {
		line += fmt.Sprintf(" labels=%s", formatLabels(e.Labels))
	}
	return line
}

// truncateMessage keeps one entry on one line and bounds its length.
func truncateMessage(msg string) string {
	msg = strings.ReplaceAll(msg, "\r\n", " ")
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")
	if strings.TrimSpace(msg) == "" {
		return "(空消息)"
	}
	if len(msg) > maxLogMessageChars {
		return msg[:maxLogMessageChars] + "...(已截断)"
	}
	return msg
}

// formatRCAReport renders the REAL report returned by the injected engine:
// its own summary, confidence and scored hypotheses, deterministically sorted.
func formatRCAReport(incident *models.Incident, report *models.RCAReport, notes []string) string {
	var b strings.Builder
	b.WriteString("RCA 分析报告（引擎：MCP 已接入的真实 RCA 引擎）\n")
	fmt.Fprintf(&b, "Incident ID: %s\n", incident.ID)
	fmt.Fprintf(&b, "时间窗: %s ~ %s\n", incident.StartTime.Format(time.RFC3339), incident.EndTime.Format(time.RFC3339))
	if len(incident.AffectedServices) == 0 {
		b.WriteString("受影响服务: (未提供，引擎按空列表分析)\n")
	} else {
		fmt.Fprintf(&b, "受影响服务: %s（共 %d 个）\n", strings.Join(incident.AffectedServices, ", "), len(incident.AffectedServices))
	}
	writeParamNotes(&b, notes)

	b.WriteString("\n引擎结论: " + report.GetSummary() + "\n")
	fmt.Fprintf(&b, "置信度: %g\n", report.Confidence)
	if report.Incident == nil {
		b.WriteString("说明：引擎返回的报告未携带 incident 字段，报告元数据以本次请求的入参为准。\n")
	} else if report.Incident.ID != "" && report.Incident.ID != incident.ID {
		fmt.Fprintf(&b, "说明：引擎报告里的 incident ID 是 %s，与本次请求的 %s 不一致（这里如实展示引擎返回的值，不做替换）。\n",
			report.Incident.ID, incident.ID)
	}

	candidates := sortedCandidates(report.Candidates)
	fmt.Fprintf(&b, "候选根因数: %d\n", len(candidates))
	if len(candidates) == 0 {
		b.WriteString("说明：引擎未产出任何候选根因（不是省略显示）。这通常意味着已配置的真实数据源在该时间窗内没有提供足以支撑假设的信号；未配置的维度使用的是诚实空源，不会产生假设。\n")
	} else {
		shown := candidates
		if len(shown) > maxRCACandidatesShown {
			shown = shown[:maxRCACandidatesShown]
		}
		fmt.Fprintf(&b, "候选根因（按 得分/置信度/类型/服务 排序，显示前 %d 条）:\n", len(shown))
		for i, c := range shown {
			writeHypothesis(&b, i+1, c)
		}
		if omitted := len(candidates) - len(shown); omitted > 0 {
			fmt.Fprintf(&b, "  ... 其余 %d 条已省略（展示上限 %d）。\n", omitted, maxRCACandidatesShown)
		}
	}

	fmt.Fprintf(&b, "\n报告元数据: 分析窗口 %s ~ %s, 耗时 %s, 生成时间 %s",
		formatTimeOrUnknown(report.AnalysisWindow.Start),
		formatTimeOrUnknown(report.AnalysisWindow.End),
		report.Duration,
		formatTimeOrUnknown(report.GeneratedAt))
	return strings.TrimRight(b.String(), "\n")
}

// writeHypothesis renders one scored hypothesis with its real evidence.
func writeHypothesis(b *strings.Builder, index int, c models.ScoredHypothesis) {
	h := c.Hypothesis
	fmt.Fprintf(b, "  [%d] 类型=%s 服务=%s 置信度=%g 得分=%g 证据数=%d\n",
		index, hypothesisTypeLabel(string(h.Type)), serviceOrUnknown(h.Service), c.Confidence, c.Score, len(h.Evidence))
	if h.Description != "" {
		fmt.Fprintf(b, "       描述: %s\n", h.Description)
	}
	if h.Metric != "" {
		fmt.Fprintf(b, "       指标: %s\n", h.Metric)
	}
	if h.ChangeID != "" {
		fmt.Fprintf(b, "       变更 ID: %s\n", h.ChangeID)
	}
	// Only the score components both the main and current model define are
	// rendered; anything a branch adds on top would break compilation against
	// the other branch.
	fmt.Fprintf(b, "       评分构成: 时间相关性=%g 拓扑=%g 变更邻近=%g 指标严重度=%g 错误频率=%g 链路证据=%g\n",
		c.Breakdown.TimeCorrelation, c.Breakdown.TopologyScore, c.Breakdown.ChangeProximity,
		c.Breakdown.MetricSeverity, c.Breakdown.ErrorFrequency, c.Breakdown.TraceEvidence)

	if len(h.Evidence) == 0 {
		b.WriteString("       证据: (引擎未提供证据条目)\n")
		return
	}
	shown := h.Evidence
	if len(shown) > maxRCAEvidenceShown {
		shown = shown[:maxRCAEvidenceShown]
	}
	b.WriteString("       证据:\n")
	for _, ev := range shown {
		fmt.Fprintf(b, "         - %s\n", truncateMessage(ev))
	}
	if omitted := len(h.Evidence) - len(shown); omitted > 0 {
		fmt.Fprintf(b, "         ... 其余 %d 条证据已省略（展示上限 %d）。\n", omitted, maxRCAEvidenceShown)
	}
}

// sortedCandidates orders the report's hypotheses deterministically: score
// desc, confidence desc, then type/service/description.
func sortedCandidates(candidates []models.ScoredHypothesis) []models.ScoredHypothesis {
	out := make([]models.ScoredHypothesis, 0, len(candidates))
	out = append(out, candidates...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Confidence != b.Confidence {
			return a.Confidence > b.Confidence
		}
		if a.Hypothesis.Type != b.Hypothesis.Type {
			return a.Hypothesis.Type < b.Hypothesis.Type
		}
		if a.Hypothesis.Service != b.Hypothesis.Service {
			return a.Hypothesis.Service < b.Hypothesis.Service
		}
		return a.Hypothesis.Description < b.Hypothesis.Description
	})
	return out
}

// hypothesisTypeLabel keeps an absent hypothesis type explicit.
func hypothesisTypeLabel(t string) string {
	if strings.TrimSpace(t) == "" {
		return "unknown"
	}
	return t
}

// serviceOrUnknown keeps an absent service explicit.
func serviceOrUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(未指明)"
	}
	return s
}

// formatTimeOrUnknown formats a timestamp, stating plainly when it is unset.
func formatTimeOrUnknown(t time.Time) string {
	if t.IsZero() {
		return "未提供"
	}
	return t.Format(time.RFC3339)
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

// maxChangesShown caps how many real change records one call renders, so a busy
// cluster cannot dump its whole history into the tool result.
const maxChangesShown = 20

// changesUnconfiguredText is the honest answer when no real change source is
// configured (nil source, or the dimension fell back to the honest empty
// source). It reports ZERO records and no count of anything, because an
// unconfigured source was never queried and has no records to report.
const changesUnconfiguredText = "变更源未配置，无法返回真实变更记录。\n" +
	"本工具只在配置了真实变更源（K8S_ENDPOINT/K8S_TOKEN/K8S_NAMESPACE 或平台事件源）时返回数据，未配置期间不会查询任何源，也不会编造任何变更条目或条数。\n" +
	"配置上述环境变量后重启 MCP 服务重试；也可直接调用 REST 接口 GET /api/v1/changes 查看当前变更源返回的内容。"

// handleChangesList lists REAL change events from the configured change source
// (K8s deployment records / platform events) — the same source REST
// GET /api/v1/changes serves from.
//
// Hard contract, matching that REST handler: when no real source is wired
// (nil source or changesReal=false) the tool says so and NEVER queries the
// mock/empty fallback; a real query failure is returned as an error with the
// upstream text, never papered over with fabricated records or a fake count.
func (s *Server) handleChangesList(ctx context.Context, args map[string]interface{}) (*ToolResponse, error) {
	service := strings.TrimSpace(stringArg(args, "service"))

	src := s.changeSource
	if src == nil || !s.changesReal {
		return textResponse(changesUnconfiguredText), nil
	}

	startTime, endTime, notes, bad := changeWindow(args)
	if bad != "" {
		return errorResponse("变更查询失败：" + bad + "，未查询任何变更源。"), nil
	}
	if msg, inverted := windowProblem(startTime, endTime); inverted {
		return errorResponse("变更查询失败：" + msg + "，未查询任何变更源。"), nil
	}

	var (
		changes []models.Change
		err     error
	)
	if service != "" {
		changes, err = src.QueryChangesByService(ctx, service, startTime, endTime)
	} else {
		changes, err = src.QueryChanges(ctx, startTime, endTime)
	}
	if err != nil {
		return errorResponse(fmt.Sprintf(
			"变更查询失败，未返回任何变更记录。\n时间窗: %s ~ %s\n原因: %v\n请检查变更源（K8S_ENDPOINT/K8S_TOKEN/K8S_NAMESPACE 或平台事件源）的配置与后端连通性，或用 REST 接口 GET /api/v1/changes 复核。",
			startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), err)), nil
	}

	return textResponse(formatChangesResult(service, startTime, endTime, notes, changes)), nil
}

// changeWindow resolves the time window of the change tool. Unlike
// parseWindow it REFUSES a malformed timestamp instead of silently substituting
// the default: a rendered change list looks like an answer to the window the
// caller asked for, so quietly answering a different one would be a lie. An
// absent value still defaults to the shared lookback and is reported in notes.
func changeWindow(args map[string]interface{}) (time.Time, time.Time, []string, string) {
	now := time.Now()
	startTime := now.Add(-defaultLookback)
	endTime := now

	var notes []string

	rawStart := strings.TrimSpace(stringArg(args, "start_time"))
	switch {
	case rawStart == "":
		notes = append(notes, fmt.Sprintf("start_time 未提供，已默认使用 %s（本次调用时间前 %s）", startTime.Format(time.RFC3339), defaultLookback))
	default:
		if t, err := time.Parse(time.RFC3339, rawStart); err != nil {
			return time.Time{}, time.Time{}, nil, fmt.Sprintf("start_time=%q 不是合法 RFC3339", rawStart)
		} else {
			startTime = t
		}
	}

	rawEnd := strings.TrimSpace(stringArg(args, "end_time"))
	switch {
	case rawEnd == "":
		notes = append(notes, fmt.Sprintf("end_time 未提供，已默认使用 %s（本次调用时间）", endTime.Format(time.RFC3339)))
	default:
		if t, err := time.Parse(time.RFC3339, rawEnd); err != nil {
			return time.Time{}, time.Time{}, nil, fmt.Sprintf("end_time=%q 不是合法 RFC3339", rawEnd)
		} else {
			endTime = t
		}
	}

	return startTime, endTime, notes, ""
}

// formatChangesResult renders the records the change source actually returned.
// Every number in it is a real result count: the source's own return count, the
// count after exact-duplicate removal, and how many entries the display cap
// left out. It never invents a record, a count or a success claim.
func formatChangesResult(service string, startTime, endTime time.Time, notes []string, returned []models.Change) string {
	var b strings.Builder
	b.WriteString("变更查询结果（数据源：已配置的真实变更源）\n")
	fmt.Fprintf(&b, "时间窗: %s ~ %s\n", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	if service != "" {
		fmt.Fprintf(&b, "范围: 按服务过滤 service=%s（QueryChangesByService）\n", service)
	} else {
		b.WriteString("范围: 全部服务（QueryChanges，未按服务过滤）\n")
	}
	writeParamNotes(&b, notes)

	changes := sortedChanges(returned)
	fmt.Fprintf(&b, "变更源返回条目数: %d\n", len(returned))
	fmt.Fprintf(&b, "去重后条目数: %d\n", len(changes))

	if len(changes) == 0 {
		b.WriteString("未显示任何条目：变更源在该时间窗内返回了 0 条变更记录（真实返回结果，不是省略显示、也不是查询失败）。\n")
		return strings.TrimRight(b.String(), "\n")
	}

	shown := changes
	if len(shown) > maxChangesShown {
		shown = shown[:maxChangesShown]
	}
	fmt.Fprintf(&b, "展示前 %d 条（按时间倒序，格式：时间 | service | 类型 | 描述）:\n", len(shown))
	for i, c := range shown {
		fmt.Fprintf(&b, "  [%d] %s | %s | %s | %s\n",
			i+1, c.Timestamp.Format(time.RFC3339), changeFieldOrUnknown(c.Service),
			changeFieldOrUnknown(c.Type), changeFieldOrUnknown(c.Description))
	}
	if omitted := len(changes) - len(shown); omitted > 0 {
		fmt.Fprintf(&b, "  ... 其余 %d 条已省略（单次展示上限 = %d）。\n", omitted, maxChangesShown)
	}
	return strings.TrimRight(b.String(), "\n")
}

// sortedChanges orders the records newest-first and drops exact duplicates, so
// the rendered list and its counts are deterministic across calls. Records that
// compare equal on timestamp are ordered by their remaining fields, which also
// makes the duplicate removal stable.
func sortedChanges(changes []models.Change) []models.Change {
	out := make([]models.Change, 0, len(changes))
	out = append(out, changes...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Timestamp.After(out[j].Timestamp)
		}
		return changeIdentity(out[i]) < changeIdentity(out[j])
	})

	seen := make(map[string]struct{}, len(out))
	deduped := out[:0]
	for _, c := range out {
		key := changeIdentity(c)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, c)
	}
	return deduped
}

// changeIdentity is the full identity of a change record: two records are the
// same record only when every displayed field matches.
func changeIdentity(c models.Change) string {
	return strings.Join([]string{
		c.Timestamp.UTC().Format(time.RFC3339Nano),
		c.ID,
		c.Service,
		c.Type,
		c.Description,
	}, "\x1f")
}

// changeFieldOrUnknown keeps an absent service/type/description explicit
// instead of rendering an empty column.
func changeFieldOrUnknown(v string) string {
	if strings.TrimSpace(v) == "" {
		return "unknown"
	}
	return v
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
