// Package agent provides the MonitorAgent for APM and RCA analysis.
package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/observability"
	"github.com/Havens-blog/OpsAgent/internal/rca/engine"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
	"github.com/Havens-blog/OpsAgent/internal/tool"
)

// MonitorAgent specializes in APM data analysis and Root Cause Analysis (RCA).
// It integrates the RCA engine to analyze incidents and identify root causes.
type MonitorAgent struct {
	*BaseAgent
	rcaEngine *engine.Engine
	sources   *RCASources
}

// NewMonitorAgent creates a new MonitorAgent.
func NewMonitorAgent(settings *config.Settings, rcaSources *RCASources) (*MonitorAgent, error) {
	cfg := &AgentConfig{
		Name:        "monitor-agent",
		Type:        AgentTypeMonitor,
		Description: "Monitor and analyze APM data, perform Root Cause Analysis",
		Model:       "claude-sonnet-4-20250514",
		MaxTurns:    10,
		Timeout:     5 * time.Minute,
		SystemPrompt: `You are a MonitorAgent specialized in observability and root cause analysis.

Your capabilities include:
- Analyzing APM data (traces, metrics, logs)
- Performing Root Cause Analysis (RCA) on incidents
- Identifying service dependencies and anomalies
- Querying time-series metrics and error logs
- Correlating signals across multiple data sources

When analyzing an incident:
1. Gather signals from all available sources
2. Apply multi-dimensional scoring (Time Correlation, Topology, Changes, etc.)
3. Generate ranked root cause hypotheses with confidence scores
4. Provide actionable recommendations`,
		Capabilities: []string{"rca", "apm", "metrics", "logs", "traces", "topology"},
	}

	base, err := NewBaseAgent(cfg, settings)
	if err != nil {
		return nil, err
	}

	agent := &MonitorAgent{
		BaseAgent: base,
	}

	// Initialize RCA engine with provided sources
	if rcaSources != nil {
		if err := agent.InitializeRCA(rcaSources); err != nil {
			return nil, fmt.Errorf("failed to initialize RCA: %w", err)
		}
	}

	return agent, nil
}

// RCASources contains the data sources for RCA analysis.
type RCASources struct {
	LogSource       sources.LogSource
	MetricSource    sources.MetricSource
	TraceSource     sources.TraceSource
	TopologySource  sources.TopologySource
	ChangeSource    sources.ChangeSource
	WAFSource       sources.WAFSource
	BackendResolver sources.DomainBackendResolver
	SLBEdgeResolver sources.SLBEdgeResolver
	CDNEdgeResolver sources.CDNEdgeResolver
}

// InitializeRCA initializes the RCA engine with data sources.
func (m *MonitorAgent) InitializeRCA(sources *RCASources) error {
	opts := []engine.Option{
		engine.WithLogSource(sources.LogSource),
		engine.WithMetricSource(sources.MetricSource),
	}

	if sources.TraceSource != nil {
		opts = append(opts, engine.WithTraceSource(sources.TraceSource))
	}
	if sources.TopologySource != nil {
		opts = append(opts, engine.WithTopologySource(sources.TopologySource))
	}
	if sources.ChangeSource != nil {
		opts = append(opts, engine.WithChangeSource(sources.ChangeSource))
	}
	if sources.WAFSource != nil {
		opts = append(opts, engine.WithWAFSource(sources.WAFSource))
	}
	if sources.BackendResolver != nil {
		opts = append(opts, engine.WithDomainBackendResolver(sources.BackendResolver))
	}
	if sources.SLBEdgeResolver != nil {
		opts = append(opts, engine.WithSLBEdgeResolver(sources.SLBEdgeResolver))
	}
	if sources.CDNEdgeResolver != nil {
		opts = append(opts, engine.WithCDNEdgeResolver(sources.CDNEdgeResolver))
	}

	rcaEngine, err := engine.NewEngine(opts...)
	if err != nil {
		return err
	}

	m.rcaEngine = rcaEngine
	m.sources = sources
	return nil
}

