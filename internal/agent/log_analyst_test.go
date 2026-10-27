// Package agent provides tests for LogAnalystAgent.
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
)

// TestNewLogAnalystAgent tests LogAnalystAgent creation.
func TestNewLogAnalystAgent(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	t.Run("creates_agent_successfully", func(t *testing.T) {
		agent, err := NewLogAnalystAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create LogAnalystAgent: %v", err)
		}
		defer agent.Shutdown(context.Background())

		if agent == nil {
			t.Fatal("Expected non-nil agent")
		}

		cfg := agent.GetConfig()
		if cfg.Type != AgentTypeLogAnalyst {
			t.Errorf("Expected type %s, got %s", AgentTypeLogAnalyst, cfg.Type)
		}

		if !agent.HasCapability("log_analysis") {
			t.Error("Expected log_analysis capability")
		}
		if !agent.HasCapability("pattern_detection") {
			t.Error("Expected pattern_detection capability")
		}
	})
}

// TestLogAnalystAgentAnalyzeLogs tests log analysis functionality.
func TestLogAnalystAgentAnalyzeLogs(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	t.Run("analyzes_error_logs", func(t *testing.T) {
		agent, err := NewLogAnalystAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create LogAnalystAgent: %v", err)
		}
		defer agent.Shutdown(context.Background())

		// Mock log source
		agent.SetLogSource(&MockLogSource{})

		ctx := context.Background()
		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()

		analysis, err := agent.AnalyzeLogs(ctx, "order-service", start, end)
		if err != nil {
			t.Fatalf("AnalyzeLogs failed: %v", err)
		}

		if analysis == nil {
			t.Fatal("Expected non-nil analysis")
		}

		if analysis.Service != "order-service" {
			t.Errorf("Expected service 'order-service', got '%s'", analysis.Service)
		}
	})

	t.Run("detects_error_patterns", func(t *testing.T) {
		agent, err := NewLogAnalystAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create LogAnalystAgent: %v", err)
		}
		defer agent.Shutdown(context.Background())

		agent.SetLogSource(&MockLogSource{})

		ctx := context.Background()
		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()

		analysis, err := agent.AnalyzeLogs(ctx, "order-service", start, end)
		if err != nil {
			t.Fatalf("AnalyzeLogs failed: %v", err)
		}

		if len(analysis.ErrorPatterns) == 0 {
			t.Error("Expected to detect error patterns")
		}
	})

	t.Run("correlates_with_incident", func(t *testing.T) {
		agent, err := NewLogAnalystAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create LogAnalystAgent: %v", err)
		}
		defer agent.Shutdown(context.Background())

		agent.SetLogSource(&MockLogSource{})

		ctx := context.Background()
		incidentTime := time.Now().Add(-30 * time.Minute)

		correlation, err := agent.CorrelateWithIncident(ctx, "order-service", incidentTime)
		if err != nil {
			t.Fatalf("CorrelateWithIncident failed: %v", err)
		}

		if correlation == nil {
			t.Fatal("Expected non-nil correlation")
		}

		if correlation.Service != "order-service" {
			t.Errorf("Expected service 'order-service', got '%s'", correlation.Service)
		}
	})
}

// TestLogAnalystAgentExecute tests task execution.
func TestLogAnalystAgentExecute(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	t.Run("log_analysis_task", func(t *testing.T) {
		agent, err := NewLogAnalystAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create LogAnalystAgent: %v", err)
		}
		defer agent.Shutdown(context.Background())

		agent.SetLogSource(&MockLogSource{})

		task := &Task{
			ID:          "log-task-001",
			Description: "Analyze error logs for order-service",
			Params: map[string]interface{}{
				"type":           "log_analysis",
				"service":        "order-service",
				"start_time":     time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
				"end_time":       time.Now().Format(time.RFC3339),
				"error_level":    "error",
				"max_results":    100,
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

	t.Run("non_log_analysis_task", func(t *testing.T) {
		agent, err := NewLogAnalystAgent(settings)
		if err != nil {
			t.Fatalf("Failed to create LogAnalystAgent: %v", err)
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
