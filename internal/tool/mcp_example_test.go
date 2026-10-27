// Package tool provides MCP integration tests.
package tool

import (
	"context"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/mcp"
)

// TestMCPBridgeSetupExternalTools tests external tool setup.
func TestMCPBridgeSetupExternalTools(t *testing.T) {
	// Create multi-client
	multiClient := mcp.NewMultiClient()

	// Create bridge
	bridge := NewMCPBridge(multiClient)

	// Setup external tools
	if err := bridge.SetupExternalTools(); err != nil {
		t.Fatalf("SetupExternalTools failed: %v", err)
	}

	// Verify tools are registered
	tools := bridge.ListTools()
	if len(tools) == 0 {
		t.Fatal("No tools registered after SetupExternalTools")
	}

	// Verify expected tools are present
	expectedTools := []string{
		"exa_web_search_exa",
		"exa_get_code_context_exa",
	}

	for _, expected := range expectedTools {
		found := false
		for _, tool := range tools {
			if tool.Metadata.Name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected tool %s not found", expected)
		}
	}
}

// TestMCPBridgeRegisterToRegistry tests tool registration.
func TestMCPBridgeRegisterToRegistry(t *testing.T) {
	// Create multi-client
	multiClient := mcp.NewMultiClient()

	// Create bridge
	bridge := NewMCPBridge(multiClient)

	// Setup external tools
	if err := bridge.SetupExternalTools(); err != nil {
		t.Fatalf("SetupExternalTools failed: %v", err)
	}

	// Create registry
	registry := NewRegistry()

	// Register tools to registry
	if err := bridge.RegisterToRegistry(registry); err != nil {
		t.Fatalf("RegisterToRegistry failed: %v", err)
	}

	// Verify tools are in registry
	allTools := registry.List()
	if len(allTools) == 0 {
		t.Fatal("No tools in registry after registration")
	}

	// Verify a specific tool can be retrieved
	tool, err := registry.Get("exa_web_search_exa")
	if err != nil {
		t.Errorf("Failed to get tool: %v", err)
	}
	if tool == nil {
		t.Fatal("Tool is nil")
	}

	// Verify tool metadata
	if tool.Metadata.Source != "mcp" {
		t.Errorf("Expected source 'mcp', got '%s'", tool.Metadata.Source)
	}
}

// TestMCPToolExecution tests MCP tool execution (mock).
func TestMCPToolExecution(t *testing.T) {
	// This test would require a running MCP server
	// For now, just verify the tool structure

	multiClient := mcp.NewMultiClient()
	bridge := NewMCPBridge(multiClient)

	// Setup external tools
	if err := bridge.SetupExternalTools(); err != nil {
		t.Fatalf("SetupExternalTools failed: %v", err)
	}

	// Get a tool
	tools := bridge.ListTools()
	if len(tools) == 0 {
		t.Fatal("No tools available")
	}

	tool := tools[0]

	// Verify tool has required fields
	if tool.Metadata.Name == "" {
		t.Error("Tool name is empty")
	}
	if tool.Metadata.Description == "" {
		t.Error("Tool description is empty")
	}
	if tool.Run == nil {
		t.Error("Tool Run function is nil")
	}
	if tool.IsAvailable == nil {
		t.Error("Tool IsAvailable function is nil")
	}
}

// TestSetupFromConfig tests configuration-based setup.
func TestSetupFromConfig(t *testing.T) {
	// Test with empty config
	bridge, err := SetupFromConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("SetupFromConfig with empty config failed: %v", err)
	}
	if bridge == nil {
		t.Fatal("Bridge is nil")
	}

	// Test with clients config
	config := map[string]interface{}{
		"clients": map[string]interface{}{
			"exa": map[string]interface{}{
				"base_url": "http://localhost:3000",
			},
		},
	}

	bridge, err = SetupFromConfig(config)
	if err != nil {
		t.Fatalf("SetupFromConfig with clients failed: %v", err)
	}
	if bridge == nil {
		t.Fatal("Bridge is nil")
	}
}

// TestMCPToolCallMock tests tool call with mock client.
func TestMCPToolCallMock(t *testing.T) {
	// Create a mock MCP server behavior
	// This would typically be tested with an actual MCP server
	_ = context.Background()

	// For now, test that the tool structure allows execution
	multiClient := mcp.NewMultiClient()
	bridge := NewMCPBridge(multiClient)

	if err := bridge.SetupExternalTools(); err != nil {
		t.Fatalf("SetupExternalTools failed: %v", err)
	}

	tools := bridge.ListTools()
	if len(tools) == 0 {
		t.Skip("No tools to test")
	}

	// Note: Actual execution would require a running MCP server
	// This just verifies the tool can be called
	tool := tools[0]

	// Verify tool can handle empty parameters
	if tool.IsAvailable != nil && !tool.IsAvailable(nil) {
		t.Log("Tool not available in this context")
		return
	}

	// The actual call would fail without a server, but we can verify the structure
	t.Logf("Tool structure verified: %s", tool.Metadata.Name)
}
