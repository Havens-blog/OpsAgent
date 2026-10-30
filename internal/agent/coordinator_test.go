// Package agent provides tests for CoordinatorAgent.
package agent

import (
	"context"
	"strings"
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
	err = coord.RegisterAgent("mock", mockAgent)
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

	err = coord.RegisterAgent("mock", mockAgent)
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

		if result.Success {
			t.Error("Expected failure for unroutable direct task (no agents, no LLM routing)")
		}
		if result.Error == "" {
			t.Error("Expected a non-empty error explaining the routing failure")
		}
		if !strings.Contains(result.Response, "无法将任务路由") {
			t.Errorf("Expected honest routing-failure message, got %q", result.Response)
		}
	})
}

// TestCoordinatorRouting exercises createExecutionPlan's three-tier routing:
// explicit type fast-path, LLM intent routing, and the direct fallback.
func TestCoordinatorRouting(t *testing.T) {
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}

	newCoord := func(t *testing.T, llmReply string) *CoordinatorAgent {
		t.Helper()
		coord, err := NewCoordinatorAgent(settings)
		if err != nil {
			t.Fatalf("NewCoordinatorAgent failed: %v", err)
		}
		// Register two specialists the router can choose between.
		for _, name := range []string{"monitor", "log_analyst"} {
			m, err := NewMockAgent(settings)
			if err != nil {
				t.Fatalf("NewMockAgent failed: %v", err)
			}
			if err := coord.RegisterAgent(name, m); err != nil {
				t.Fatalf("RegisterAgent(%s) failed: %v", name, err)
			}
		}
		// Deterministic LLM so routing never hits a real provider.
		coord.llmClient = &stubLLM{reply: llmReply}
		return coord
	}

	ctx := context.Background()

	t.Run("explicit type fast-path skips LLM", func(t *testing.T) {
		// A reply that WOULD route elsewhere if the LLM were consulted; the
		// explicit type must win and the LLM must not be used.
		coord := newCoord(t, `{"strategy":"sequential","agents":["log_analyst"]}`)
		defer coord.Shutdown(ctx)

		task := &Task{
			ID:          "t1",
			Description: "run RCA",
			Params:      map[string]interface{}{"type": "rca"},
			CreatedAt:   time.Now(),
			Status:      "pending",
		}
		plan, err := coord.createExecutionPlan(ctx, task)
		if err != nil {
			t.Fatalf("createExecutionPlan failed: %v", err)
		}
		if len(plan.Delegations) != 1 || plan.Delegations[0].AgentName != "monitor" {
			t.Fatalf("expected single delegation to monitor, got %+v", plan.Delegations)
		}
	})

	t.Run("LLM routes free-text to chosen agents (parallel)", func(t *testing.T) {
		coord := newCoord(t, `Here you go: {"strategy":"parallel","agents":["monitor","log_analyst"]}`)
		defer coord.Shutdown(ctx)

		task := &Task{
			ID:          "t2",
			Description: "errors spiking and logs look weird",
			CreatedAt:   time.Now(),
			Status:      "pending",
		}
		plan, err := coord.createExecutionPlan(ctx, task)
		if err != nil {
			t.Fatalf("createExecutionPlan failed: %v", err)
		}
		if plan.Strategy != "parallel" {
			t.Errorf("expected parallel strategy, got %q", plan.Strategy)
		}
		if len(plan.Delegations) != 2 {
			t.Fatalf("expected 2 delegations, got %d", len(plan.Delegations))
		}
	})

	t.Run("invalid/unknown agents are dropped", func(t *testing.T) {
		coord := newCoord(t, `{"strategy":"sequential","agents":["ghost","monitor","ghost"]}`)
		defer coord.Shutdown(ctx)

		task := &Task{
			ID:          "t3",
			Description: "investigate",
			CreatedAt:   time.Now(),
			Status:      "pending",
		}
		plan, err := coord.createExecutionPlan(ctx, task)
		if err != nil {
			t.Fatalf("createExecutionPlan failed: %v", err)
		}
		if len(plan.Delegations) != 1 || plan.Delegations[0].AgentName != "monitor" {
			t.Fatalf("expected only valid agent monitor, got %+v", plan.Delegations)
		}
	})

	t.Run("unparseable LLM reply falls back to direct", func(t *testing.T) {
		coord := newCoord(t, "I cannot help with that.")
		defer coord.Shutdown(ctx)

		task := &Task{
			ID:          "t4",
			Description: "something ambiguous",
			CreatedAt:   time.Now(),
			Status:      "pending",
		}
		plan, err := coord.createExecutionPlan(ctx, task)
		if err != nil {
			t.Fatalf("createExecutionPlan failed: %v", err)
		}
		if plan.Strategy != "direct" {
			t.Errorf("expected direct fallback, got %q", plan.Strategy)
		}
		if len(plan.Delegations) != 0 {
			t.Errorf("expected no delegations on fallback, got %d", len(plan.Delegations))
		}
	})

	t.Run("nil LLM client falls back to direct", func(t *testing.T) {
		coord := newCoord(t, "")
		defer coord.Shutdown(ctx)
		coord.llmClient = nil

		task := &Task{
			ID:          "t5",
			Description: "no llm available",
			CreatedAt:   time.Now(),
			Status:      "pending",
		}
		plan, err := coord.createExecutionPlan(ctx, task)
		if err != nil {
			t.Fatalf("createExecutionPlan failed: %v", err)
		}
		if plan.Strategy != "direct" {
			t.Errorf("expected direct fallback with nil LLM, got %q", plan.Strategy)
		}
	})
}