// AnalyzeIncident performs RCA on the given incident.
func (m *MonitorAgent) AnalyzeIncident(ctx context.Context, incident *models.Incident) (*models.RCAReport, error) {
	if m.rcaEngine == nil {
		return nil, fmt.Errorf("RCA engine not initialized")
	}

	if err := incident.Validate(); err != nil {
		return nil, fmt.Errorf("invalid incident: %w", err)
	}

	m.logger.Info("Starting RCA analysis",
		observability.String("incident_id", incident.ID),
		observability.String("severity", incident.Severity),
		observability.Any("affected_services", incident.AffectedServices),
	)

	report, err := m.rcaEngine.Analyze(ctx, incident)
	if err != nil {
		m.logger.Error("RCA analysis failed",
			observability.String("incident_id", incident.ID),
			observability.Err(err),
		)
		return nil, err
	}

	m.logger.Info("RCA analysis completed",
		observability.String("incident_id", incident.ID),
		observability.Float64("confidence", report.Confidence),
		observability.Int("candidates", len(report.Candidates)),
		observability.String("duration", report.Duration.String()),
	)

	return report, nil
}

// GetRCAStatus returns the current status of the RCA engine.
func (m *MonitorAgent) GetRCAStatus() map[string]interface{} {
	status := map[string]interface{}{
		"initialized": m.rcaEngine != nil,
	}

	if m.rcaEngine != nil {
		status["ready"] = true
		status["agent"] = m.GetConfig().Name
	}

	return status
}

// Execute overrides BaseAgent.Execute to handle RCA-specific tasks.
func (m *MonitorAgent) Execute(ctx context.Context, task *Task) (*ExecutionResult, error) {
	startTime := time.Now()

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && m.BaseAgent.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.BaseAgent.config.Timeout)
		defer cancel()
	}

	// Add user message
	m.BaseAgent.conversation.AddMessage(Message{
		Role:      "user",
		Content:   task.Description,
		Timestamp: time.Now(),
	})

	m.logger.Info("MonitorAgent execution started",
		observability.String("task_id", task.ID),
	)

	// Check if this is an RCA task
	if taskType, ok := task.Params["type"].(string); ok && taskType == "rca" {
		return m.executeRCATask(ctx, task)
	}

	// Default execution
	result, err := m.executeTask(ctx, task)
	duration := time.Since(startTime)
	result.Duration = duration

	m.logger.Info("MonitorAgent execution completed",
		observability.String("task_id", task.ID),
		observability.Bool("success", result.Success),
		observability.String("duration", duration.String()),
	)

	return result, err
}

// executeRCATask executes an RCA analysis task.
func (m *MonitorAgent) executeRCATask(ctx context.Context, task *Task) (*ExecutionResult, error) {
	// Extract incident from params; free-text delegations (e.g. from the
	// coordinator's LLM routing) may lack a structured incident — derive a
	// default one-hour window from the task itself instead of failing.
	incidentData, ok := task.Params["incident"].(map[string]interface{})
	if !ok {
		incidentData = map[string]interface{}{
			"id":          task.ID,
			"start_time":  time.Now().Add(-time.Hour).Format(time.RFC3339),
			"end_time":    time.Now().Format(time.RFC3339),
			"severity":    "P3",
			"description": task.Description,
		}
		if svc := getString(task.Params, "service"); svc != "" {
			incidentData["affected_services"] = []interface{}{svc}
		}
	}

	incident := &models.Incident{
		ID:               getString(incidentData, "id"),
		StartTime:        getTime(incidentData, "start_time"),
		EndTime:          getTime(incidentData, "end_time"),
		Severity:         getString(incidentData, "severity"),
		Description:      getString(incidentData, "description"),
		AffectedServices: getStringSlice(incidentData, "affected_services"),
	}

	report, err := m.AnalyzeIncident(ctx, incident)
	if err != nil {
		return &ExecutionResult{
			Success:  false,
			Error:    errText(err),
			Response: fmt.Sprintf("RCA analysis failed: %v", err),
		}, nil
	}

	// Format response
	response := m.formatRCAReport(report)

	return &ExecutionResult{
		Success:  true,
		Response: response,
		Data: map[string]interface{}{
			"incident_id":   report.Incident.ID,
			"confidence":    report.Confidence,
			"top_candidate": report.TopCandidate,
			"candidates":    report.Candidates,
			"signals":       report.Signals,
			"duration":      report.Duration.String(),
		},
		ToolsUsed: []string{"rca_engine"},
	}, nil
}

