// Package mcp provides MCP (Model Context Protocol) client for OpsAgent.
// This allows OpsAgent to call external MCP tools like exa-search.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client represents an MCP client.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new MCP client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Tool represents an available tool from an MCP server.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ToolsResponse represents the response from listing tools.
type ToolsResponse struct {
	Tools []Tool `json:"tools"`
}

// ListTools lists available tools from the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	url := fmt.Sprintf("%s/mcp/tools", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list tools (status %d): %s", resp.StatusCode, string(body))
	}

	var toolsResp ToolsResponse
	if err := json.NewDecoder(resp.Body).Decode(&toolsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return toolsResp.Tools, nil
}

// CallToolRequest represents a tool call request.
type CallToolRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// CallTool calls a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (*ToolResponse, error) {
	url := fmt.Sprintf("%s/mcp/call", c.baseURL)

	reqBody := CallToolRequest{
		Name:      toolName,
		Arguments: args,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call tool: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to call tool (status %d): %s", resp.StatusCode, string(body))
	}

	var toolResp ToolResponse
	if err := json.NewDecoder(resp.Body).Decode(&toolResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &toolResp, nil
}

// MultiClient manages multiple MCP clients.
type MultiClient struct {
	clients map[string]*Client
}

// NewMultiClient creates a new multi-client.
func NewMultiClient() *MultiClient {
	return &MultiClient{
		clients: make(map[string]*Client),
	}
}

// Register registers an MCP client.
func (m *MultiClient) Register(name string, client *Client) {
	m.clients[name] = client
}

// CallTool calls a tool on a specific MCP server.
func (m *MultiClient) CallTool(ctx context.Context, server, toolName string, args map[string]interface{}) (*ToolResponse, error) {
	client, ok := m.clients[server]
	if !ok {
		return nil, fmt.Errorf("unknown MCP server: %s", server)
	}

	return client.CallTool(ctx, toolName, args)
}

// ListAllTools lists tools from all registered servers.
func (m *MultiClient) ListAllTools(ctx context.Context) (map[string][]Tool, error) {
	result := make(map[string][]Tool)

	for name, client := range m.clients {
		tools, err := client.ListTools(ctx)
		if err != nil {
			continue
		}
		result[name] = tools
	}

	return result, nil
}
