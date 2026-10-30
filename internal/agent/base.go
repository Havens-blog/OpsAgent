// Package agent provides the base agent implementation for OpsAgent.
package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/llm"
	"github.com/Havens-blog/OpsAgent/internal/observability"
	"github.com/Havens-blog/OpsAgent/internal/tool"
)

// AgentType represents the type of agent.
type AgentType string

const (
	// AgentTypeCoordinator represents the coordinator agent.
	AgentTypeCoordinator AgentType = "coordinator"
	// AgentTypeLogAnalyst represents the log analyst agent.
	AgentTypeLogAnalyst AgentType = "log_analyst"
	// AgentTypeMonitor represents the monitor agent.
	AgentTypeMonitor AgentType = "monitor"
	// AgentTypeInspector represents the inspector agent.
	AgentTypeInspector AgentType = "inspector"
)

// Message represents a message in the conversation.
type Message struct {
	Role      string                 `json:"role"`
	Content   string                 `json:"content"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Conversation represents a conversation with the agent.
type Conversation struct {
	ID        string    `json:"id"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	mu        sync.RWMutex
}

// AddMessage adds a message to the conversation.
func (c *Conversation) AddMessage(msg Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Messages = append(c.Messages, msg)
	c.UpdatedAt = time.Now()
}

// GetMessages returns all messages in the conversation.
func (c *Conversation) GetMessages() []Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Messages
}

// Task represents a task to be executed by the agent.
type Task struct {
	ID          string                 `json:"id"`
	Description string                 `json:"description"`
	Params      map[string]interface{} `json:"params"`
	CreatedAt   time.Time              `json:"created_at"`
	Status      string                 `json:"status"`
	Result      interface{}            `json:"result,omitempty"`
	Error       error                  `json:"error,omitempty"`
}

