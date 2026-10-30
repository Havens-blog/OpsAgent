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

// TestInspectorAgentRunInspection locks in honest refusal: with no real
// inspection backend wired, RunInspection must error instead of fabricating
// findings (the old "Simulate" implementation invented resource counts and
// compliance violations).
func TestInspectorAgentRunInspection(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	cases := []struct {
		name   string
		params *InspectionParams
	}{
		{"cloud_inspection_refuses_to_fabricate", &InspectionParams{
			Type:      "cloud_resources",
			Providers: []string{"aliyun"},
			Services:  []string{"ecs", "rds"},
		}},
		{"elasticsearch_inspection_refuses_to_fabricate", &InspectionParams{
			Type:         "elasticsearch",
			ClusterURL:   "http://localhost:9200",
			CheckIndices: true,
			CheckNodes:   true,
		}},
		{"compliance_check_refuses_to_fabricate", &InspectionParams{
			Type:         "compliance",
			ComplianceID: "CIS-aliyun-1.0.0",
			ResourceType: "all",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent, err := NewInspectorAgent(settings)
			if err != nil {
				t.Fatalf("Failed to create InspectorAgent: %v", err)
			}
			defer agent.Shutdown(context.Background())

			report, err := agent.RunInspection(context.Background(), tc.params)
			if err == nil {
				t.Fatalf("expected honest error, got report %+v", report)
			}
			if report != nil {
				t.Errorf("report must be nil on error, got %+v", report)
			}
		})
	}
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

		// No real inspection backend is wired: the task must fail HONESTLY
		// instead of returning fabricated findings.
		if result.Success {
			t.Errorf("Expected honest failure without a real backend, got success: %s", result.Response)
		}
		if result.Error == nil {
			t.Error("Expected error to be set on the failed result")
		}
	})

	t.Run("non_inspection_task", func(t *testing.T) {
		agent, err := NewInspectorAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create InspectorAgent: %v", err)
		}
		defer agent.Shutdown(context.Background())
		useStubLLM(agent.BaseAgent) // default branch routes to LLM; keep it hermetic

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
