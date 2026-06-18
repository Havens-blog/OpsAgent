// Package agent provides tests for MonitorAgent.
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// TestNewMonitorAgent tests MonitorAgent creation.
func TestNewMonitorAgent(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	t.Run("Without RCA sources", func(t *testing.T) {
		agent, err := NewMonitorAgent(settings, nil)
		if err != nil {
			t.Fatalf("NewMonitorAgent failed: %v", err)
		}

		if agent == nil {
			t.Fatal("Expected non-nil agent")
		}

		// Verify RCA is not initialized when no sources provided
		status := agent.GetRCAStatus()
		if initialized, ok := status["initialized"].(bool); ok && initialized {
			t.Error("Expected RCA not to be initialized without sources")
		}
		if ready, ok := status["ready"].(bool); ok && ready {
			t.Error("Expected RCA not to be ready without sources")
		}

		agent.Shutdown(context.Background())
	})

	t.Run("With Mock RCA sources", func(t *testing.T) {
		rcaSources := CreateMockRCASources()
		agent, err := NewMonitorAgent(settings, rcaSources)
		if err != nil {
			t.Fatalf("NewMonitorAgent with RCA failed: %v", err)
		}

		status := agent.GetRCAStatus()
		if ready, ok := status["ready"].(bool); !ok || !ready {
			t.Error("Expected RCA to be ready with mock sources")
		}

		agent.Shutdown(context.Background())
	})
}

// TestMonitorAgentAnalyzeIncident tests RCA analysis.
func TestMonitorAgentAnalyzeIncident(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	rcaSources := CreateMockRCASources()
	agent, err := NewMonitorAgent(settings, rcaSources)
	if err != nil {
		t.Fatalf("NewMonitorAgent failed: %v", err)
	}
	defer agent.Shutdown(context.Background())

	ctx := context.Background()

	t.Run("Valid incident", func(t *testing.T) {
		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()

		incident := &models.Incident{
			ID:              "INC-001",
			StartTime:       start,
			EndTime:         end,
			Severity:        "P1",
			Description:     "High error rate detected",
			AffectedServices: []string{"order-service", "payment-service"},
		}

		report, err := agent.AnalyzeIncident(ctx, incident)
		if err != nil {
			t.Fatalf("AnalyzeIncident failed: %v", err)
		}

		if report == nil {
			t.Fatal("Expected non-nil report")
		}

		if report.Incident.ID != incident.ID {
			t.Errorf("Expected incident ID %s, got %s", incident.ID, report.Incident.ID)
		}
	})

	t.Run("Invalid incident - no ID", func(t *testing.T) {
		incident := &models.Incident{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
			Severity:  "P1",
		}

		_, err := agent.AnalyzeIncident(ctx, incident)
		if err == nil {
			t.Error("Expected error for invalid incident")
		}
	})

	t.Run("Invalid incident - end before start", func(t *testing.T) {
		start := time.Now()
		end := time.Now().Add(-1 * time.Hour)

		incident := &models.Incident{
			ID:        "INC-002",
			StartTime: start,
			EndTime:   end,
			Severity:  "P1",
		}

		_, err := agent.AnalyzeIncident(ctx, incident)
		if err == nil {
			t.Error("Expected error for end time before start time")
		}
	})
}

// TestMonitorAgentExecute tests the Execute method.
func TestMonitorAgentExecute(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	rcaSources := CreateMockRCASources()
	agent, err := NewMonitorAgent(settings, rcaSources)
	if err != nil {
		t.Fatalf("NewMonitorAgent failed: %v", err)
	}
	defer agent.Shutdown(context.Background())

	ctx := context.Background()

	t.Run("RCA task", func(t *testing.T) {
		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()

		task := &Task{
			ID:          "task-001",
			Description: "Analyze incident",
			Params: map[string]interface{}{
				"type": "rca",
				"incident": map[string]interface{}{
					"id":               "INC-001",
					"start_time":       start.Format(time.RFC3339),
					"end_time":         end.Format(time.RFC3339),
					"severity":         "P1",
					"description":      "High error rate",
					"affected_services": []string{"order-service"},
				},
			},
			CreatedAt: time.Now(),
			Status:    "pending",
		}

		result, err := agent.Execute(ctx, task)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success, got failure: %s", result.Response)
		}

		if result.Data == nil {
			t.Error("Expected data in result")
		}
	})

	t.Run("Non-RCA task", func(t *testing.T) {
		task := &Task{
			ID:          "task-002",
			Description: "Generic task",
			Params:      map[string]interface{}{},
			CreatedAt:   time.Now(),
			Status:      "pending",
		}

		result, err := agent.Execute(ctx, task)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success, got failure")
		}
	})

	t.Run("RCA task with invalid incident data", func(t *testing.T) {
		task := &Task{
			ID:          "task-003",
			Description: "Invalid RCA task",
			Params: map[string]interface{}{
				"type":     "rca",
				"incident": "invalid", // Not a map
			},
			CreatedAt: time.Now(),
			Status:    "pending",
		}

		result, err := agent.Execute(ctx, task)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if result.Success {
			t.Error("Expected failure for invalid incident data")
		}
	})
}

// TestMonitorAgentGetRCAStatus tests GetRCAStatus.
func TestMonitorAgentGetRCAStatus(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	t.Run("Without RCA", func(t *testing.T) {
		agent, err := NewMonitorAgent(settings, nil)
		if err != nil {
			t.Fatalf("NewMonitorAgent failed: %v", err)
		}
		defer agent.Shutdown(context.Background())

		status := agent.GetRCAStatus()
		if initialized, ok := status["initialized"].(bool); !ok || initialized {
			t.Error("Expected initialized=false without RCA")
		}
	})

	t.Run("With RCA", func(t *testing.T) {
		rcaSources := CreateMockRCASources()
		agent, err := NewMonitorAgent(settings, rcaSources)
		if err != nil {
			t.Fatalf("NewMonitorAgent failed: %v", err)
		}
		defer agent.Shutdown(context.Background())

		status := agent.GetRCAStatus()
		if initialized, ok := status["initialized"].(bool); !ok || !initialized {
			t.Error("Expected initialized=true with RCA")
		}
		if ready, ok := status["ready"].(bool); !ok || !ready {
			t.Error("Expected ready=true with RCA")
		}
		if agentName, ok := status["agent"].(string); !ok || agentName != "monitor-agent" {
			t.Errorf("Expected agent name 'monitor-agent', got '%v'", status["agent"])
		}
	})
}

// TestCreateMonitorTools tests tool creation.
func TestCreateMonitorTools(t *testing.T) {
	tools := CreateMonitorTools()

	if len(tools) != 5 {
		t.Errorf("Expected 5 tools, got %d", len(tools))
	}

	expectedTools := []string{
		"analyze_incident",
		"query_metrics",
		"query_logs",
		"query_traces",
		"get_topology",
	}

	toolNames := make(map[string]bool)
	for _, t := range tools {
		toolNames[t.Metadata.Name] = true
	}

	for _, expected := range expectedTools {
		if !toolNames[expected] {
			t.Errorf("Expected tool '%s' not found", expected)
		}
	}
}
