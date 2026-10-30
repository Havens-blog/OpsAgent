package agent

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/llm"
	"github.com/Havens-blog/OpsAgent/internal/tool"
)

// captureLLM records the tools passed to ChatWithTools so the test can assert
// the input schema reaches the LLM instead of being silently dropped.
type captureLLM struct {
	tools []llm.ClaudeTool
}

func (c *captureLLM) Chat(_ context.Context, _ []llm.ClaudeMessage) (string, error) {
	return "done", nil
}

func (c *captureLLM) ChatWithTools(_ context.Context, _ []llm.ClaudeMessage, tools []llm.ClaudeTool) (string, []string, error) {
	c.tools = tools
	return "done", nil, nil
}

// TestExecuteTaskPassesToolInputSchema guards the tool schema wiring: a tool's
// public input schema must reach the LLM, otherwise the model cannot know the
// arguments it is allowed to pass.
func TestExecuteTaskPassesToolInputSchema(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	agent, err := NewBaseAgent(&AgentConfig{
		Name:        "test-agent",
		Type:        AgentTypeLogAnalyst,
		Description: "test agent",
		Model:       "claude-sonnet-4-20250514",
		Timeout:     30 * time.Second,
	}, settings)
	if err != nil {
		t.Fatalf("NewBaseAgent failed: %v", err)
	}
	defer agent.Shutdown(context.Background())

	if err := agent.Initialize([]*tool.Tool{NewMockTool("query_logs", "Query logs")}); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	capture := &captureLLM{}
	agent.llmClient = capture

	if _, err := agent.executeTask(context.Background(), &Task{ID: "task-schema", Description: "query logs"}); err != nil {
		t.Fatalf("executeTask failed: %v", err)
	}

	if len(capture.tools) != 1 {
		t.Fatalf("expected 1 tool passed to LLM, got %d", len(capture.tools))
	}
	schema, ok := capture.tools[0].InputSchema.(map[string]interface{})
	if !ok || schema == nil {
		t.Fatalf("expected non-nil object input schema, got %#v", capture.tools[0].InputSchema)
	}
	if schema["properties"] == nil {
		t.Fatalf("expected input schema to expose properties, got %#v", schema)
	}
}
