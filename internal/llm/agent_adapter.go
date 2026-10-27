// Package llm provides an OpenAI-compatible adapter for the agent layer.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

// AgentLLM is the narrow interface the agent layer depends on. Both
// ClaudeClient (Anthropic protocol) and OpenAIAgentClient (OpenAI-compatible
// protocol) satisfy it, so BaseAgent can select a client by provider instead
// of being hard-wired to Anthropic.
type AgentLLM interface {
	Chat(ctx context.Context, messages []ClaudeMessage) (string, error)
	ChatWithTools(ctx context.Context, messages []ClaudeMessage, tools []ClaudeTool) (string, error)
}

// Compile-time checks that both clients satisfy the interface.
var _ AgentLLM = (*ClaudeClient)(nil)
var _ AgentLLM = (*OpenAIAgentClient)(nil)

// OpenAIAgentClient adapts the OpenAI-compatible Client to AgentLLM, including
// a tool-calling loop. This lets agents run against OpenAI-compatible providers
// (dashscope, deepseek, zhipu, moonshot, ...) rather than only Anthropic.
type OpenAIAgentClient struct {
	client        *Client
	maxIterations int
}

// NewOpenAIAgentClient creates an adapter around an OpenAI-compatible Client.
func NewOpenAIAgentClient(config *Config) (*OpenAIAgentClient, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if config.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}
	return &OpenAIAgentClient{
		client:        NewClient(config),
		maxIterations: 10,
	}, nil
}

// SetMaxIterations sets the maximum number of tool-calling iterations.
func (a *OpenAIAgentClient) SetMaxIterations(max int) {
	if max > 0 {
		a.maxIterations = max
	}
}

// Chat sends a plain chat completion and returns the assistant text.
func (a *OpenAIAgentClient) Chat(ctx context.Context, messages []ClaudeMessage) (string, error) {
	resp, err := a.client.Chat(ctx, toOpenAIMessages(messages))
	if err != nil {
		return "", err
	}
	return firstContent(resp), nil
}

// ChatWithTools runs the OpenAI tool-calling loop until the model returns a
// final text response or maxIterations is reached.
func (a *OpenAIAgentClient) ChatWithTools(ctx context.Context, messages []ClaudeMessage, tools []ClaudeTool) (string, error) {
	if len(tools) == 0 {
		return a.Chat(ctx, messages)
	}

	convo := toOpenAIMessages(messages)
	openAITools := make([]Tool, 0, len(tools))
	handlers := make(map[string]ToolHandler, len(tools))
	for _, t := range tools {
		openAITools = append(openAITools, Tool{
			Type: "function",
			Function: FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  toParamsMap(t.InputSchema),
			},
		})
		handlers[t.Name] = t.Handler
	}

	var finalText string
	for i := 0; i < a.maxIterations; i++ {
		resp, err := a.client.ChatWithTools(ctx, convo, openAITools)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 {
			return finalText, nil
		}
		msg := resp.Choices[0].Message
		if msg.Content != "" {
			finalText = msg.Content
		}
		// No tool calls -> this is the final answer.
		if len(msg.ToolCalls) == 0 {
			return finalText, nil
		}

		// Append the assistant message carrying the tool-call requests.
		convo = append(convo, Message{
			Role:      "assistant",
			Content:   msg.Content,
			ToolCalls: msg.ToolCalls,
		})

		// Execute each tool call and append its result as a tool message.
		for _, tc := range msg.ToolCalls {
			handler := handlers[tc.Function.Name]
			var result string
			if handler == nil {
				result = fmt.Sprintf("Error: no handler registered for tool %s", tc.Function.Name)
			} else {
				var input map[string]interface{}
				if tc.Function.Arguments != "" {
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
						input = map[string]interface{}{}
					}
				}
				r, herr := handler(ctx, input)
				if herr != nil {
					result = fmt.Sprintf("Error: %v", herr)
				} else {
					result = r
				}
			}
			convo = append(convo, Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}
	return finalText, nil
}

// toOpenAIMessages converts agent-layer ClaudeMessages to OpenAI messages.
// system/user/assistant roles pass through unchanged (OpenAI supports all).
func toOpenAIMessages(messages []ClaudeMessage) []Message {
	out := make([]Message, 0, len(messages))
	for _, m := range messages {
		out = append(out, Message{Role: m.Role, Content: m.Content})
	}
	return out
}

// toParamsMap coerces a tool input schema into a parameters map.
func toParamsMap(schema interface{}) map[string]interface{} {
	if m, ok := schema.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{"type": "object"}
}

// firstContent returns the content of the first choice, or "".
func firstContent(resp *ChatResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Message.Content
}
