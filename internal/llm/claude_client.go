// Package llm provides Claude SDK integration for OpsAgent.
package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// ClaudeClient represents the official Anthropic Claude client.
type ClaudeClient struct {
	client        anthropic.Client
	config        *Config
	maxIterations int
}

// ClaudeMessage represents a message in the conversation.
type ClaudeMessage struct {
	Role    string                 `json:"role"`
	Content string                 `json:"content"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ClaudeTool represents a tool definition for Claude.
type ClaudeTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"`
	Handler     ToolHandler `json:"-"`
}

// ToolHandler is a function that handles tool execution.
type ToolHandler func(ctx context.Context, input map[string]interface{}) (string, error)

// NewClaudeClient creates a new Claude client using the official SDK.
func NewClaudeClient(config *Config) (*ClaudeClient, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if config.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	// Build client options
	opts := []option.RequestOption{
		option.WithAPIKey(config.APIKey),
	}

	// Set base URL if custom endpoint provided
	if config.BaseURL != "" && config.BaseURL != "https://api.anthropic.com" {
		opts = append(opts, option.WithBaseURL(config.BaseURL))
	}

	// Create client
	client := anthropic.NewClient(opts...)

	return &ClaudeClient{
		client:        client,
		config:        config,
		maxIterations: 10,
	}, nil
}

// Chat sends a chat completion request to Claude.
func (c *ClaudeClient) Chat(ctx context.Context, messages []ClaudeMessage) (string, error) {
	return c.ChatWithTools(ctx, messages, nil)
}

// ChatWithTools sends a chat completion request with tools to Claude.
func (c *ClaudeClient) ChatWithTools(ctx context.Context, messages []ClaudeMessage, tools []ClaudeTool) (string, error) {
	// Convert messages to Anthropic format
	anthropicMessages, err := c.convertMessages(messages)
	if err != nil {
		return "", fmt.Errorf("failed to convert messages: %w", err)
	}

	// Convert tools to Anthropic format
	var toolParams []anthropic.ToolParam
	if len(tools) > 0 {
		toolParams = make([]anthropic.ToolParam, len(tools))
		for i, tool := range tools {
			props := c.getProperties(tool.InputSchema)
			required := c.getRequired(tool.InputSchema)

			inputSchema := anthropic.ToolInputSchemaParam{
				Type:       "object",
				Properties: props,
			}
			if len(required) > 0 {
				inputSchema.Required = required
			}

			toolParams[i] = anthropic.ToolParam{
				Name:        tool.Name,
				Description: anthropic.String(tool.Description),
				InputSchema: inputSchema,
			}
		}
	}

	// Tool execution loop
	currentMessages := anthropicMessages
	var finalText string

	for iteration := 0; iteration < c.maxIterations; iteration++ {
		// Build message parameters
		params := anthropic.MessageNewParams{
			Model:     c.getModelParam(),
			MaxTokens: int64(c.getMaxTokens()),
			Messages:  currentMessages,
		}

		// Add tools if provided
		if len(toolParams) > 0 {
			toolsUnion := make([]anthropic.ToolUnionParam, len(toolParams))
			for i, tp := range toolParams {
				toolsUnion[i] = anthropic.ToolUnionParam{OfTool: &tp}
			}
			params.Tools = toolsUnion
		}

		// Send message
		message, err := c.client.Messages.New(ctx, params)
		if err != nil {
			return "", fmt.Errorf("failed to send message: %w", err)
		}

		// Extract text and tool use from response
		var toolUseBlocks []anthropic.ToolUseBlock
		for _, block := range message.Content {
			// Check block type and extract content accordingly
			if block.Type == "text" {
				finalText += block.Text
			} else if block.Type == "tool_use" {
				// Create ToolUseBlock from ContentBlockUnion
				toolUseBlock := anthropic.ToolUseBlock{
					/* Type is auto-filled */
					ID:    block.ID,
					Name:  block.Name,
					Input: block.Input,
				}
				toolUseBlocks = append(toolUseBlocks, toolUseBlock)
			}
		}

		// No more tool calls, return the response
		if len(toolUseBlocks) == 0 {
			return finalText, nil
		}

		// Execute tools and build results
		toolResults := make([]anthropic.ContentBlockParamUnion, 0, len(toolUseBlocks))
		for _, toolBlock := range toolUseBlocks {
			// Find the tool handler
			var handler ToolHandler
			for _, tool := range tools {
				if tool.Name == toolBlock.Name {
					handler = tool.Handler
					break
				}
			}

			if handler == nil {
				// No handler found, create error result
				result := fmt.Sprintf("Error: No handler found for tool %s", toolBlock.Name)
				toolResults = append(toolResults, anthropic.NewToolResultBlock(toolBlock.ID, result, false))
				continue
			}

			// Parse tool input
			var inputMap map[string]interface{}
			if err := json.Unmarshal(toolBlock.Input, &inputMap); err != nil {
				result := fmt.Sprintf("Error parsing input: %v", err)
				toolResults = append(toolResults, anthropic.NewToolResultBlock(toolBlock.ID, result, false))
				continue
			}

			// Execute the tool
			result, err := handler(ctx, inputMap)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}

			// Create tool result block
			toolResults = append(toolResults, anthropic.NewToolResultBlock(toolBlock.ID, result, false))
		}

		// Add assistant message and user message with tool results
		currentMessages = append(currentMessages, message.ToParam())
		currentMessages = append(currentMessages, anthropic.NewUserMessage(toolResults...))
	}

	return finalText, nil
}

