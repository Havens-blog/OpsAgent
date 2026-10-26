// Package agent provides tests for InspectorAgent.
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
)

// TestNewInspectorAgent tests InspectorAgent creation.
func TestNewInspectorAgent(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	t.Run("creates_agent_successfully", func(t *testing.T) {
		agent, err := NewInspectorAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create InspectorAgent: %v", err)
		}
		defer agent.Shutdown(context.Background())

		if agent == nil {
			t.Fatal("Expected non-nil agent")
		}

		cfg := agent.GetConfig()
		if cfg.Type != AgentTypeInspector {
			t.Errorf("Expected type %s, got %s", AgentTypeInspector, cfg.Type)
		}

		if !agent.HasCapability("inspection") {
			t.Error("Expected inspection capability")
		}
		if !agent.HasCapability("compliance_check") {
			t.Error("Expected compliance_check capability")
		}
	})
}

// TestInspectorAgentRunInspection tests inspection functionality.
func TestInspectorAgentRunInspection(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	t.Run("runs_cloud_inspection", func(t *testing.T) {
		agent, err := NewInspectorAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create InspectorAgent: %v", err)
		}
		defer agent.Shutdown(context.Background())

		ctx := context.Background()

		report, err := agent.RunInspection(ctx, &InspectionParams{
			Type:      "cloud_resources",
			Providers: []string{"aliyun"},
			Services:  []string{"ecs", "rds"},
		})
		if err != nil {
			t.Fatalf("RunInspection failed: %v", err)
		}

		if report == nil {
			t.Fatal("Expected non-nil report")
		}

		if report.Type != "cloud_resources" {
			t.Errorf("Expected type 'cloud_resources', got '%s'", report.Type)
		}
	})

	t.Run("runs_elasticsearch_health_check", func(t *testing.T) {
		agent, err := NewInspectorAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create InspectorAgent: %v", err)
		}
		defer agent.Shutdown(context.Background())

		ctx := context.Background()

		report, err := agent.RunInspection(ctx, &InspectionParams{
			Type:          "elasticsearch",
			ClusterURL:    "http://localhost:9200",
			CheckIndices:  true,
			CheckNodes:    true,
		})
		if err != nil {
			t.Fatalf("RunInspection failed: %v", err)
		}

		if report == nil {
			t.Fatal("Expected non-nil report")
		}

		if report.Status != "completed" {
			t.Errorf("Expected status 'completed', got '%s'", report.Status)
		}
	})

	t.Run("validates_config_compliance", func(t *testing.T) {
		agent, err := NewInspectorAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create InspectorAgent: %v", err)
		}
		defer agent.Shutdown(context.Background())

		ctx := context.Background()

		report, err := agent.RunInspection(ctx, &InspectionParams{
			Type:         "compliance",
			ComplianceID: "CIS-aliyun-1.0.0",
			ResourceType: "all",
		})
		if err != nil {
			t.Fatalf("RunInspection failed: %v", err)
		}

		if report == nil {
			t.Fatal("Expected non-nil report")
		}

		// Compliance reports should include findings
		if len(report.Findings) == 0 {
			t.Error("Expected compliance findings")
		}
	})
}

// TestInspectorAgentExecute tests task execution.
func TestInspectorAgentExecute(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	t.Run("inspection_task", func(t *testing.T) {
		agent, err := NewInspectorAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create InspectorAgent: %v", err)
		}
		defer agent.Shutdown(context.Background())

		task := &Task{
			ID:          "inspection-task-001",
			Description: "Run cloud resource inspection",
			Params: map[string]interface{}{
				"type":           "inspection",
				"inspection_type": "cloud_resources",
				"providers":      []string{"aliyun"},
			},
			CreatedAt: time.Now(),
			Status:    "pending",
		}

		result, err := agent.Execute(context.Background(), task)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success, got error response: %s", result.Response)
		}

		if result.Data == nil {
			t.Error("Expected data in result")
		}
	})

	t.Run("non_inspection_task", func(t *testing.T) {
		agent, err := NewInspectorAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create InspectorAgent: %v", err)
		}
		defer agent.Shutdown(context.Background())

		task := &Task{
			ID:          "generic-task",
			Description: "Generic task",
			CreatedAt:    time.Now(),
			Status:       "pending",
		}

		result, err := agent.Execute(context.Background(), task)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Error("Expected success for generic task")
		}
	})
}