// ExecutionResult represents the result of an agent execution.
type ExecutionResult struct {
	Success   bool                   `json:"success"`
	Response  string                 `json:"response"`
	Data      map[string]interface{} `json:"data,omitempty"`
	ToolsUsed []string               `json:"tools_used,omitempty"`
	Error     error                  `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Duration  time.Duration          `json:"duration"`
}

// AgentConfig holds the configuration for an agent.
type AgentConfig struct {
	Name             string        `json:"name"`
	Type             AgentType     `json:"type"`
	Description      string        `json:"description"`
	Model            string        `json:"model"`
	MaxTurns         int           `json:"max_turns"`
	Timeout          time.Duration `json:"timeout"`
	SystemPrompt     string        `json:"system_prompt"`
	AllowedTools     []string      `json:"allowed_tools"`
	RequiresApproval bool          `json:"requires_approval"`
	Capabilities     []string      `json:"capabilities"`
}

// BaseAgent provides the base implementation for all agents.
// It encapsulates the Claude Agent SDK and provides common functionality.
type BaseAgent struct {
	config       *AgentConfig
	settings     *config.Settings
	toolRegistry *tool.Registry
	approvalMgr  *guardrails.ApprovalManager
	logger       *observability.Logger
	auditor      *observability.AuditLogger
	conversation *Conversation
	llmClient    llm.AgentLLM

	mu sync.RWMutex
}

// NewBaseAgent creates a new base agent.
func NewBaseAgent(cfg *AgentConfig, settings *config.Settings) (*BaseAgent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("agent config is required")
	}
	if settings == nil {
		return nil, fmt.Errorf("settings is required")
	}

	// Create logger
	logger, err := observability.NewLogger(cfg.Name, settings.Observability.LogLevel, observability.FormatJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Create audit logger if enabled
	var auditor *observability.AuditLogger
	if settings.Security.EnableAuditLog {
		writer, err := observability.NewFileAuditWriter(settings.Security.AuditLogPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create audit writer: %w", err)
		}
		auditor = observability.NewAuditLogger(writer, true)
	}

	// Create tool registry
	toolRegistry := tool.NewRegistry()

	// Create approval manager
	approvalMgr := guardrails.NewApprovalManager(nil)

	// Create conversation
	conversation := &Conversation{
		ID:        generateConversationID(),
		Messages:  make([]Message, 0),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Add system prompt if provided
	if cfg.SystemPrompt != "" {
		conversation.AddMessage(Message{
			Role:      "system",
			Content:   cfg.SystemPrompt,
			Timestamp: time.Now(),
		})
	}

	// Create LLM client.
	// Fall back to the legacy Agent.AnthropicAPIKey when the unified
	// LLM.APIKey is unset — mirrors the migration in Settings.Validate().
	apiKey := settings.LLM.APIKey
	if apiKey == "" {
		apiKey = settings.Agent.AnthropicAPIKey
	}

	// Resolve the effective model. The agent configs hard-code a Claude model
	// name, which is meaningless for OpenAI-compatible providers (dashscope,
	// deepseek, ...). For those, use the configured LLM.Model instead.
	model := cfg.Model
	if llm.IsOpenAICompatible(settings.LLM.Provider) {
		if settings.LLM.Model != "" {
			model = settings.LLM.Model
		} else if p, ok := llm.GetProvider(settings.LLM.Provider); ok {
			model = p.DefaultModel
		}
		cfg.Model = model // keep reported model accurate
	}

	maxTokens := settings.LLM.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	llmConfig := &llm.Config{
		Provider:    settings.LLM.Provider,
		BaseURL:     settings.LLM.BaseURL,
		APIKey:      apiKey,
		Model:       model,
		MaxTokens:   maxTokens,
		Temperature: settings.LLM.Temperature,
		Timeout:     120 * time.Second,
	}

	// Select the client implementation by provider protocol:
	//   - OpenAI-compatible providers -> OpenAIAgentClient (chat/completions)
	//   - claude / anthropic          -> ClaudeClient (Anthropic Messages API)
	var llmClient llm.AgentLLM
	if llm.IsOpenAICompatible(settings.LLM.Provider) {
		llmClient, err = llm.NewOpenAIAgentClient(llmConfig)
	} else {
		llmClient, err = llm.NewClaudeClient(llmConfig)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	agent := &BaseAgent{
		config:       cfg,
		settings:     settings,
		toolRegistry: toolRegistry,
		approvalMgr:  approvalMgr,
		logger:       logger,
		auditor:      auditor,
		conversation: conversation,
		llmClient:    llmClient,
	}

	return agent, nil
}

// Initialize initializes the agent with the given tools.
func (a *BaseAgent) Initialize(tools []*tool.Tool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, t := range tools {
		if err := a.toolRegistry.Register(t); err != nil {
			return fmt.Errorf("failed to register tool %s: %w", t.Metadata.Name, err)
		}
		a.logger.Info("Registered tool",
			observability.String("tool", t.Metadata.Name),
			observability.String("category", t.Metadata.Category),
		)
	}

	a.logger.Info("Agent initialized",
		observability.String("agent", a.config.Name),
		observability.String("type", string(a.config.Type)),
		observability.Int("tools", len(tools)),
	)

	return nil
}

// Execute executes the agent with the given task.
func (a *BaseAgent) Execute(ctx context.Context, task *Task) (*ExecutionResult, error) {
	startTime := time.Now()

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && a.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.config.Timeout)
		defer cancel()
	}

	// Add user message to conversation
	a.conversation.AddMessage(Message{
		Role:      "user",
		Content:   task.Description,
		Timestamp: time.Now(),
	})

	// Log execution start
	a.logger.Info("Agent execution started",
		observability.String("agent", a.config.Name),
		observability.String("task_id", task.ID),
		observability.String("type", string(a.config.Type)),
	)

	// Audit log start
	if a.auditor != nil {
		_ = a.auditor.LogAgentStart(ctx, a.conversation.ID, a.config.Name, task.Params)
	}

	// Execute the task (this will be implemented by subclasses)
	result, err := a.executeTask(ctx, task)

	duration := time.Since(startTime)
	result.Duration = duration

	// Log execution end
	a.logger.Info("Agent execution completed",
		observability.String("agent", a.config.Name),
		observability.String("task_id", task.ID),
		observability.Bool("success", result.Success),
		observability.String("duration", duration.String()),
	)

	// Audit log end
	if a.auditor != nil {
		_ = a.auditor.LogAgentStop(ctx, a.conversation.ID, a.config.Name, duration, nil)
	}

	// Add assistant message to conversation
	if result.Response != "" {
		a.conversation.AddMessage(Message{
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

// ExecuteTool executes a specific tool with the given parameters.
func (a *BaseAgent) ExecuteTool(ctx context.Context, toolName string, params map[string]interface{}) (interface{}, error) {
	// Get the tool
	t, err := a.toolRegistry.Get(toolName)
	if err != nil {
		return nil, fmt.Errorf("tool not found: %s", toolName)
	}

	// Check if approval is required
	if t.Metadata.RequiresConfirmation() {
		// Try session-level approval first
		sessionReq := guardrails.ApprovalRequest{
			ToolName:  toolName,
			Operation: "execute",
			Params:    params,
			Reason:    t.Metadata.ApprovalReason,
			Scope:     guardrails.ScopeSession,
			SessionID: a.conversation.ID,
		}
		if err := a.approvalMgr.RequireApproval(sessionReq); err != nil {
			// Session-level approval not found, try one-shot
			oneShotReq := sessionReq
			oneShotReq.Scope = guardrails.ScopeOneShot
			if err := a.approvalMgr.RequireApproval(oneShotReq); err != nil {
				return nil, fmt.Errorf("approval required for tool %s: %w", toolName, err)
			}
		}
	}

	// Log tool execution
	a.logger.Debug("Executing tool",
		observability.String("tool", toolName),
		observability.Any("params", params),
	)

	// Audit log tool execution
	if a.auditor != nil {
		_ = a.auditor.LogToolCall(ctx, a.conversation.ID, a.config.Name, toolName, params)
	}

	// Get data source config
	dsConfig, _ := a.settings.GetDataSourceConfig(string(t.Metadata.Source))

	// Execute the tool
	result, err := t.Execute(ctx, params, dsConfig)
	if err != nil {
		a.logger.Error("Tool execution failed",
			observability.String("tool", toolName),
			observability.Err(err),
		)
		return nil, err
	}

	// Audit log tool result
	if a.auditor != nil {
		_ = a.auditor.LogToolResult(ctx, a.conversation.ID, a.config.Name, toolName, 0, result, nil)
	}

	return result, nil
}

// RequestApproval requests approval for a tool execution.
func (a *BaseAgent) RequestApproval(toolName, operation string, params map[string]interface{}, reason string) error {
	req := guardrails.ApprovalRequest{
		ToolName:      toolName,
		Operation:     operation,
		Params:        params,
		Reason:        reason,
		ExpirySeconds: 300, // 5 minutes default
		Scope:         guardrails.ScopeSession,
		SessionID:     a.conversation.ID,
	}

	return a.approvalMgr.RequireApproval(req)
}

// GrantApproval grants approval for a tool execution.
func (a *BaseAgent) GrantApproval(toolName string, scope guardrails.ApprovalScope) error {
	return a.approvalMgr.GrantApproval(toolName, scope, a.conversation.ID, 300)
}

// GetConversation returns the current conversation.
func (a *BaseAgent) GetConversation() *Conversation {
	return a.conversation
}

// GetConversationID returns the conversation ID.
func (a *BaseAgent) GetConversationID() string {
	return a.conversation.ID
}

// GetConfig returns the agent configuration.
func (a *BaseAgent) GetConfig() *AgentConfig {
	return a.config
}

// GetAvailableTools returns the list of available tools for this agent.
func (a *BaseAgent) GetAvailableTools() []string {
	if len(a.config.AllowedTools) > 0 {
		return a.config.AllowedTools
	}
	return a.toolRegistry.List()
}

// GetToolSpecsForLLM returns tool specifications formatted for LLM consumption.
func (a *BaseAgent) GetToolSpecsForLLM() ([]map[string]interface{}, error) {
	dsConfig := a.settings.DataSources
	return a.toolRegistry.GetToolSpecsForLLM(dsConfig)
}

// HasCapability checks if the agent has a specific capability.
func (a *BaseAgent) HasCapability(capability string) bool {
	for _, cap := range a.config.Capabilities {
		if cap == capability {
			return true
		}
	}
	return false
}

// ResetConversation resets the conversation, keeping the system prompt.
func (a *BaseAgent) ResetConversation() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Create new conversation
	newConv := &Conversation{
		ID:        generateConversationID(),
		Messages:  make([]Message, 0),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Add system prompt if it existed
	if len(a.conversation.Messages) > 0 && a.conversation.Messages[0].Role == "system" {
		newConv.AddMessage(a.conversation.Messages[0])
	}

	a.conversation = newConv

	a.logger.Info("Conversation reset",
		observability.String("agent", a.config.Name),
		observability.String("new_conversation_id", newConv.ID),
	)

	return nil
}

// Shutdown gracefully shuts down the agent.
func (a *BaseAgent) Shutdown(ctx context.Context) error {
	a.logger.Info("Agent shutting down",
		observability.String("agent", a.config.Name),
	)

	// Close audit logger if present
	if a.auditor != nil {
		if err := a.auditor.Close(); err != nil {
			return fmt.Errorf("failed to close audit logger: %w", err)
		}
	}

	// Sync logger
	if a.logger != nil {
		_ = a.logger.Sync()
	}

	return nil
}

// executeTask is the internal implementation of task execution.
// This is the REAL implementation using Claude SDK.
func (a *BaseAgent) executeTask(ctx context.Context, task *Task) (*ExecutionResult, error) {
	// Convert conversation messages to Claude format
	messages := make([]llm.ClaudeMessage, 0, len(a.conversation.Messages))
	for _, msg := range a.conversation.Messages {
		messages = append(messages, llm.ClaudeMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Prepare tools if available
	var tools []llm.ClaudeTool
	if len(a.toolRegistry.List()) > 0 {
		toolSpecs, _ := a.GetToolSpecsForLLM()
		tools = make([]llm.ClaudeTool, 0, len(toolSpecs))

		for _, spec := range toolSpecs {
			toolName, _ := spec["name"].(string)
			toolDesc, _ := spec["description"].(string)
			// GetToolSpecsForLLM exposes the schema under "input_schema";
			// "parameters" does not exist and would leave InputSchema nil.
			inputSchema := spec["input_schema"]

			// Create tool handler
			handler := func(ctx context.Context, input map[string]interface{}) (string, error) {
				result, err := a.ExecuteTool(ctx, toolName, input)
				if err != nil {
					return "", err
				}
				// Convert result to string
				return fmt.Sprintf("%v", result), nil
			}

			tools = append(tools, llm.ClaudeTool{
				Name:        toolName,
				Description: toolDesc,
				InputSchema: inputSchema,
				Handler:     handler,
			})
		}
	}

	// Call Claude with or without tools
	var responseText string
	var toolsUsed []string
	var err error

	if len(tools) > 0 {
		responseText, toolsUsed, err = a.llmClient.ChatWithTools(ctx, messages, tools)
	} else {
		responseText, err = a.llmClient.Chat(ctx, messages)
	}

	if err != nil {
		return &ExecutionResult{
			Success:   false,
			Error:     err,
			Response:  fmt.Sprintf("LLM call failed: %v", err),
			Data:      make(map[string]interface{}),
			ToolsUsed: make([]string, 0),
			Metadata:  make(map[string]interface{}),
		}, nil
	}

	if toolsUsed == nil {
		toolsUsed = []string{}
	}

	return &ExecutionResult{
		Success:  true,
		Response: responseText,
		Data: map[string]interface{}{
			"model":           a.config.Model,
			"tools_available": len(a.toolRegistry.List()),
			"provider":        a.settings.LLM.Provider,
		},
		ToolsUsed: toolsUsed,
		Metadata:  make(map[string]interface{}),
	}, nil
}

// conversationIDCounter is used to ensure unique conversation IDs.
var conversationIDCounter uint64

// generateConversationID generates a unique conversation ID using
// a combination of timestamp and atomic counter to guarantee uniqueness.
func generateConversationID() string {
	// Use atomic add to get a unique sequence number
	seq := atomic.AddUint64(&conversationIDCounter, 1)
	return fmt.Sprintf("conv-%d-%d", time.Now().UnixNano(), seq)
}