// ChatStream sends a streaming chat completion request.
// Note: Currently implemented as non-streaming for simplicity.
func (c *ClaudeClient) ChatStream(ctx context.Context, messages []ClaudeMessage, handler func(string)) error {
	// For now, use non-streaming and call handler with full response
	response, err := c.Chat(ctx, messages)
	if err != nil {
		return err
	}

	handler(response)
	return nil
}

// GetModels returns available Claude models.
func (c *ClaudeClient) GetModels(ctx context.Context) ([]string, error) {
	return []string{
		"claude-sonnet-4-20250514",
		"claude-opus-4-20250514",
		"claude-haiku-4-20250501",
	}, nil
}

// convertMessages converts our message format to Anthropic format.
func (c *ClaudeClient) convertMessages(messages []ClaudeMessage) ([]anthropic.MessageParam, error) {
	result := make([]anthropic.MessageParam, 0, len(messages))

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			result = append(result, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
		case "assistant":
			result = append(result, anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)))
		case "system":
			// System messages are handled separately, skip for now
			continue
		default:
			return nil, fmt.Errorf("unknown role: %s", msg.Role)
		}
	}

	return result, nil
}

// getProperties extracts properties from input schema.
func (c *ClaudeClient) getProperties(schema interface{}) map[string]any {
	if m, ok := schema.(map[string]interface{}); ok {
		if props, ok := m["properties"].(map[string]interface{}); ok {
			return props
		}
	}
	return make(map[string]any)
}

// getRequired extracts required fields from input schema.
func (c *ClaudeClient) getRequired(schema interface{}) []string {
	if m, ok := schema.(map[string]interface{}); ok {
		if required, ok := m["required"].([]interface{}); ok {
			result := make([]string, len(required))
			for i, r := range required {
				if s, ok := r.(string); ok {
					result[i] = s
				}
			}
			return result
		}
	}
	return nil
}

// getModelParam returns the model parameter for API requests.
func (c *ClaudeClient) getModelParam() string {
	model := c.config.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return model
}

// getMaxTokens returns the configured max tokens or default.
func (c *ClaudeClient) getMaxTokens() int {
	if c.config.MaxTokens > 0 {
		return c.config.MaxTokens
	}
	return 4096
}

// SetMaxIterations sets the maximum number of tool iterations.
func (c *ClaudeClient) SetMaxIterations(max int) {
	c.maxIterations = max
}
