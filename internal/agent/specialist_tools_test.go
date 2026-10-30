package agent

import (
	"context"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/tool"
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

// assertBoundTools verifies a specialist's BoundTools exposes the expected
// tool names. The old package-level CreateXxxTools were removed; each agent's
// BoundTools is now the single source of its real tools.
func assertBoundTools(t *testing.T, tools []*tool.Tool, expected []string) {
	t.Helper()

	got := make(map[string]bool, len(tools))
	for _, tl := range tools {
		if tl != nil && tl.Metadata != nil {
			got[tl.Metadata.Name] = true
		}
	}
	if len(got) != len(expected) {
		t.Fatalf("expected %d bound tools, got %d (%v)", len(expected), len(got), got)
	}
	for _, name := range expected {
		if !got[name] {
			t.Errorf("expected tool %q in bound tools, got %v", name, got)
		}
	}
}

// TestSpecialistAgentsRegisterTools guards the free-text delegation path: each
// specialist must expose its dedicated tools through BoundTools.
func TestSpecialistAgentsRegisterTools(t *testing.T) {
	settings := specialistTestSettings()

	t.Run("monitor", func(t *testing.T) {
		agent, err := NewMonitorAgent(settings, nil)
		if err != nil {
			t.Fatalf("NewMonitorAgent failed: %v", err)
		}
		defer agent.Shutdown(context.Background())

		assertBoundTools(t, agent.BoundTools(), []string{
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

		assertBoundTools(t, agent.BoundTools(), []string{
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

		assertBoundTools(t, agent.BoundTools(), []string{
			"run_inspection",
			"check_health",
			"validate_compliance",
		})
	})
}