// formatRCAReport formats the RCA report for human consumption.
func (m *MonitorAgent) formatRCAReport(report *models.RCAReport) string {
	var output string

	output += fmt.Sprintf("## Root Cause Analysis Report\n\n")
	output += fmt.Sprintf("**Incident:** %s\n", report.Incident.ID)
	output += fmt.Sprintf("**Severity:** %s\n", report.Incident.Severity)
	if report.Note != "" {
		output += fmt.Sprintf("**结论:** %s\n\n", report.Note)
		output += fmt.Sprintf("**Confidence:** 0.0%%\n\n")
	} else {
		output += fmt.Sprintf("**Confidence:** %.1f%%\n\n", report.Confidence*100)
	}

	if report.TopCandidate != nil {
		if report.Note != "" {
			output += fmt.Sprintf("### 窗口内信号(仅供参考)\n\n")
		} else {
			output += fmt.Sprintf("### Top Root Cause Candidate\n\n")
		}
		output += fmt.Sprintf("**Type:** %s\n", report.TopCandidate.Hypothesis.Type)
		output += fmt.Sprintf("**Service:** %s\n", report.TopCandidate.Hypothesis.Service)
		output += fmt.Sprintf("**Description:** %s\n", report.TopCandidate.Hypothesis.Description)
		output += fmt.Sprintf("**Score:** %.2f\n", report.TopCandidate.Score)
		if len(report.TopCandidate.Hypothesis.Evidence) > 0 {
			output += fmt.Sprintf("**Evidence:**\n")
			for _, ev := range report.TopCandidate.Hypothesis.Evidence {
				output += fmt.Sprintf("  - %s\n", ev)
			}
		}
		output += "\n"
	}

	if len(report.Candidates) > 1 {
		// Cap the list: a fleet-wide window can produce dozens of same-score
		// candidates, and an unbounded dump buries the ranked ones.
		const maxOther = 8
		others := report.Candidates[1:]
		output += fmt.Sprintf("### Other Candidates (%d)\n\n", len(others))
		for i, c := range others {
			if i >= maxOther {
				output += fmt.Sprintf("...(另有 %d 个更低分候选,见事件详情页)\n", len(others)-maxOther)
				break
			}
			output += fmt.Sprintf("%d. %s - Score: %.2f\n", i+1, c.Hypothesis.Description, c.Score)
		}
		output += "\n"
	}

	// Only when the facts did NOT become the root cause: the top candidate's
	// evidence already lists these exact lines, and repeating them is the
	// "wall of text" on-call complained about.
	if report.Signals != nil && report.Signals.WAFFacts != nil && report.Signals.WAFFacts.Total > 0 &&
		(report.TopCandidate == nil || report.TopCandidate.Hypothesis.Type != models.HypothesisTypeWAFAttack) {
		output += "\n### WAF 攻击事实\n\n"
		for _, line := range report.Signals.WAFFacts.EvidenceLines() {
			output += "- " + line + "\n"
		}
		output += "\n"
	}

	// Edge (CDN/SLB) reference facts for a domain alarm. These never became a
	// root-cause candidate — they answer "did the SLB->backend hop forward 5xx
	// in the window, and does the CDN hop even have logs" for a dial-test alarm
	// whose backend looked healthy, so on-call can see the full edge chain and
	// which hops actually have data instead of guessing from the Note.
	if report.EdgeFacts != nil && (report.EdgeFacts.CDN != nil || report.EdgeFacts.SLB != nil) {
		output += "\n### 边缘事实(仅供参考)\n\n"
		if report.EdgeFacts.CDN != nil {
			cdn := report.EdgeFacts.CDN
			if cdn.Provider != "" || cdn.CNAME != "" || cdn.OriginHost != "" {
				chain := cdn.Provider
				if cdn.CNAME != "" {
					chain += " → " + cdn.CNAME
				}
				if cdn.OriginHost != "" {
					chain += " → 回源 " + cdn.OriginHost
				}
				output += "- CDN: " + strings.TrimSpace(chain) + "\n"
			}
			if cdn.LogChecked {
				switch {
				case cdn.Total == 0:
					output += "- CDN 日志: 窗口内 0 条,该域名 CDN 日志未采集,边缘 5xx 数据不可得\n"
				case cdn.SampleSize > 0:
					output += fmt.Sprintf("- CDN 日志: 窗口内命中 %d 条(采样 %d 条),边缘 5xx %d 条\n", cdn.Total, cdn.SampleSize, cdn.Status5xx)
					if cdn.Status5xx > 0 {
						output += "- 解读: CDN 边缘在窗口内返回 5xx,不可用可能就在 CDN 这一跳\n"
					} else {
						output += "- 解读: CDN 边缘无 5xx,转发正常,不可用不在 CDN 这一跳\n"
					}
				default:
					output += fmt.Sprintf("- CDN 日志: 窗口内命中 %d 条(采样为空,无法统计 5xx)\n", cdn.Total)
				}
			}
		}
		if report.EdgeFacts.SLB != nil {
			slb := report.EdgeFacts.SLB
			if len(slb.Backends) > 0 {
				backends := make([]string, 0, len(slb.Backends))
				for _, b := range slb.Backends {
					if b.Service != "" && b.TargetIP != "" {
						backends = append(backends, fmt.Sprintf("%s(%s:%d)", b.Service, b.TargetIP, b.Port))
					} else if b.Service != "" {
						backends = append(backends, b.Service)
					} else if b.TargetIP != "" {
						backends = append(backends, b.TargetIP)
					}
				}
				if len(backends) > 0 {
					output += "- SLB 后端: " + strings.Join(backends, " · ") + "\n"
				}
			}
			output += fmt.Sprintf("- SLB 窗口内命中 %d 条(采样 %d 条),5xx %d 条\n", slb.Total, slb.SampleSize, slb.Status5xx)
			switch {
			case slb.Status5xx > 0:
				output += "- 解读: SLB→后端在窗口内出现 5xx,不可用大概率在这一跳\n"
			case slb.Total > 0:
				output += "- 解读: SLB→后端转发正常(无 5xx),不可用不在 SLB 这一跳\n"
			default:
				output += "- 解读: 窗口内该域名无 SLB 记录,探测流量可能未到达 SLB\n"
			}
		}
		output += "\n"
	}

	output += fmt.Sprintf("**Analysis Duration:** %s\n", report.Duration.String())
	output += fmt.Sprintf("**Analysis Window:** %s to %s\n",
		report.AnalysisWindow.Start.Format(time.RFC3339),
		report.AnalysisWindow.End.Format(time.RFC3339))

	return output
}

