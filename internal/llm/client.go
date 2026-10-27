// Package llm provides OpenAI-compatible LLM client integration.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ToolCalls carries assistant tool-call requests (OpenAI format).
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID links a tool-result message back to its call.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// Tool represents a function/tool definition.
type Tool struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function,omitempty"`
}

// FunctionDefinition represents a function definition for tool calling.
type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ToolCall represents a tool call from the LLM.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall represents a function call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatRequest represents a chat completion request.
type ChatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Tools          []Tool          `json:"tools,omitempty"`
	ToolChoice     interface{}     `json:"tool_choice,omitempty"`
	Temperature    float64         `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	TopP           float64         `json:"top_p,omitempty"`
	Stream         bool            `json:"stream,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// ResponseFormat represents the response format.
type ResponseFormat struct {
	Type string `json:"type"`
}

// ChatResponse represents a chat completion response.
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a choice in the response.
type Choice struct {
	Index        int             `json:"index"`
	Message      ResponseMessage `json:"message"`
	Delta        *Delta          `json:"delta,omitempty"`
	FinishReason string          `json:"finish_reason,omitempty"`
	ToolCalls    []ToolCall      `json:"tool_calls,omitempty"`
}

// ResponseMessage represents a message in the response.
type ResponseMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Delta represents a delta in streaming mode.
type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Usage represents token usage information.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatChunk represents a chunk in streaming response.
type ChatChunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

// Config represents the LLM client configuration.
type Config struct {
	Provider    string
	BaseURL     string
	APIKey      string
	Model       string
	MaxTokens   int
	Temperature float64
	Timeout     time.Duration
}

// Client represents an OpenAI-compatible LLM client.
type Client struct {
	config     *Config
	httpClient *http.Client
}

// NewClient creates a new LLM client.
func NewClient(config *Config) *Client {
	if config == nil {
		config = &Config{}
	}

	if config.Timeout == 0 {
		config.Timeout = 120 * time.Second
	}

	return &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

// Chat sends a chat completion request.
func (c *Client) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	req := &ChatRequest{
		Model:       c.config.Model,
		Messages:    messages,
		MaxTokens:   c.config.MaxTokens,
		Temperature: c.config.Temperature,
	}

	return c.doChat(ctx, req, nil)
}

// ChatWithTools sends a chat completion request with tools.
func (c *Client) ChatWithTools(ctx context.Context, messages []Message, tools []Tool) (*ChatResponse, error) {
	req := &ChatRequest{
		Model:       c.config.Model,
		Messages:    messages,
		Tools:       tools,
		ToolChoice:  "auto",
		MaxTokens:   c.config.MaxTokens,
		Temperature: c.config.Temperature,
	}

	return c.doChat(ctx, req, nil)
}

// ChatStream sends a streaming chat completion request.
func (c *Client) ChatStream(ctx context.Context, messages []Message, handler func(ChatChunk)) error {
	req := &ChatRequest{
		Model:       c.config.Model,
		Messages:    messages,
		MaxTokens:   c.config.MaxTokens,
		Temperature: c.config.Temperature,
		Stream:      true,
	}

	_, err := c.doChat(ctx, req, handler)
	return err
}

// doChat performs the actual chat request.
func (c *Client) doChat(ctx context.Context, req *ChatRequest, streamHandler func(ChatChunk)) (*ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	url := fmt.Sprintf("%s/chat/completions", baseURL)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	// Send request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	// Handle streaming response
	if req.Stream {
		return c.handleStream(resp, streamHandler)
	}

	// Handle non-streaming response
	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &chatResp, nil
}

// handleStream handles streaming response.
func (c *Client) handleStream(resp *http.Response, handler func(ChatChunk)) (*ChatResponse, error) {
	scanner := bufio.NewScanner(resp.Body)

	var lastChunk *ChatChunk

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and non-data lines
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		// Remove "data: " prefix
		data := strings.TrimPrefix(line, "data: ")

		// Check for end of stream
		if data == "[DONE]" {
			break
		}

		// Parse chunk
		var chunk ChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		lastChunk = &chunk

		if handler != nil {
			handler(chunk)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading stream: %w", err)
	}

	// Return final response (reconstructed from chunks)
	if lastChunk != nil && len(lastChunk.Choices) > 0 {
		return &ChatResponse{
			ID:      lastChunk.ID,
			Object:  lastChunk.Object,
			Created: lastChunk.Created,
			Model:   lastChunk.Model,
			Choices: lastChunk.Choices,
		}, nil
	}

	return nil, fmt.Errorf("no valid response received")
}

// GetModels returns available models for the current provider by querying its
// OpenAI-compatible GET /models endpoint.
func (c *Client) GetModels(ctx context.Context) ([]string, error) {
	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	url := fmt.Sprintf("%s/models", baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Surface non-2xx responses instead of silently decoding an error body
	// into an empty model list.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("provider returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}

	return models, nil
}
