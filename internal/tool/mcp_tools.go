// Package tool provides MCP tool integration for OpsAgent agents.
// This allows agents to use external MCP tools like web search.
package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/mcp"
)

// MCPBridge bridges MCP tools to OpsAgent tool system.
type MCPBridge struct {
	multiClient *mcp.MultiClient
	tools       map[string]Tool
}

// NewMCPBridge creates a new MCP bridge.
func NewMCPBridge(multiClient *mcp.MultiClient) *MCPBridge {
	return &MCPBridge{
		multiClient: multiClient,
		tools:       make(map[string]Tool),
	}
}

// RegisterTool registers an MCP tool as an OpsAgent tool.
func (b *MCPBridge) RegisterTool(server, toolName string, mcpTool mcp.Tool) error {
	opsTool := &Tool{
		Metadata: &ToolMetadata{
			Name:        fmt.Sprintf("%s_%s", server, toolName),
			Description: mcpTool.Description,
			Source:      DataSourceTypeMCP,
			InputSchema: mustMarshalJSON(mcpTool.InputSchema),
			Tier:        guardrails.TierSafe,     // MCP tools are safe by default
			SideEffect:  guardrails.SideEffectReadOnly, // Read-only side effects
			Category:    "mcp", // MCP category
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			resp, err := b.multiClient.CallTool(ctx, server, toolName, params)
			if err != nil {
				return nil, err
			}
			if resp.IsError {
				return nil, fmt.Errorf("MCP tool error: %v", resp.Content)
			}
			return resp.Content, nil
		},
		IsAvailable: func(config map[string]interface{}) bool {
			return true // MCP tools are always available if configured
		},
	}

	b.tools[opsTool.Metadata.Name] = *opsTool
	return nil
}

// GetTool returns a registered tool.
func (b *MCPBridge) GetTool(name string) (Tool, bool) {
	tool, ok := b.tools[name]
	return tool, ok
}

// ListTools returns all registered MCP tools.
func (b *MCPBridge) ListTools() []Tool {
	tools := make([]Tool, 0, len(b.tools))
	for _, tool := range b.tools {
		tools = append(tools, tool)
	}
	return tools
}

// SetupExternalTools sets up common external MCP tools.
func (b *MCPBridge) SetupExternalTools() error {
	// Exa Search tools (if available)
	exaTools := []struct {
		name string
		desc string
		schema map[string]interface{}
	}{
		{
			name: "web_search_exa",
			desc: "Search the web using Exa's neural search engine",
			schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
					"numResults": map[string]interface{}{
						"type":        "integer",
						"description": "Number of results",
						"default":     8,
					},
				},
				"required": []string{"query"},
			},
		},
		{
			name: "get_code_context_exa",
			desc: "Find code examples and documentation",
			schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Code or API search query",
					},
					"tokensNum": map[string]interface{}{
						"type":        "integer",
						"description": "Content tokens",
						"default":     5000,
					},
				},
				"required": []string{"query"},
			},
		},
	}

	for _, t := range exaTools {
		mcpTool := mcp.Tool{
			Name:        t.name,
			Description: t.desc,
			InputSchema: t.schema,
		}
		if err := b.RegisterTool("exa", t.name, mcpTool); err != nil {
			return err
		}
	}

	return nil
}

// mustMarshalJSON marshals to JSON or panics.
func mustMarshalJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// RegisterToRegistry registers all MCP tools to a tool Registry.
func (b *MCPBridge) RegisterToRegistry(registry *Registry) error {
	for name, tool := range b.tools {
		// Create a copy to avoid mutation
		t := tool
		toolPtr := &t

		// Set availability check if not set
		if toolPtr.IsAvailable == nil {
			toolPtr.IsAvailable = func(config map[string]interface{}) bool {
				return true
			}
		}

		if err := registry.Register(toolPtr); err != nil {
			return fmt.Errorf("failed to register MCP tool %s: %w", name, err)
		}
	}
	return nil
}

// SetupFromConfig sets up MCP clients from configuration.
func SetupFromConfig(config map[string]interface{}) (*MCPBridge, error) {
	// Create multi-client
	multiClient := mcp.NewMultiClient()

	// Parse MCP client configurations
	clientsConfig, ok := config["clients"].(map[string]interface{})
	if !ok {
		// No clients configured, return empty bridge
		return NewMCPBridge(multiClient), nil
	}

	for name, clientCfg := range clientsConfig {
		clientMap, ok := clientCfg.(map[string]interface{})
		if !ok {
			continue
		}

		baseURL, _ := clientMap["base_url"].(string)
		if baseURL == "" {
			continue
		}

		client := mcp.NewClient(baseURL)
		multiClient.Register(name, client)
	}

	// Create bridge and setup external tools
	bridge := NewMCPBridge(multiClient)
	if err := bridge.SetupExternalTools(); err != nil {
		return nil, fmt.Errorf("failed to setup external tools: %w", err)
	}

	return bridge, nil
}