// executeTask implements the default task execution.
func (m *MonitorAgent) executeTask(ctx context.Context, task *Task) (*ExecutionResult, error) {
	// Delegate to the base agent's real LLM-backed execution (Go uses static
	// dispatch, so forward explicitly rather than relying on virtual dispatch).
	result, err := m.BaseAgent.executeTask(ctx, task)
	if result != nil {
		if result.Data == nil {
			result.Data = map[string]interface{}{}
		}
		result.Data["rca_available"] = m.rcaEngine != nil
		result.Data["capabilities"] = m.GetConfig().Capabilities
	}
	return result, err
}

// Helper functions for type extraction
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getTime(m map[string]interface{}, key string) time.Time {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t
			}
		}
	}
	return time.Now()
}

func getStringSlice(m map[string]interface{}, key string) []string {
	if v, ok := m[key]; ok {
		if slice, ok := v.([]interface{}); ok {
			result := make([]string, 0, len(slice))
			for _, item := range slice {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
	}
	return []string{}
}

// BoundTools returns the monitor's tools bound to its REAL RCA engine and data
// sources. (The old package-level CreateMonitorTools returned stubs telling the
// LLM to "use the method directly" — a dead end a model cannot call.)
func (m *MonitorAgent) BoundTools() []*tool.Tool {
	return []*tool.Tool{
		m.analyzeIncidentTool(),
		m.queryMetricsTool(),
		m.queryLogsTool(),
		m.queryTracesTool(),
		m.getTopologyTool(),
	}
}

// timeRange extracts start/end from tool params, defaulting to the last hour.
func timeRange(params map[string]interface{}) (time.Time, time.Time) {
	end := time.Now()
	start := end.Add(-time.Hour)
	if v, ok := params["start_time"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			start = t
		}
	}
	if v, ok := params["end_time"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			end = t
		}
	}
	return start, end
}

