// Package tool provides tool registration, validation, and discovery.
package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/guardrails"
)

// TestNewRegistry tests registry creation.
func TestNewRegistry(t *testing.T) {
	t.Run("creates_registry", func(t *testing.T) {
		registry := NewRegistry()
		if registry == nil {
			t.Fatal("Expected non-nil registry")
		}
	})

	t.Run("registry_has_no_tools_initially", func(t *testing.T) {
		registry := NewRegistry()

		tools := registry.List()
		if len(tools) != 0 {
			t.Errorf("Expected no tools, got %d", len(tools))
		}
	})
}

// TestToolRegistration tests tool registration.
func TestToolRegistration(t *testing.T) {
	t.Run("registers_tool_successfully", func(t *testing.T) {
		registry := NewRegistry()

		tool := &Tool{
			Metadata: &ToolMetadata{
				Name:        "query-logs",
				Description: "Query logs from data source",
				Source:      DataSourceTypeGeneric,
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Tier:        guardrails.TierSafe,
				SideEffect:  guardrails.SideEffectReadOnly,
			},
			Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return "result", nil
			},
		}

		err := registry.Register(tool)
		if err != nil {
			t.Fatalf("Failed to register tool: %v", err)
		}

		tools := registry.List()
		if len(tools) != 1 {
			t.Errorf("Expected 1 tool, got %d", len(tools))
		}
	})

	t.Run("prevents_duplicate_registration", func(t *testing.T) {
		registry := NewRegistry()

		tool := &Tool{
			Metadata: &ToolMetadata{
				Name:        "test-tool",
				Description: "Test tool",
				Source:      DataSourceTypeGeneric,
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Tier:        guardrails.TierSafe,
				SideEffect:  guardrails.SideEffectNone,
			},
			Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return "result", nil
			},
		}

		registry.Register(tool)

		err := registry.Register(tool)
		if err == nil {
			t.Error("Expected error for duplicate registration")
		}
	})

	t.Run("validates_required_fields", func(t *testing.T) {
		registry := NewRegistry()

		// Missing name
		tool := &Tool{
			Metadata: &ToolMetadata{
				Description: "Tool without name",
				Source:      DataSourceTypeGeneric,
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Tier:        guardrails.TierSafe,
				SideEffect:  guardrails.SideEffectNone,
			},
			Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return "result", nil
			},
		}

		err := registry.Register(tool)
		if err == nil {
			t.Error("Expected error for missing name")
		}
	})
}

// TestToolRetrieval tests tool retrieval.
func TestToolRetrieval(t *testing.T) {
	registry := NewRegistry()

	tool := &Tool{
		Metadata: &ToolMetadata{
			Name:        "query-logs",
			Description: "Query logs from data source",
			Source:      DataSourceTypeGeneric,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "result", nil
		},
	}

	registry.Register(tool)

	t.Run("gets_existing_tool", func(t *testing.T) {
		retrieved, err := registry.Get("query-logs")
		if err != nil {
			t.Fatalf("Failed to get tool: %v", err)
		}

		if retrieved.Metadata.Name != "query-logs" {
			t.Errorf("Expected tool name 'query-logs', got '%s'", retrieved.Metadata.Name)
		}
	})

	t.Run("returns_error_for_nonexistent_tool", func(t *testing.T) {
		_, err := registry.Get("nonexistent")
		if err == nil {
			t.Error("Expected error for nonexistent tool")
		}
	})

	t.Run("checks_tool_exists", func(t *testing.T) {
		_, err := registry.Get("query-logs")
		if err != nil {
			t.Error("Expected tool to exist")
		}

		_, err = registry.Get("nonexistent")
		if err == nil {
			t.Error("Expected error for nonexistent tool")
		}
	})
}

// TestToolExecution tests tool execution.
func TestToolExecution(t *testing.T) {
	t.Run("executes_tool_with_params", func(t *testing.T) {
		registry := NewRegistry()

		tool := &Tool{
			Metadata: &ToolMetadata{
				Name:        "test-tool",
				Description: "Test tool",
				Source:      DataSourceTypeGeneric,
				InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
				Tier:        guardrails.TierSafe,
				SideEffect:  guardrails.SideEffectNone,
			},
			Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return params["query"], nil
			},
		}

		registry.Register(tool)

		retrieved, _ := registry.Get("test-tool")
		config := map[string]interface{}{
			string(DataSourceTypeGeneric): map[string]interface{}{
				"enabled": true,
			},
		}
		result, err := retrieved.Execute(context.Background(), map[string]interface{}{"query": "test"}, config)
		if err != nil {
			t.Fatalf("Failed to execute tool: %v", err)
		}

		if result != "test" {
			t.Errorf("Expected 'test', got %v", result)
		}
	})

	t.Run("validates_required_params", func(t *testing.T) {
		registry := NewRegistry()

		tool := &Tool{
			Metadata: &ToolMetadata{
				Name:        "test-tool",
				Description: "Test tool",
				Source:      DataSourceTypeGeneric,
				InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
				Tier:        guardrails.TierSafe,
				SideEffect:  guardrails.SideEffectNone,
			},
			Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return params["query"], nil
			},
		}

		registry.Register(tool)

		retrieved, _ := registry.Get("test-tool")
		_, err := retrieved.Execute(context.Background(), map[string]interface{}{}, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing required parameter")
		}
	})
}

