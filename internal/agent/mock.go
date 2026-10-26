// Package agent provides mock implementations for testing.
package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/observability"
	"github.com/Havens-blog/OpsAgent/internal/tool"
)

// MockAgent is a mock implementation for testing BaseAgent functionality.
type MockAgent struct {
	*BaseAgent
	executeCount int
	mu           sync.Mutex
}

// NewMockAgent creates a new mock agent for testing.
func NewMockAgent(settings *config.Settings) (*MockAgent, error) {
	cfg := &AgentConfig{
		Name:         "mock-agent",
		Type:         AgentTypeCoordinator,
		Description:  "Mock agent for testing",
		Model:        "claude-sonnet-4-20250514",
		MaxTurns:     5,
		Timeout:      30,
		SystemPrompt: "You are a helpful mock agent for testing purposes.",
		Capabilities: []string{"test", "mock"},
	}

	base, err := NewBaseAgent(cfg, settings)
	if err != nil {
		return nil, err
	}

	return &MockAgent{
		BaseAgent: base,
	}, nil
}

// executeTask implements the actual task execution for the mock agent.
// This is a separate method to be called by the overridden Execute method.
func (m *MockAgent) executeTask(ctx context.Context, task *Task) (*ExecutionResult, error) {
	m.mu.Lock()
	m.executeCount++
	count := m.executeCount
	m.mu.Unlock()

	// Simulate work
	result := &ExecutionResult{
		Success:  true,
		Response: fmt.Sprintf("Mock execution #%d: Task '%s' completed successfully", count, task.Description),
		Data: map[string]interface{}{
			"task_id":    task.ID,
			"exec_count": count,
			"agent_type": string(m.GetConfig().Type),
		},
		ToolsUsed: []string{},
		Metadata: map[string]interface{}{
			"mock": true,
		},
	}

	// Log execution
	m.logger.Info("Mock task executed",
		observability.Int("count", count),
		observability.String("task_id", task.ID),
	)

	return result, nil
}

// Execute overrides BaseAgent.Execute to properly track execution count.
func (m *MockAgent) Execute(ctx context.Context, task *Task) (*ExecutionResult, error) {
	startTime := time.Now()

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && m.BaseAgent.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.BaseAgent.config.Timeout)
		defer cancel()
	}

	// Add user message to conversation
	m.BaseAgent.conversation.AddMessage(Message{
		Role:      "user",
		Content:   task.Description,
		Timestamp: time.Now(),
	})

	// Log execution start
	m.BaseAgent.logger.Info("Agent execution started",
		observability.String("agent", m.BaseAgent.config.Name),
		observability.String("task_id", task.ID),
		observability.String("type", string(m.BaseAgent.config.Type)),
	)

	// Audit log start
	if m.BaseAgent.auditor != nil {
		_ = m.BaseAgent.auditor.LogAgentStart(ctx, m.BaseAgent.conversation.ID, m.BaseAgent.config.Name, task.Params)
	}

	// Execute the task using MockAgent's executeTask
	result, err := m.executeTask(ctx, task)

	duration := time.Since(startTime)
	result.Duration = duration

	// Log execution end
	m.BaseAgent.logger.Info("Agent execution completed",
		observability.String("agent", m.BaseAgent.config.Name),
		observability.String("task_id", task.ID),
		observability.Bool("success", result.Success),
		observability.String("duration", duration.String()),
	)

	// Audit log end
	if m.BaseAgent.auditor != nil {
		_ = m.BaseAgent.auditor.LogAgentStop(ctx, m.BaseAgent.conversation.ID, m.BaseAgent.config.Name, duration, nil)
	}

	// Add assistant message to conversation
	if result.Response != "" {
		m.BaseAgent.conversation.AddMessage(Message{
			Role:      "assistant",
			Content:   result.Response,
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"success":  result.Success,
				"duration": duration.String(),
			},
		})
	}

	return result, err
}

// GetExecuteCount returns the number of times execute was called.
func (m *MockAgent) GetExecuteCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.executeCount
}

// ResetCount resets the execution count.
func (m *MockAgent) ResetCount() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executeCount = 0
}

// MockTool is a simple mock tool for testing.
type MockTool struct {
	name        string
	description string
	callCount   int
	mu          sync.Mutex
}

// NewMockTool creates a new mock tool.
func NewMockTool(name, description string) *tool.Tool {
	mt := &MockTool{
		name:        name,
		description: description,
	}

	// Create a simple input schema
	inputSchema := `{
		"type": "object",
		"properties": {
			"input": {"type": "string", "description": "Input parameter"}
		}
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        name,
			Description: description,
			Source:      tool.DataSourceTypeGeneric,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "test",
		},
		Run: mt.run,
		IsAvailable: func(config map[string]interface{}) bool {
			// Mock tools are always available
			return true
		},
	}
}

func (mt *MockTool) run(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	mt.mu.Lock()
	mt.callCount++
	count := mt.callCount
	mt.mu.Unlock()

	return map[string]interface{}{
		"tool":       mt.name,
		"call_count": count,
		"params":     params,
		"result":     "success",
	}, nil
}

// GetCallCount returns the number of times the tool was called.
func (mt *MockTool) GetCallCount() int {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	return mt.callCount
}
