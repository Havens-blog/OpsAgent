// Package agent provides the LogAnalystAgent for log analysis and pattern detection.
package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/observability"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
	"github.com/Havens-blog/OpsAgent/internal/tool"
)

// LogAnalystAgent specializes in log analysis and pattern detection.
// It queries logs from multiple sources and identifies error patterns.
type LogAnalystAgent struct {
	*BaseAgent
	logSource sources.LogSource
}

// NewLogAnalystAgent creates a new LogAnalystAgent.
func NewLogAnalystAgent(settings *config.Settings) (*LogAnalystAgent, error) {
	cfg := &AgentConfig{
		Name:         "log-analyst-agent",
		Type:         AgentTypeLogAnalyst,
		Description:  "Analyze logs and detect error patterns",
		Model:        "claude-sonnet-4-20250514",
		MaxTurns:     10,
		Timeout:      5 * time.Minute,
		SystemPrompt: `You are a LogAnalystAgent specialized in log analysis.

Your capabilities include:
- Querying error logs from multiple sources (SLS, Elasticsearch)
- Detecting error patterns and anomalies
- Correlating log events with incidents
- Identifying potential root causes from log patterns

When analyzing logs:
1. Focus on error and warning level messages
2. Identify recurring patterns
3. Correlate timestamps with incident timing
4. Extract relevant context (service, error codes, stack traces)`,
		Capabilities: []string{"log_analysis", "pattern_detection", "error_correlation", "log_querying"},
	}

	base, err := NewBaseAgent(cfg, settings)
	if err != nil {
		return nil, err
	}

	return &LogAnalystAgent{
		BaseAgent: base,
	}, nil
}

// SetLogSource sets the log source for analysis.
func (l *LogAnalystAgent) SetLogSource(source sources.LogSource) {
	l.logSource = source
}

// LogAnalysisResult represents the result of log analysis.
type LogAnalysisResult struct {
	Service       string             `json:"service"`
	StartTime     time.Time          `json:"start_time"`
	EndTime       time.Time          `json:"end_time"`
	TotalErrors   int64              `json:"total_errors"`
	ErrorPatterns []ErrorPattern     `json:"error_patterns"`
	Timeline      []LogTimeSlot      `json:"timeline"`
	Sources       []string           `json:"sources"`
}

