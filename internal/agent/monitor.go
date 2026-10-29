// Package agent provides the MonitorAgent for APM and RCA analysis.
package agent

import (
	"context"
	"fmt"
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

	if err := base.Initialize(CreateMonitorTools()); err != nil {
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
	LogSource      sources.LogSource
	MetricSource   sources.MetricSource
	TraceSource    sources.TraceSource
	TopologySource sources.TopologySource
	ChangeSource   sources.ChangeSource
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

	rcaEngine, err := engine.NewEngine(opts...)
	if err != nil {
		return err
	}

	m.rcaEngine = rcaEngine
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
	// Extract incident from params
	incidentData, ok := task.Params["incident"].(map[string]interface{})
	if !ok {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("invalid incident data"),
		}, nil
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
			Error:    err,
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
	output += fmt.Sprintf("**Confidence:** %.1f%%\n\n", report.Confidence*100)

	if report.TopCandidate != nil {
		output += fmt.Sprintf("### Top Root Cause Candidate\n\n")
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
		output += fmt.Sprintf("### Other Candidates (%d)\n\n", len(report.Candidates)-1)
		for i, c := range report.Candidates[1:] {
			output += fmt.Sprintf("%d. %s - Score: %.2f\n", i+1, c.Hypothesis.Description, c.Score)
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

// CreateMonitorTools creates the standard set of tools for MonitorAgent.
func CreateMonitorTools() []*tool.Tool {
	return []*tool.Tool{
		createAnalyzeIncidentTool(),
		createQueryMetricsTool(),
		createQueryLogsTool(),
		createQueryTracesTool(),
		createGetTopologyTool(),
	}
}

// createAnalyzeIncidentTool creates the RCA analysis tool.
func createAnalyzeIncidentTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"incident_id": {"type": "string", "description": "Unique identifier for the incident"},
			"start_time": {"type": "string", "description": "Incident start time in RFC3339 format"},
			"end_time": {"type": "string", "description": "Incident end time in RFC3339 format"},
			"severity": {"type": "string", "description": "Incident severity (P0-P4)", "enum": ["P0", "P1", "P2", "P3", "P4"]},
			"affected_services": {"type": "array", "items": {"type": "string"}, "description": "List of affected service names"},
			"description": {"type": "string", "description": "Incident description"}
		},
		"required": ["incident_id", "start_time", "end_time", "severity"]
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "analyze_incident",
			Description: "Perform Root Cause Analysis (RCA) on a production incident using multi-dimensional scoring",
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
		IsAvailable: func(config map[string]interface{}) bool { return true },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"status": "use MonitorAgent.AnalyzeIncident method directly"}, nil
		},
	}
}

// createQueryMetricsTool creates the metrics query tool.
func createQueryMetricsTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Prometheus query expression"},
			"start_time": {"type": "string", "description": "Query start time in RFC3339 format"},
			"end_time": {"type": "string", "description": "Query end time in RFC3339 format"},
			"step": {"type": "string", "description": "Query resolution step (e.g., '1m', '5m')"}
		},
		"required": ["query", "start_time", "end_time"]
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "query_metrics",
			Description: "Query time-series metrics from Prometheus",
			Source:      tool.DataSourceTypePrometheus,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "metrics",
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"message": "metrics query - to be implemented with Prometheus integration", "query": params["query"]}, nil
		},
	}
}

// createQueryLogsTool creates the log query tool.
func createQueryLogsTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Log query expression"},
			"start_time": {"type": "string", "description": "Query start time in RFC3339 format"},
			"end_time": {"type": "string", "description": "Query end time in RFC3339 format"},
			"limit": {"type": "integer", "description": "Maximum number of log entries to return", "default": 100}
		},
		"required": ["query", "start_time", "end_time"]
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "query_logs",
			Description: "Query error logs from SLS for the specified time range",
			Source:      tool.DataSourceTypeSLS,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "logs",
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"message": "log query - to be implemented with SLS integration", "query": params["query"]}, nil
		},
	}
}

// createQueryTracesTool creates the trace query tool.
func createQueryTracesTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"service": {"type": "string", "description": "Service name to filter traces"},
			"start_time": {"type": "string", "description": "Query start time in RFC3339 format"},
			"end_time": {"type": "string", "description": "Query end time in RFC3339 format"},
			"has_error": {"type": "boolean", "description": "Filter only error traces", "default": true},
			"limit": {"type": "integer", "description": "Maximum number of traces to return", "default": 50}
		},
		"required": ["start_time", "end_time"]
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "query_traces",
			Description: "Query distributed traces from ARMS",
			Source:      tool.DataSourceTypeARMS,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "traces",
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"message": "trace query - to be implemented with ARMS integration", "service": params["service"]}, nil
		},
	}
}

// createGetTopologyTool creates the topology query tool.
func createGetTopologyTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"service": {"type": "string", "description": "Service name to get topology for (optional, returns full topology if omitted)"}
		}
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "get_topology",
			Description: "Get service dependency topology from ARMS",
			Source:      tool.DataSourceTypeARMS,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "topology",
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"message": "topology query - to be implemented with ARMS integration"}, nil
		},
	}
}
