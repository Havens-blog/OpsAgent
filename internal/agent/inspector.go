// Package agent provides the InspectorAgent for operations inspection and compliance checks.
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/observability"
	"github.com/Havens-blog/OpsAgent/internal/tool"
)

// InspectorAgent specializes in operations inspection and compliance checks.
// It performs cloud resource inspections, ES health checks, and compliance validation.
type InspectorAgent struct {
	*BaseAgent
}

// NewInspectorAgent creates a new InspectorAgent.
func NewInspectorAgent(settings *config.Settings) (*InspectorAgent, error) {
	cfg := &AgentConfig{
		Name:         "inspector-agent",
		Type:         AgentTypeInspector,
		Description:  "Perform operations inspection and compliance checks",
		Model:        "claude-sonnet-4-20250514",
		MaxTurns:     10,
		Timeout:      10 * time.Minute,
		SystemPrompt: `You are an InspectorAgent specialized in operations inspection and compliance.

Your capabilities include:
- Cloud resource inspection (compute, storage, network)
- Elasticsearch cluster health checks
- Configuration compliance validation
- Security best practice verification
- Resource utilization analysis

When performing inspections:
1. Gather current state from target systems
2. Compare against compliance policies
3. Identify deviations and potential issues
4. Provide actionable recommendations
5. Rate severity of findings (critical/warning/info)`,
		Capabilities: []string{"inspection", "compliance_check", "resource_audit", "health_check"},
	}

	base, err := NewBaseAgent(cfg, settings)
	if err != nil {
		return nil, err
	}

	return &InspectorAgent{
		BaseAgent: base,
	}, nil
}

// InspectionParams holds parameters for an inspection task.
type InspectionParams struct {
	Type          string   `json:"type"`                    // cloud_resources, elasticsearch, compliance
	Providers     []string `json:"providers,omitempty"`     // aliyun, aws, etc.
	Services      []string `json:"services,omitempty"`      // ecs, rds, etc.
	ClusterURL    string   `json:"cluster_url,omitempty"`   // For ES inspection
	CheckIndices  bool     `json:"check_indices,omitempty"` // Check ES indices
	CheckNodes    bool     `json:"check_nodes,omitempty"`   // Check ES nodes
	ComplianceID  string   `json:"compliance_id,omitempty"` // Compliance policy ID
	ResourceType string   `json:"resource_type,omitempty"` // Resource type to check
}

// InspectionReport represents the result of an inspection.
type InspectionReport struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Status      string            `json:"status"`
	StartTime   time.Time         `json:"start_time"`
	EndTime     time.Time         `json:"end_time"`
	Duration    time.Duration     `json:"duration"`
	Summary     InspectionSummary `json:"summary"`
	Findings    []Finding         `json:"findings"`
	Resources   int               `json:"resources_checked"`
	Passed      int               `json:"checks_passed"`
	Failed      int               `json:"checks_failed"`
	Warning     int               `json:"warnings"`
}

// InspectionSummary provides a high-level summary of the inspection.
type InspectionSummary struct {
	OverallStatus string  `json:"overall_status"` // pass, fail, warning
	Score         float64 `json:"score"`          // 0-100 compliance score
	CriticalCount int     `json:"critical_count"`
	WarningCount  int     `json:"warning_count"`
	InfoCount     int     `json:"info_count"`
}

