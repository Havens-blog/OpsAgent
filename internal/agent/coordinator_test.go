// Package agent provides tests for CoordinatorAgent.
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
)

// TestNewCoordinatorAgent tests coordinator creation.
func TestNewCoordinatorAgent(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	coord, err := NewCoordinatorAgent(settings)
	if err != nil {
		t.Fatalf("NewCoordinatorAgent failed: %v", err)
	}

	if coord == nil {
		t.Fatal("Expected non-nil coordinator")
	}

	if coord.GetConfig().Type != AgentTypeCoordinator {
		t.Errorf("Expected type %s, got %s", AgentTypeCoordinator, coord.GetConfig().Type)
	}

	coord.Shutdown(context.Background())
}

// TestCoordinatorAgentRegisterAgent tests agent registration.
func TestCoordinatorAgentRegisterAgent(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	coord, err := NewCoordinatorAgent(settings)
	if err != nil {
		t.Fatalf("NewCoordinatorAgent failed: %v", err)
	}
	defer coord.Shutdown(context.Background())

	// Create a mock agent to register
	mockAgent, err := NewMockAgent(settings)
	if err != nil {
		t.Fatalf("NewMockAgent failed: %v", err)
	}

	// Register the mock agent
	err = coord.RegisterAgent("mock", mockAgent.BaseAgent)
	if err != nil {
		t.Errorf("RegisterAgent failed: %v", err)
	}

	// Verify agent is registered
	agents := coord.GetRegisteredAgents()
	if len(agents) != 1 {
		t.Errorf("Expected 1 registered agent, got %d", len(agents))
	}
	if agents[0] != "mock" {
		t.Errorf("Expected agent 'mock', got '%s'", agents[0])
	}

	// Unregister
	coord.UnregisterAgent("mock")
	agents = coord.GetRegisteredAgents()
	if len(agents) != 0 {
		t.Errorf("Expected 0 registered agents after unregister, got %d", len(agents))
	}
}

// TestCoordinatorAgentDelegateTask tests task delegation.
func TestCoordinatorAgentDelegateTask(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	coord, err := NewCoordinatorAgent(settings)
	if err != nil {
		t.Fatalf("NewCoordinatorAgent failed: %v", err)
	}
	defer coord.Shutdown(context.Background())

	// Register a mock agent
	mockAgent, err := NewMockAgent(settings)
	if err != nil {
		t.Fatalf("NewMockAgent failed: %v", err)
	}

	err = coord.RegisterAgent("mock", mockAgent.BaseAgent)
	if err != nil {
		t.Fatalf("RegisterAgent failed: %v", err)
	}

	// Delegate a task
	ctx := context.Background()
	task := &Task{
		ID:          "test-task",
		Description: "Test delegation",
		Params:      map[string]interface{}{},
		CreatedAt:   time.Now(),
		Status:      "pending",
	}

	submission, err := coord.DelegateTask(ctx, "mock", task)
	if err != nil {
		t.Fatalf("DelegateTask failed: %v", err)
	}

	if submission.Status != "completed" {
		t.Errorf("Expected status 'completed', got '%s'", submission.Status)
	}

	// Verify task status was stored
	statuses := coord.GetTaskStatus(task.ID)
	if len(statuses) != 1 {
		t.Errorf("Expected 1 status, got %d", len(statuses))
	}
}

// TestCoordinatorAgentExecute tests the Execute method.
func TestCoordinatorAgentExecute(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	coord, err := NewCoordinatorAgent(settings)
	if err != nil {
		t.Fatalf("NewCoordinatorAgent failed: %v", err)
	}
	defer coord.Shutdown(context.Background())

	ctx := context.Background()

	t.Run("Direct task", func(t *testing.T) {
		task := &Task{
			ID:          "direct-task",
			Description: "Direct execution",
			CreatedAt:   time.Now(),
			Status:      "pending",
		}

		result, err := coord.Execute(ctx, task)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Error("Expected success for direct task")
		}
	})
}