// analyzeIncidentTool runs the real RCA engine over a structured incident.
func (m *MonitorAgent) analyzeIncidentTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"incident_id": {"type": "string", "description": "Unique identifier for the incident"},
			"start_time": {"type": "string", "description": "Incident start time in RFC3339 format (default 1h ago)"},
			"end_time": {"type": "string", "description": "Incident end time in RFC3339 format (default now)"},
			"severity": {"type": "string", "description": "Incident severity (P0-P4)", "enum": ["P0", "P1", "P2", "P3", "P4"]},
			"affected_services": {"type": "array", "items": {"type": "string"}, "description": "List of affected service names"},
			"description": {"type": "string", "description": "Incident description"}
		}
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "analyze_incident",
			Description: "Perform Root Cause Analysis (RCA) on a production incident using multi-dimensional scoring over real logs/metrics/topology/changes",
			Source:      tool.DataSourceTypeGeneric,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectNone,
			Category:    "rca",
			UseCases: []string{
				"Analyze service degradation",
				"Investigate error rate spikes",
				"Identify root cause of latency increases",
			},
		},
		IsAvailable: func(map[string]interface{}) bool { return m.rcaEngine != nil },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			if m.rcaEngine == nil {
				return nil, fmt.Errorf("RCA engine not initialized")
			}
			id := getString(params, "incident_id")
			if id == "" {
				id = fmt.Sprintf("tool-%d", time.Now().UnixNano())
			}
			start, end := timeRange(params)
			severity := getString(params, "severity")
			if severity == "" {
				severity = "P3"
			}
			incident := &models.Incident{
				ID:               id,
				DetectedAt:       time.Now(),
				StartTime:        start,
				EndTime:          end,
				Severity:         severity,
				Description:      getString(params, "description"),
				AffectedServices: getStringSlice(params, "affected_services"),
			}
			report, err := m.AnalyzeIncident(ctx, incident)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"incident_id": report.Incident.ID,
				"confidence":  report.Confidence,
				"summary":     report.GetSummary(),
				"report":      m.formatRCAReport(report),
			}, nil
		},
	}
}

// queryMetricsTool queries the real metric source for anomalous series.
func (m *MonitorAgent) queryMetricsTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"start_time": {"type": "string", "description": "Query start time in RFC3339 format (default 1h ago)"},
			"end_time": {"type": "string", "description": "Query end time in RFC3339 format (default now)"}
		}
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "query_metrics",
			Description: "Query anomalous time-series metrics (Prometheus) in the window",
			Source:      tool.DataSourceTypePrometheus,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "metrics",
		},
		IsAvailable: func(map[string]interface{}) bool { return m.sources != nil && m.sources.MetricSource != nil },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			if m.sources == nil || m.sources.MetricSource == nil {
				return nil, fmt.Errorf("metric source not configured")
			}
			start, end := timeRange(params)
			series, err := m.sources.MetricSource.QueryAnomalies(ctx, start, end)
			if err != nil {
				return nil, err
			}
			out := make([]map[string]interface{}, 0, len(series))
			for _, sr := range series {
				item := map[string]interface{}{
					"name":   sr.Name,
					"labels": sr.Labels,
					"points": len(sr.DataPoints),
				}
				if n := len(sr.DataPoints); n > 0 {
					item["latest_value"] = sr.DataPoints[n-1].Value
					item["latest_time"] = sr.DataPoints[n-1].Timestamp.Format(time.RFC3339)
				}
				out = append(out, item)
			}
			return map[string]interface{}{"count": len(out), "anomaly_series": out}, nil
		},
	}
}

