package agent

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/tool"
)

// TestExecuteTaskReportsToolsUsed verifies that tools executed during the
// LLM tool loop surface in the returned ExecutionResult instead of always
// being an empty slice.
func TestExecuteTaskReportsToolsUsed(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	agent, err := NewBaseAgent(&AgentConfig{
		Name:        "test-agent",
		Type:        AgentTypeMonitor,
		Description: "test agent",
		Model:       "claude-sonnet-4-20250514",
		Timeout:     30 * time.Second,
	}, settings)
	if err != nil {
		t.Fatalf("NewBaseAgent failed: %v", err)
	}
	defer agent.Shutdown(context.Background())

	if err := agent.Initialize([]*tool.Tool{NewMockTool("query_metrics", "Query metrics")}); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	useStubLLMWithTools(agent, []string{"query_metrics"})

	result, err := agent.executeTask(context.Background(), &Task{
		ID:          "task-tools",
		Description: "query metrics",
	})
	if err != nil {
		t.Fatalf("executeTask failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("expected success, got failure: %s", result.Response)
	}
	if want := []string{"query_metrics"}; !reflect.DeepEqual(result.ToolsUsed, want) {
		t.Errorf("ToolsUsed = %v, want %v", result.ToolsUsed, want)
	}
}