// TestToolListByCategory tests filtering by category.
func TestToolListByCategory(t *testing.T) {
	registry := NewRegistry()

	registry.Register(&Tool{
		Metadata: &ToolMetadata{
			Name:        "log-tool",
			Description: "Log tool",
			Source:      DataSourceTypeGeneric,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectNone,
			Category:    "logs",
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "result", nil
		},
	})

	registry.Register(&Tool{
		Metadata: &ToolMetadata{
			Name:        "metric-tool",
			Description: "Metric tool",
			Source:      DataSourceTypeGeneric,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectNone,
			Category:    "metrics",
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "result", nil
		},
	})

	t.Run("filters_by_category", func(t *testing.T) {
		logTools := registry.ListByCategory("logs")
		if len(logTools) != 1 {
			t.Errorf("Expected 1 log tool, got %d", len(logTools))
		}

		metricTools := registry.ListByCategory("metrics")
		if len(metricTools) != 1 {
			t.Errorf("Expected 1 metric tool, got %d", len(metricTools))
		}
	})
}

// TestToolListBySource tests filtering by data source.
func TestToolListBySource(t *testing.T) {
	registry := NewRegistry()

	registry.Register(&Tool{
		Metadata: &ToolMetadata{
			Name:        "sls-tool",
			Description: "SLS tool",
			Source:      DataSourceTypeSLS,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectNone,
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "result", nil
		},
	})

	registry.Register(&Tool{
		Metadata: &ToolMetadata{
			Name:        "prometheus-tool",
			Description: "Prometheus tool",
			Source:      DataSourceTypePrometheus,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectNone,
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "result", nil
		},
	})

	t.Run("filters_by_source", func(t *testing.T) {
		slsTools := registry.ListBySource(DataSourceTypeSLS)
		if len(slsTools) != 1 {
			t.Errorf("Expected 1 SLS tool, got %d", len(slsTools))
		}

		promTools := registry.ListBySource(DataSourceTypePrometheus)
		if len(promTools) != 1 {
			t.Errorf("Expected 1 Prometheus tool, got %d", len(promTools))
		}
	})
}

// TestPublicInputSchema tests parameter injection filtering.
func TestPublicInputSchema(t *testing.T) {
	t.Run("removes_injected_params_from_schema", func(t *testing.T) {
		metadata := &ToolMetadata{
			Name:        "api-tool",
			Description: "API tool",
			Source:      DataSourceTypeGeneric,
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string"},
					"api_key": {"type": "string"}
				},
				"required": ["query", "api_key"]
			}`),
			InjectedParams: []string{"api_key"},
			Tier:           guardrails.TierSafe,
			SideEffect:     guardrails.SideEffectNone,
		}

		publicSchema, err := metadata.PublicInputSchema()
		if err != nil {
			t.Fatalf("Failed to get public schema: %v", err)
		}

		props, ok := publicSchema["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected properties to be a map")
		}

		if _, exists := props["api_key"]; exists {
			t.Error("Expected api_key to be removed from properties")
		}

		required, ok := publicSchema["required"].([]interface{})
		if !ok {
			t.Fatal("Expected required to be a list")
		}

		for _, r := range required {
			if r == "api_key" {
				t.Error("Expected api_key to be removed from required")
			}
		}
	})
}

// TestToolListSafe tests filtering safe tools.
func TestToolListSafe(t *testing.T) {
	registry := NewRegistry()

	registry.Register(&Tool{
		Metadata: &ToolMetadata{
			Name:        "safe-tool",
			Description: "Safe tool",
			Source:      DataSourceTypeGeneric,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectNone,
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "result", nil
		},
	})

	registry.Register(&Tool{
		Metadata: &ToolMetadata{
			Name:        "unsafe-tool",
			Description: "Unsafe tool",
			Source:      DataSourceTypeGeneric,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Tier:        guardrails.TierElevated,
			SideEffect:  guardrails.SideEffectMutating,
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "result", nil
		},
	})

	t.Run("lists_only_safe_tools", func(t *testing.T) {
		safeTools := registry.ListSafe()
		if len(safeTools) != 1 {
			t.Errorf("Expected 1 safe tool, got %d", len(safeTools))
		}

		if safeTools[0] != "safe-tool" {
			t.Errorf("Expected 'safe-tool', got '%s'", safeTools[0])
		}
	})
}

// TestRegistryCount tests counting tools.
func TestRegistryCount(t *testing.T) {
	registry := NewRegistry()

	if registry.Count() != 0 {
		t.Errorf("Expected 0 tools, got %d", registry.Count())
	}

	registry.Register(&Tool{
		Metadata: &ToolMetadata{
			Name:        "tool1",
			Description: "Tool 1",
			Source:      DataSourceTypeGeneric,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectNone,
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "result", nil
		},
	})

	if registry.Count() != 1 {
		t.Errorf("Expected 1 tool, got %d", registry.Count())
	}

	registry.Register(&Tool{
		Metadata: &ToolMetadata{
			Name:        "tool2",
			Description: "Tool 2",
			Source:      DataSourceTypeGeneric,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectNone,
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "result", nil
		},
	})

	if registry.Count() != 2 {
		t.Errorf("Expected 2 tools, got %d", registry.Count())
	}
}

// TestUnregister tests tool removal.
func TestUnregister(t *testing.T) {
	registry := NewRegistry()

	tool := &Tool{
		Metadata: &ToolMetadata{
			Name:        "tool1",
			Description: "Tool 1",
			Source:      DataSourceTypeGeneric,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectNone,
		},
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "result", nil
		},
	}

	registry.Register(tool)

	if registry.Count() != 1 {
		t.Errorf("Expected 1 tool, got %d", registry.Count())
	}

	err := registry.Unregister("tool1")
	if err != nil {
		t.Fatalf("Failed to unregister tool: %v", err)
	}

	if registry.Count() != 0 {
		t.Errorf("Expected 0 tools, got %d", registry.Count())
	}

	err = registry.Unregister("tool1")
	if err == nil {
		t.Error("Expected error when unregistering nonexistent tool")
	}
}