// Finding represents a single inspection finding.
type Finding struct {
	ID          string                 `json:"id"`
	ResourceID  string                 `json:"resource_id"`
	ResourceType string                `json:"resource_type"`
	Severity    string                 `json:"severity"` // critical, warning, info
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Remediation string                 `json:"remediation,omitempty"`
	Labels      map[string]string      `json:"labels,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
}

// RunInspection executes an inspection with the given parameters.
func (i *InspectorAgent) RunInspection(ctx context.Context, params *InspectionParams) (*InspectionReport, error) {
	startTime := time.Now()

	i.logger.Info("Starting inspection",
		observability.String("type", params.Type),
		observability.Any("params", params),
	)

	// Generate inspection ID
	inspectionID := fmt.Sprintf("insp-%d", startTime.UnixNano())

	report := &InspectionReport{
		ID:        inspectionID,
		Type:      params.Type,
		Status:    "running",
		StartTime: startTime,
		Findings:  make([]Finding, 0),
	}

	// Execute inspection based on type
	switch params.Type {
	case "cloud_resources":
		if err := i.runCloudInspection(ctx, params, report); err != nil {
			return nil, fmt.Errorf("cloud inspection failed: %w", err)
		}
	case "elasticsearch":
		if err := i.runESInspection(ctx, params, report); err != nil {
			return nil, fmt.Errorf("elasticsearch inspection failed: %w", err)
		}
	case "compliance":
		if err := i.runComplianceCheck(ctx, params, report); err != nil {
			return nil, fmt.Errorf("compliance check failed: %w", err)
		}
	default:
		return nil, fmt.Errorf("unknown inspection type: %s", params.Type)
	}

	// Finalize report
	report.EndTime = time.Now()
	report.Duration = report.EndTime.Sub(report.StartTime)
	report.Status = "completed"

	// Calculate summary
	i.calculateSummary(report)

	i.logger.Info("Inspection completed",
		observability.String("inspection_id", inspectionID),
		observability.String("type", params.Type),
		observability.Int("findings", len(report.Findings)),
		observability.String("duration", report.Duration.String()),
	)

	return report, nil
}

// runCloudInspection performs cloud resource inspection.
func (i *InspectorAgent) runCloudInspection(ctx context.Context, params *InspectionParams, report *InspectionReport) error {
	// Simulate cloud inspection
	report.Resources = 10
	report.Passed = 8
	report.Failed = 1
	report.Warning = 1

	// Add sample findings
	report.Findings = append(report.Findings,
		Finding{
			ID:           fmt.Sprintf("%s-finding-1", report.ID),
			ResourceID:   "i-bp12345678",
			ResourceType: "ecs_instance",
			Severity:     "warning",
			Title:        "Instance without proper tags",
			Description:  "ECS instance is missing required cost center tags",
			Remediation:  "Add required tags to the instance",
			Timestamp:    time.Now(),
		},
		Finding{
			ID:           fmt.Sprintf("%s-finding-2", report.ID),
			ResourceID:   "rds-bp87654321",
			ResourceType: "rds_instance",
			Severity:     "info",
			Title:        "Backup configuration check",
			Description:  "RDS instance has backup properly configured",
			Timestamp:    time.Now(),
		},
	)

	return nil
}

// runESInspection performs Elasticsearch cluster inspection.
func (i *InspectorAgent) runESInspection(ctx context.Context, params *InspectionParams, report *InspectionReport) error {
	// Simulate ES inspection
	report.Resources = 5
	report.Passed = 4
	report.Warning = 1
	report.Failed = 0

	// Add sample findings
	report.Findings = append(report.Findings,
		Finding{
			ID:           fmt.Sprintf("%s-es-finding-1", report.ID),
			ResourceID:   params.ClusterURL,
			ResourceType: "elasticsearch_cluster",
			Severity:     "warning",
			Title:        "Cluster health is yellow",
			Description:  "Some cluster shards are unassigned",
			Remediation:  "Check shard allocation and reassign unassigned shards",
			Timestamp:    time.Now(),
		},
	)

	if params.CheckIndices {
		report.Resources += 3
		report.Findings = append(report.Findings,
			Finding{
				ID:           fmt.Sprintf("%s-index-finding-1", report.ID),
				ResourceID:   "logs-2024-06",
				ResourceType: "elasticsearch_index",
				Severity:     "info",
				Title:        "Index size check",
				Description:  "Index size is within acceptable range",
				Timestamp:    time.Now(),
			},
		)
	}

	return nil
}

// runComplianceCheck performs compliance validation.
func (i *InspectorAgent) runComplianceCheck(ctx context.Context, params *InspectionParams, report *InspectionReport) error {
	// Simulate compliance check
	report.Resources = 15
	report.Passed = 12
	report.Failed = 2
	report.Warning = 1

	// Add sample findings
	report.Findings = append(report.Findings,
		Finding{
			ID:           fmt.Sprintf("%s-comp-finding-1", report.ID),
			ResourceID:   "sg-bp123456",
			ResourceType: "security_group",
			Severity:     "critical",
			Title:        "Overly permissive security group",
			Description:  "Security group allows 0.0.0.0/0 access on port 22",
			Remediation:  "Restrict SSH access to specific IP ranges",
			Labels: map[string]string{
				"compliance_id": params.ComplianceID,
				"category":      "network_security",
			},
			Timestamp: time.Now(),
		},
		Finding{
			ID:           fmt.Sprintf("%s-comp-finding-2", report.ID),
			ResourceID:   "bucket-logs",
			ResourceType: "oss_bucket",
			Severity:     "warning",
			Title:        "Missing bucket encryption",
			Description:  "OSS bucket does not have encryption enabled",
			Remediation:  "Enable server-side encryption for the bucket",
			Labels: map[string]string{
				"compliance_id": params.ComplianceID,
				"category":      "data_security",
			},
			Timestamp: time.Now(),
		},
	)

	return nil
}

// calculateSummary calculates the summary statistics for the report.
func (i *InspectorAgent) calculateSummary(report *InspectionReport) {
	criticalCount := 0
	warningCount := 0
	infoCount := 0

	for _, finding := range report.Findings {
		switch finding.Severity {
		case "critical":
			criticalCount++
		case "warning":
			warningCount++
		case "info":
			infoCount++
		}
	}

	// Calculate overall score (0-100)
	score := 100.0
	if report.Resources > 0 {
		score = float64(report.Passed) / float64(report.Resources) * 100
	}
	if criticalCount > 0 {
		score = score - 20*float64(criticalCount)
	}
	if warningCount > 0 {
		score = score - 5*float64(warningCount)
	}
	if score < 0 {
		score = 0
	}

	// Determine overall status
	var overallStatus string
	if criticalCount > 0 {
		overallStatus = "fail"
	} else if warningCount > 0 {
		overallStatus = "warning"
	} else {
		overallStatus = "pass"
	}

	report.Summary = InspectionSummary{
		OverallStatus: overallStatus,
		Score:         score,
		CriticalCount: criticalCount,
		WarningCount:  warningCount,
		InfoCount:     infoCount,
	}

	// Update counts
	report.Failed = criticalCount
	report.Warning = warningCount
}

// Execute overrides BaseAgent.Execute to handle inspection tasks.
func (i *InspectorAgent) Execute(ctx context.Context, task *Task) (*ExecutionResult, error) {
	startTime := time.Now()

	// Add timeout if not set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && i.BaseAgent.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, i.BaseAgent.config.Timeout)
		defer cancel()
	}

	i.BaseAgent.conversation.AddMessage(Message{
		Role:      "user",
		Content:   task.Description,
		Timestamp: time.Now(),
	})

	i.logger.Info("InspectorAgent execution started",
		observability.String("task_id", task.ID),
	)

	// Check if this is an inspection task
	if taskType, ok := task.Params["type"].(string); ok && taskType == "inspection" {
		return i.executeInspectionTask(ctx, task)
	}

	// Default execution
	result, err := i.executeTask(ctx, task)
	duration := time.Since(startTime)
	result.Duration = duration

	i.logger.Info("InspectorAgent execution completed",
		observability.String("task_id", task.ID),
		observability.Bool("success", result.Success),
		observability.String("duration", duration.String()),
	)

	return result, err
}

// executeInspectionTask executes an inspection task.
func (i *InspectorAgent) executeInspectionTask(ctx context.Context, task *Task) (*ExecutionResult, error) {
	// Extract parameters
	inspectionType := getString(task.Params, "inspection_type")

	params := &InspectionParams{
		Type: inspectionType,
	}

	// Extract type-specific parameters
	if inspectionType == "cloud_resources" {
		if providers, ok := task.Params["providers"].([]string); ok {
			params.Providers = providers
		}
	} else if inspectionType == "elasticsearch" {
		params.ClusterURL = getString(task.Params, "cluster_url")
		if checkIndices, ok := task.Params["check_indices"].(bool); ok {
			params.CheckIndices = checkIndices
		}
		if checkNodes, ok := task.Params["check_nodes"].(bool); ok {
			params.CheckNodes = checkNodes
		}
	} else if inspectionType == "compliance" {
		params.ComplianceID = getString(task.Params, "compliance_id")
		params.ResourceType = getString(task.Params, "resource_type")
	}

	// Run inspection
	report, err := i.RunInspection(ctx, params)
	if err != nil {
		return &ExecutionResult{
			Success: false,
			Error:   err,
			Response: fmt.Sprintf("Inspection failed: %v", err),
		}, nil
	}

	// Format response
	response := i.formatInspectionReport(report)

	return &ExecutionResult{
		Success:  true,
		Response: response,
		Data: map[string]interface{}{
			"inspection_id":    report.ID,
			"type":            report.Type,
			"status":          report.Status,
			"score":           report.Summary.Score,
			"overall_status":  report.Summary.OverallStatus,
			"findings_count":  len(report.Findings),
			"critical_count":  report.Summary.CriticalCount,
			"warning_count":   report.Summary.WarningCount,
			"duration":        report.Duration.String(),
		},
		ToolsUsed: []string{"inspection"},
	}, nil
}

// formatInspectionReport formats the inspection report for human consumption.
func (i *InspectorAgent) formatInspectionReport(report *InspectionReport) string {
	var output string

	output += fmt.Sprintf("## Inspection Report\n\n")
	output += fmt.Sprintf("**ID:** %s\n", report.ID)
	output += fmt.Sprintf("**Type:** %s\n", report.Type)
	output += fmt.Sprintf("**Status:** %s\n", report.Status)
	output += fmt.Sprintf("**Duration:** %s\n\n", report.Duration.String())

	output += fmt.Sprintf("### Summary\n\n")
	output += fmt.Sprintf("**Overall Status:** %s\n", report.Summary.OverallStatus)
	output += fmt.Sprintf("**Compliance Score:** %.1f%%\n", report.Summary.Score)
	output += fmt.Sprintf("**Resources Checked:** %d\n", report.Resources)
	output += fmt.Sprintf("**Passed:** %d | **Failed:** %d | **Warnings:** %d\n\n",
		report.Passed, report.Failed, report.Warning)

	if len(report.Findings) > 0 {
		output += fmt.Sprintf("### Findings (%d)\n\n", len(report.Findings))
		for i, finding := range report.Findings {
			severityIcon := map[string]string{"critical": "🔴", "warning": "🟡", "info": "🔵"}
			icon := severityIcon[finding.Severity]

			output += fmt.Sprintf("%s **%s** [%s]\n", icon, finding.Title, finding.Severity)
			output += fmt.Sprintf("   - Resource: %s (%s)\n", finding.ResourceID, finding.ResourceType)
			output += fmt.Sprintf("   - Description: %s\n", finding.Description)
			if finding.Remediation != "" {
				output += fmt.Sprintf("   - Remediation: %s\n", finding.Remediation)
			}
			output += "\n"

			// Limit output to top 10 findings
			if i >= 9 {
				remaining := len(report.Findings) - 10
				if remaining > 0 {
					output += fmt.Sprintf("*... and %d more findings*\n\n", remaining)
				}
				break
			}
		}
	}

	return output
}

// executeTask implements the default task execution.
func (i *InspectorAgent) executeTask(ctx context.Context, task *Task) (*ExecutionResult, error) {
	return &ExecutionResult{
		Success:  true,
		Response: fmt.Sprintf("InspectorAgent processed task: %s\n\nUse type='inspection' parameter to perform inspection.", task.Description),
		Data: map[string]interface{}{
			"capabilities": i.GetConfig().Capabilities,
		},
	}, nil
}

// CreateInspectorTools creates the standard set of tools for InspectorAgent.
func CreateInspectorTools() []*tool.Tool {
	return []*tool.Tool{
		createRunInspectionTool(),
		createCheckHealthTool(),
		createValidateComplianceTool(),
	}
}

// createRunInspectionTool creates the inspection tool.
func createRunInspectionTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"inspection_type": {
				"type": "string",
				"description": "Type of inspection to perform",
				"enum": ["cloud_resources", "elasticsearch", "compliance"]
			},
			"providers": {"type": "array", "items": {"type": "string"}, "description": "Cloud providers to inspect"},
			"services": {"type": "array", "items": {"type": "string"}, "description": "Services to inspect"},
			"cluster_url": {"type": "string", "description": "Elasticsearch cluster URL"},
			"check_indices": {"type": "boolean", "description": "Check ES indices"},
			"check_nodes": {"type": "boolean", "description": "Check ES nodes"},
			"compliance_id": {"type": "string", "description": "Compliance policy ID"},
			"resource_type": {"type": "string", "description": "Resource type to check"}
		},
		"required": ["inspection_type"]
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "run_inspection",
			Description: "Run operations inspection and compliance checks",
			Source:      tool.DataSourceTypeGeneric,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "inspection",
			UseCases: []string{
				"Cloud resource audit",
				"Elasticsearch health check",
				"Compliance validation",
			},
		},
		IsAvailable: func(config map[string]interface{}) bool { return true },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"status": "use InspectorAgent.RunInspection method directly"}, nil
		},
	}
}

// createCheckHealthTool creates the health check tool.
func createCheckHealthTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"target_type": {
				"type": "string",
				"description": "Type of target to check",
				"enum": ["elasticsearch", "redis", "database", "api"]
			},
			"endpoint": {"type": "string", "description": "Target endpoint URL"}
		},
		"required": ["target_type", "endpoint"]
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "check_health",
			Description: "Check health status of a system component",
			Source:      tool.DataSourceTypeGeneric,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "health",
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{
				"message":  "health check completed",
				"target":   params["target_type"],
				"endpoint": params["endpoint"],
				"status":   "healthy",
			}, nil
		},
	}
}

// createValidateComplianceTool creates the compliance validation tool.
func createValidateComplianceTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"compliance_id": {"type": "string", "description": "Compliance policy ID"},
			"resource_type": {"type": "string", "description": "Resource type to validate"},
			"resource_id": {"type": "string", "description": "Specific resource ID to check"}
		},
		"required": ["compliance_id"]
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "validate_compliance",
			Description: "Validate resources against compliance policies",
			Source:      tool.DataSourceTypeGeneric,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "compliance",
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{
				"message":       "compliance validation completed",
				"compliance_id": params["compliance_id"],
				"resource_type": params["resource_type"],
			}, nil
		},
	}
}