// ErrorPattern represents a detected error pattern.
type ErrorPattern struct {
	Pattern    string   `json:"pattern"`
	Count      int      `json:"count"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	Services   []string `json:"services"`
	Example    string   `json:"example"`
	Severity   string   `json:"severity"`
}

// LogTimeSlot represents logs aggregated in a time slot.
type LogTimeSlot struct {
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	ErrorCount int       `json:"error_count"`
	LogCount   int       `json:"log_count"`
}

// IncidentCorrelation represents the correlation between logs and an incident.
type IncidentCorrelation struct {
	Service         string       `json:"service"`
	IncidentTime    time.Time    `json:"incident_time"`
	CorrelatedLogs  int          `json:"correlated_logs"`
	ErrorSpike      bool         `json:"error_spike"`
	RelatedErrors   []string     `json:"related_errors"`
	Confidence      float64      `json:"confidence"`
}

// AnalyzeLogs performs log analysis for the given service and time range.
func (l *LogAnalystAgent) AnalyzeLogs(ctx context.Context, service string, start, end time.Time) (*LogAnalysisResult, error) {
	if l.logSource == nil {
		return nil, fmt.Errorf("log source not configured")
	}

	l.logger.Info("Starting log analysis",
		observability.String("service", service),
		observability.String("start", start.Format(time.RFC3339)),
		observability.String("end", end.Format(time.RFC3339)),
	)

	// Query error logs
	logs, err := l.logSource.QueryErrors(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query error logs: %w", err)
	}

	// Filter by service if specified
	var filteredLogs []models.LogEntry
	if service != "" {
		for _, log := range logs {
			if log.Service == service {
				filteredLogs = append(filteredLogs, log)
			}
		}
	} else {
		filteredLogs = logs
	}

	// Analyze patterns
	patterns := l.detectPatterns(filteredLogs)

	// Create timeline slots
	slots := l.createTimeSlots(filteredLogs, start, end)

	// Get total error count
	totalCount := int64(len(filteredLogs))

	result := &LogAnalysisResult{
		Service:       service,
		StartTime:     start,
		EndTime:       end,
		TotalErrors:   totalCount,
		ErrorPatterns: patterns,
		Timeline:      slots,
		Sources:       []string{"sls"},
	}

	l.logger.Info("Log analysis completed",
		observability.String("service", service),
		observability.Int64("total_errors", totalCount),
		observability.Int("patterns", len(patterns)),
	)

	return result, nil
}

// detectPatterns identifies common error patterns from logs.
func (l *LogAnalystAgent) detectPatterns(logs []models.LogEntry) []ErrorPattern {
	if len(logs) == 0 {
		return []ErrorPattern{}
	}

	// Group by message similarity
	patternMap := make(map[string]*ErrorPattern)

	for _, log := range logs {
		// Normalize message for pattern detection
		pattern := l.normalizePattern(log.Message)

		if existing, ok := patternMap[pattern]; ok {
			existing.Count++
			if log.Timestamp.Before(existing.FirstSeen) {
				existing.FirstSeen = log.Timestamp
			}
			if log.Timestamp.After(existing.LastSeen) {
				existing.LastSeen = log.Timestamp
			}
			// Add service if not present
			serviceFound := false
			for _, s := range existing.Services {
				if s == log.Service {
					serviceFound = true
					break
				}
			}
			if !serviceFound {
				existing.Services = append(existing.Services, log.Service)
			}
		} else {
			patternMap[pattern] = &ErrorPattern{
				Pattern:   pattern,
				Count:     1,
				FirstSeen: log.Timestamp,
				LastSeen:  log.Timestamp,
				Services:  []string{log.Service},
				Example:   log.Message,
				Severity:  l.inferSeverity(log.Level),
			}
		}
	}

	// Convert to slice and filter by count
	result := make([]ErrorPattern, 0, len(patternMap))
	for _, pattern := range patternMap {
		// Only include patterns with 2+ occurrences
		if pattern.Count >= 2 {
			result = append(result, *pattern)
		}
	}

	return result
}

// normalizePattern normalizes a log message for pattern detection.
func (l *LogAnalystAgent) normalizePattern(message string) string {
	// Remove numbers, timestamps, and IDs for pattern matching
	pattern := message

	// Replace common variable patterns
	replacements := []struct {
		from string
		to   string
	}{
		{"\\d+\\.\\d+\\.\\d+\\.\\d+", "IP_ADDRESS"},
		{"\\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\\b", "UUID"},
		{"\\b\\d+\\b", "N"},
		{"\\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Z|a-z]{2,}\\b", "EMAIL"},
		{"'[^']*'", "'VALUE'"},
		{"\"[^\"]*\"", "\"VALUE\""},
	}

	for _, repl := range replacements {
		pattern = strings.ReplaceAll(pattern, repl.from, repl.to)
	}

	return pattern
}

// inferSeverity infers severity from log level.
func (l *LogAnalystAgent) inferSeverity(level string) string {
	switch strings.ToLower(level) {
	case "error", "err":
		return "high"
	case "warning", "warn":
		return "medium"
	case "info":
		return "low"
	default:
		return "unknown"
	}
}

// createTimeSlots creates time slots for timeline analysis.
func (l *LogAnalystAgent) createTimeSlots(logs []models.LogEntry, start, end time.Time) []LogTimeSlot {
	// Create 5-minute slots
	slotDuration := 5 * time.Minute
	slots := make([]LogTimeSlot, 0)

	for slotStart := start; slotStart.Before(end); slotStart = slotStart.Add(slotDuration) {
		slotEnd := slotStart.Add(slotDuration)
		if slotEnd.After(end) {
			slotEnd = end
		}

		count := 0
		for _, log := range logs {
			if (log.Timestamp.Equal(slotStart) || log.Timestamp.After(slotStart)) &&
				log.Timestamp.Before(slotEnd) {
				count++
			}
		}

		slots = append(slots, LogTimeSlot{
			StartTime:  slotStart,
			EndTime:    slotEnd,
			ErrorCount: count,
			LogCount:   count, // In real implementation, would include all log levels
		})
	}

	return slots
}

// CorrelateWithIncident correlates logs with an incident time.
func (l *LogAnalystAgent) CorrelateWithIncident(ctx context.Context, service string, incidentTime time.Time) (*IncidentCorrelation, error) {
	if l.logSource == nil {
		return nil, fmt.Errorf("log source not configured")
	}

	// Query logs around incident time (30 minutes before and after)
	start := incidentTime.Add(-30 * time.Minute)
	end := incidentTime.Add(30 * time.Minute)

	logs, err := l.logSource.QueryErrors(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query logs: %w", err)
	}

	// Filter by service
	var correlatedLogs []models.LogEntry
	relatedErrors := make(map[string]bool)

	for _, log := range logs {
		if log.Service == service {
			correlatedLogs = append(correlatedLogs, log)
			pattern := l.normalizePattern(log.Message)
			relatedErrors[pattern] = true
		}
	}

	// Detect error spike (5-minute window around incident)
	spikeStart := incidentTime.Add(-5 * time.Minute)
	spikeEnd := incidentTime.Add(5 * time.Minute)
	spikeCount := 0

	for _, log := range correlatedLogs {
		if (log.Timestamp.Equal(spikeStart) || log.Timestamp.After(spikeStart)) &&
			log.Timestamp.Before(spikeEnd) {
			spikeCount++
		}
	}

	errorSpike := spikeCount > 10 // Threshold for spike detection

	// Calculate confidence based on correlation strength
	confidence := 0.0
	if len(correlatedLogs) > 0 {
		confidence = 0.3
	}
	if errorSpike {
		confidence += 0.4
	}
	if len(relatedErrors) > 0 {
		confidence += 0.2
	}
	if confidence > 1.0 {
		confidence = 1.0
	}

	// Extract unique error patterns
	errorPatterns := make([]string, 0, len(relatedErrors))
	for pattern := range relatedErrors {
		errorPatterns = append(errorPatterns, pattern)
	}

	return &IncidentCorrelation{
		Service:        service,
		IncidentTime:   incidentTime,
		CorrelatedLogs: len(correlatedLogs),
		ErrorSpike:     errorSpike,
		RelatedErrors:  errorPatterns,
		Confidence:     confidence,
	}, nil
}

// Execute overrides BaseAgent.Execute to handle log analysis tasks.
func (l *LogAnalystAgent) Execute(ctx context.Context, task *Task) (*ExecutionResult, error) {
	startTime := time.Now()

	// Add timeout if not set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && l.BaseAgent.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.BaseAgent.config.Timeout)
		defer cancel()
	}

	l.BaseAgent.conversation.AddMessage(Message{
		Role:      "user",
		Content:   task.Description,
		Timestamp: time.Now(),
	})

	l.logger.Info("LogAnalystAgent execution started",
		observability.String("task_id", task.ID),
	)

	// Check if this is a log analysis task
	if taskType, ok := task.Params["type"].(string); ok && taskType == "log_analysis" {
		return l.executeLogAnalysisTask(ctx, task)
	}

	// Default execution
	result, err := l.executeTask(ctx, task)
	duration := time.Since(startTime)
	result.Duration = duration

	l.logger.Info("LogAnalystAgent execution completed",
		observability.String("task_id", task.ID),
		observability.Bool("success", result.Success),
		observability.String("duration", duration.String()),
	)

	return result, err
}

// executeLogAnalysisTask executes a log analysis task.
func (l *LogAnalystAgent) executeLogAnalysisTask(ctx context.Context, task *Task) (*ExecutionResult, error) {
	// Extract parameters
	service := getString(task.Params, "service")
	startTimeStr := getString(task.Params, "start_time")
	endTimeStr := getString(task.Params, "end_time")

	start, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("invalid start_time format: %w", err),
		}, nil
	}

	end, err := time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("invalid end_time format: %w", err),
		}, nil
	}

	// Perform analysis
	analysis, err := l.AnalyzeLogs(ctx, service, start, end)
	if err != nil {
		return &ExecutionResult{
			Success: false,
			Error:   err,
			Response: fmt.Sprintf("Log analysis failed: %v", err),
		}, nil
	}

	// Format response
	response := l.formatAnalysisResult(analysis)

	return &ExecutionResult{
		Success:  true,
		Response: response,
		Data: map[string]interface{}{
			"service":       analysis.Service,
			"total_errors":  analysis.TotalErrors,
			"pattern_count": len(analysis.ErrorPatterns),
			"timeline":      analysis.Timeline,
		},
		ToolsUsed: []string{"log_analysis"},
	}, nil
}

// formatAnalysisResult formats the analysis result for human consumption.
func (l *LogAnalystAgent) formatAnalysisResult(analysis *LogAnalysisResult) string {
	var output string

	output += fmt.Sprintf("## Log Analysis Report\n\n")
	output += fmt.Sprintf("**Service:** %s\n", analysis.Service)
	output += fmt.Sprintf("**Time Range:** %s to %s\n",
		analysis.StartTime.Format(time.RFC3339),
		analysis.EndTime.Format(time.RFC3339))
	output += fmt.Sprintf("**Total Errors:** %d\n\n", analysis.TotalErrors)

	if len(analysis.ErrorPatterns) > 0 {
		output += fmt.Sprintf("### Detected Error Patterns (%d)\n\n", len(analysis.ErrorPatterns))
		for i, pattern := range analysis.ErrorPatterns {
			output += fmt.Sprintf("%d. **%s**\n", i+1, pattern.Pattern)
			output += fmt.Sprintf("   - Count: %d\n", pattern.Count)
			output += fmt.Sprintf("   - Severity: %s\n", pattern.Severity)
			output += fmt.Sprintf("   - Services: %v\n", pattern.Services)
			output += fmt.Sprintf("   - Example: `%s`\n\n", pattern.Example)
		}
	}

	if len(analysis.Timeline) > 0 {
		output += fmt.Sprintf("### Error Timeline\n\n")
		for _, slot := range analysis.Timeline {
			if slot.ErrorCount > 0 {
				output += fmt.Sprintf("- %s: %d errors\n",
					slot.StartTime.Format("15:04"), slot.ErrorCount)
			}
		}
	}

	return output
}

// executeTask implements the default task execution.
func (l *LogAnalystAgent) executeTask(ctx context.Context, task *Task) (*ExecutionResult, error) {
	return &ExecutionResult{
		Success:  true,
		Response: fmt.Sprintf("LogAnalystAgent processed task: %s\n\nUse type='log_analysis' parameter to perform log analysis.", task.Description),
		Data: map[string]interface{}{
			"capabilities": l.GetConfig().Capabilities,
		},
	}, nil
}

// CreateLogAnalystTools creates the standard set of tools for LogAnalystAgent.
func CreateLogAnalystTools() []*tool.Tool {
	return []*tool.Tool{
		createAnalyzeLogsTool(),
		createQueryLogsAdvancedTool(),
		createCorrelateLogsTool(),
	}
}

// createAnalyzeLogsTool creates the log analysis tool.
func createAnalyzeLogsTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"service": {"type": "string", "description": "Service name to analyze logs for"},
			"start_time": {"type": "string", "description": "Analysis start time in RFC3339 format"},
			"end_time": {"type": "string", "description": "Analysis end time in RFC3339 format"},
			"error_level": {"type": "string", "description": "Error level to filter", "enum": ["error", "warning", "info"]},
			"max_results": {"type": "integer", "description": "Maximum results to return", "default": 100}
		},
		"required": ["service", "start_time", "end_time"]
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "analyze_logs",
			Description: "Analyze error logs and detect patterns for a service",
			Source:      tool.DataSourceTypeSLS,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "logs",
			UseCases: []string{
				"Detect error patterns",
				"Identify recurring issues",
				"Correlate logs with incidents",
			},
		},
		IsAvailable: func(config map[string]interface{}) bool { return true },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"status": "use LogAnalystAgent.AnalyzeLogs method directly"}, nil
		},
	}
}

// createQueryLogsAdvancedTool creates the advanced log query tool.
func createQueryLogsAdvancedTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Log query expression"},
			"start_time": {"type": "string", "description": "Query start time in RFC3339 format"},
			"end_time": {"type": "string", "description": "Query end time in RFC3339 format"},
			"limit": {"type": "integer", "description": "Maximum number of entries", "default": 100}
		},
		"required": ["query", "start_time", "end_time"]
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "query_logs_advanced",
			Description: "Query logs from SLS with custom filters and pattern detection",
			Source:      tool.DataSourceTypeSLS,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "logs",
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"message": "advanced log query - to be implemented with SLS integration", "query": params["query"]}, nil
		},
	}
}

// createCorrelateLogsTool creates the log correlation tool.
func createCorrelateLogsTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"service": {"type": "string", "description": "Service name"},
			"incident_time": {"type": "string", "description": "Incident timestamp in RFC3339 format"},
			"window_minutes": {"type": "integer", "description": "Time window in minutes", "default": 30}
		},
		"required": ["service", "incident_time"]
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "correlate_logs",
			Description: "Correlate error logs with an incident time to find related errors",
			Source:      tool.DataSourceTypeGeneric,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "logs",
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"message": "log correlation - use LogAnalystAgent.CorrelateWithIncident method directly"}, nil
		},
	}
}