// queryLogsTool queries the real log source; a service name scopes the query
// to that service's store (fast path), otherwise it scans error logs globally.
func (m *MonitorAgent) queryLogsTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"service": {"type": "string", "description": "Service name to scope the query (recommended: much faster)"},
			"start_time": {"type": "string", "description": "Query start time in RFC3339 format (default 1h ago)"},
			"end_time": {"type": "string", "description": "Query end time in RFC3339 format (default now)"},
			"limit": {"type": "integer", "description": "Max sample entries to return", "default": 20}
		}
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "query_logs",
			Description: "Query error logs in the window, optionally scoped to one service",
			Source:      tool.DataSourceTypeSLS,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "logs",
		},
		IsAvailable: func(map[string]interface{}) bool { return m.sources != nil && m.sources.LogSource != nil },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			if m.sources == nil || m.sources.LogSource == nil {
				return nil, fmt.Errorf("log source not configured")
			}
			start, end := timeRange(params)
			var entries []models.LogEntry
			var err error
			if svc := getString(params, "service"); svc != "" {
				entries, err = m.sources.LogSource.QueryErrorsByService(ctx, svc, start, end)
			} else {
				entries, err = m.sources.LogSource.QueryErrors(ctx, start, end)
			}
			if err != nil {
				return nil, err
			}
			limit := 20
			if v, ok := params["limit"].(float64); ok && int(v) > 0 {
				limit = int(v)
			}
			sample := make([]map[string]interface{}, 0, limit)
			for i, e := range entries {
				if i >= limit {
					break
				}
				msg := e.Message
				if len(msg) > 200 {
					msg = msg[:200] + "…"
				}
				sample = append(sample, map[string]interface{}{
					"timestamp": e.Timestamp.Format(time.RFC3339),
					"service":   e.Service,
					"level":     e.Level,
					"message":   msg,
				})
			}
			return map[string]interface{}{"total": len(entries), "sample": sample}, nil
		},
	}
}

// queryTracesTool queries the real trace source. The trace dimension is
// currently backed by an empty source (tracing data lives in Prometheus in
// this deployment), so an empty result is the honest steady state.
func (m *MonitorAgent) queryTracesTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"start_time": {"type": "string", "description": "Query start time in RFC3339 format (default 1h ago)"},
			"end_time": {"type": "string", "description": "Query end time in RFC3339 format (default now)"}
		}
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "query_traces",
			Description: "Query error traces in the window (empty until a tracing backend is wired)",
			Source:      tool.DataSourceTypeARMS,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "traces",
		},
		IsAvailable: func(map[string]interface{}) bool { return m.sources != nil && m.sources.TraceSource != nil },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			if m.sources == nil || m.sources.TraceSource == nil {
				return nil, fmt.Errorf("trace source not configured")
			}
			start, end := timeRange(params)
			traces, err := m.sources.TraceSource.QueryErrorTraces(ctx, start, end)
			if err != nil {
				return nil, err
			}
			const maxTraces = 10
			if len(traces) > maxTraces {
				traces = traces[:maxTraces]
			}
			return map[string]interface{}{"count": len(traces), "traces": traces}, nil
		},
	}
}

// getTopologyTool queries the real topology source (ARMS APM when configured).
func (m *MonitorAgent) getTopologyTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {}
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "get_topology",
			Description: "Get the service dependency topology (nodes + edges)",
			Source:      tool.DataSourceTypeARMS,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "topology",
		},
		IsAvailable: func(map[string]interface{}) bool { return m.sources != nil && m.sources.TopologySource != nil },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			if m.sources == nil || m.sources.TopologySource == nil {
				return nil, fmt.Errorf("topology source not configured")
			}
			topo, err := m.sources.TopologySource.GetTopology(ctx)
			if err != nil {
				return nil, err
			}
			if topo == nil {
				return map[string]interface{}{"node_count": 0, "edge_count": 0}, nil
			}
			const maxItems = 30
			names := make([]string, 0, len(topo.Nodes))
			for i, n := range topo.Nodes {
				if i >= maxItems {
					break
				}
				names = append(names, n.Name)
			}
			edges := make([]string, 0, len(topo.Edges))
			for i, e := range topo.Edges {
				if i >= maxItems {
					break
				}
				edges = append(edges, e.From+"->"+e.To)
			}
			return map[string]interface{}{
				"node_count":   len(topo.Nodes),
				"edge_count":   len(topo.Edges),
				"services":     names,
				"dependencies": edges,
			}, nil
		},
	}
}
