// Package agent provides tests for agent implementations.
package agent

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/tool"
)

// TestMockAgent tests the MockAgent implementation.
func TestMockAgent(t *testing.T) {
	// Create test settings
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	// Create mock agent
	agent, err := NewMockAgent(settings)
	if err != nil {
		t.Fatalf("Failed to create mock agent: %v", err)
	}
	defer agent.Shutdown(context.Background())

	// Test 1: Execute a task
	t.Run("ExecuteTask", func(t *testing.T) {
		task := &Task{
			ID:          "test-task-1",
			Description: "Test task description",
			Params:      map[string]interface{}{"key": "value"},
			CreatedAt:   time.Now(),
			Status:      "pending",
		}

		result, err := agent.Execute(context.Background(), task)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Error("Expected success to be true")
		}

		if agent.GetExecuteCount() != 1 {
			t.Errorf("Expected execute count to be 1, got %d", agent.GetExecuteCount())
		}
	})

	// Test 2: Multiple executions
	t.Run("MultipleExecutions", func(t *testing.T) {
		agent.ResetCount()

		for i := 0; i < 3; i++ {
			task := &Task{
				ID:          fmt.Sprintf("task-%d", i),
				Description: "Test task",
				CreatedAt:   time.Now(),
				Status:      "pending",
			}
			_, err := agent.Execute(context.Background(), task)
			if err != nil {
				t.Fatalf("Execute %d failed: %v", i, err)
			}
		}

		if agent.GetExecuteCount() != 3 {
			t.Errorf("Expected execute count to be 3, got %d", agent.GetExecuteCount())
		}
	})

	// Test 3: Conversation management
	t.Run("ConversationManagement", func(t *testing.T) {
		convID := agent.GetConversationID()
		if convID == "" {
			t.Error("Expected conversation ID to be set")
		}

		conv := agent.GetConversation()
		if conv == nil {
			t.Fatal("Expected conversation to exist")
		}

		initialCount := len(conv.GetMessages())

		// Execute a task to add messages
		task := &Task{
			ID:          "conv-test",
			Description: "Conversation test",
			CreatedAt:   time.Now(),
			Status:      "pending",
		}
		_, err := agent.Execute(context.Background(), task)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		// Check that messages were added
		conv = agent.GetConversation()
		newCount := len(conv.GetMessages())
		if newCount <= initialCount {
			t.Errorf("Expected messages to be added, got %d (was %d)", newCount, initialCount)
		}
	})

	// Test 4: Reset conversation
	t.Run("ResetConversation", func(t *testing.T) {
		oldID := agent.GetConversationID()

		err := agent.ResetConversation()
		if err != nil {
			t.Fatalf("ResetConversation failed: %v", err)
		}

		newID := agent.GetConversationID()
		if newID == oldID {
			t.Error("Expected new conversation ID after reset")
		}

		conv := agent.GetConversation()
		// Should only have system prompt after reset
		msgCount := len(conv.GetMessages())
		if msgCount > 1 {
			t.Errorf("Expected at most 1 message (system prompt) after reset, got %d", msgCount)
		}
	})
}

// TestMockTool tests the MockTool implementation.
func TestMockTool(t *testing.T) {
	tool := NewMockTool("test_tool", "A test tool")

	if tool.Metadata.Name != "test_tool" {
		t.Errorf("Expected tool name to be 'test_tool', got '%s'", tool.Metadata.Name)
	}

	// Execute the tool
	result, err := tool.Run(context.Background(), map[string]interface{}{"param": "value"})
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be a map")
	}

	if resultMap["tool"] != "test_tool" {
		t.Errorf("Expected tool name in result to be 'test_tool', got '%v'", resultMap["tool"])
	}
}

// TestBaseAgentWithTools tests BaseAgent with tool registration.
func TestBaseAgentWithTools(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		Agent:         config.AgentConfig{AnthropicAPIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	agent, err := NewMockAgent(settings)
	if err != nil {
		t.Fatalf("Failed to create mock agent: %v", err)
	}
	defer agent.Shutdown(context.Background())

	// Register mock tools
	tools := []*tool.Tool{
		NewMockTool("tool1", "First mock tool"),
		NewMockTool("tool2", "Second mock tool"),
	}

	err = agent.Initialize(tools)
	if err != nil {
		t.Fatalf("Failed to initialize agent: %v", err)
	}

	// Check available tools
	availableTools := agent.GetAvailableTools()
	if len(availableTools) < 2 {
		t.Errorf("Expected at least 2 available tools, got %d", len(availableTools))
	}

	// Test tool execution
	ctx := context.Background()
	result, err := agent.ExecuteTool(ctx, "tool1", map[string]interface{}{"test": "data"})
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be a map")
	}

	if resultMap["tool"] != "tool1" {
		t.Errorf("Expected result tool to be 'tool1', got '%v'", resultMap["tool"])
	}
}

// TestAgentCapabilities tests agent capability checking.
func TestAgentCapabilities(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		Agent:         config.AgentConfig{AnthropicAPIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	agent, err := NewMockAgent(settings)
	if err != nil {
		t.Fatalf("Failed to create mock agent: %v", err)
	}
	defer agent.Shutdown(context.Background())

	// Check mock capabilities
	if !agent.HasCapability("test") {
		t.Error("Expected agent to have 'test' capability")
	}

	if !agent.HasCapability("mock") {
		t.Error("Expected agent to have 'mock' capability")
	}

	if agent.HasCapability("nonexistent") {
		t.Error("Expected agent to not have 'nonexistent' capability")
	}
}

// TestAgentWithAuditLog tests agent with audit logging enabled.
func TestAgentWithAuditLog(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	auditPath := tmpDir + "/audit.log"

	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		Agent:         config.AgentConfig{AnthropicAPIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: true, AuditLogPath: auditPath},
		DataSources:   make(map[string]interface{}),
	}

	agent, err := NewMockAgent(settings)
	if err != nil {
		t.Fatalf("Failed to create mock agent: %v", err)
	}
	defer agent.Shutdown(context.Background())

	// Execute a task
	task := &Task{
		ID:          "audit-test",
		Description: "Test audit logging",
		CreatedAt:   time.Now(),
		Status:      "pending",
	}

	_, err = agent.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Check that audit log was created
	_, err = os.Stat(auditPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Error("Expected audit log file to be created")
		} else {
			t.Fatalf("Failed to stat audit log: %v", err)
		}
	}
}
