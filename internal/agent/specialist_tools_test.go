package agent

import (
	"context"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/config"
)

func specialistTestSettings() *config.Settings {
	return &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}
}

func assertAvailableTools(t *testing.T, available []string, expected []string) {
	t.Helper()

	got := make(map[string]bool, len(available))
	for _, name := range available {
		got[name] = true
	}
	for _, name := range expected {
		if !got[name] {
			t.Errorf("expected tool %q in available tools, got %v", name, available)
		}
	}
}

// TestSpecialistAgentsRegisterTools guards the free-text delegation path: each
// specialist must expose its dedicated tools through the base tool registry.
func TestSpecialistAgentsRegisterTools(t *testing.T) {
	settings := specialistTestSettings()

	t.Run("monitor", func(t *testing.T) {
		agent, err := NewMonitorAgent(settings, nil)
		if err != nil {
			t.Fatalf("NewMonitorAgent failed: %v", err)
		}
		defer agent.Shutdown(context.Background())

		tools := agent.BoundTools()
		available := agent.GetAvailableTools()
		if len(available) != len(tools) {
			t.Fatalf("expected %d available tools, got %d (%v)", len(tools), len(available), available)
		}
		assertAvailableTools(t, available, []string{
			"analyze_incident",
			"query_metrics",
			"query_logs",
			"query_traces",
			"get_topology",
		})
	})

	t.Run("log_analyst", func(t *testing.T) {
		agent, err := NewLogAnalystAgent(settings)
		if err != nil {
			t.Fatalf("NewLogAnalystAgent failed: %v", err)
		}
		defer agent.Shutdown(context.Background())

		tools := agent.BoundTools()
		available := agent.GetAvailableTools()
		if len(available) != len(tools) {
			t.Fatalf("expected %d available tools, got %d (%v)", len(tools), len(available), available)
		}
		assertAvailableTools(t, available, []string{
			"analyze_logs",
			"query_logs_advanced",
			"correlate_logs",
		})
	})

	t.Run("inspector", func(t *testing.T) {
		agent, err := NewInspectorAgent(settings)
		if err != nil {
			t.Fatalf("NewInspectorAgent failed: %v", err)
		}
		defer agent.Shutdown(context.Background())

		tools := agent.BoundTools()
		available := agent.GetAvailableTools()
		if len(available) != len(tools) {
			t.Fatalf("expected %d available tools, got %d (%v)", len(tools), len(available), available)
		}
		assertAvailableTools(t, available, []string{
			"run_inspection",
			"check_health",
			"validate_compliance",
		})
	})
}
