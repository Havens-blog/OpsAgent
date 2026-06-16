// Package tool provides tool registration and discovery functionality.
package tool

import (
	"context"
	"fmt"
	"sync"

	"github.com/yourusername/OpsAgent/internal/guardrails"
)

// ToolFunc represents the function that implements a tool.
type ToolFunc func(ctx context.Context, params map[string]interface{}) (interface{}, error)

// Tool represents a registered tool with its metadata and execution function.
type Tool struct {
	Metadata *ToolMetadata
	Run      ToolFunc
	// IsAvailable is an optional function to check tool availability at runtime
	IsAvailable func(config map[string]interface{}) bool
	// ExtractParams is an optional function to extract parameters from config
	ExtractParams func(config map[string]interface{}) (map[string]interface{}, error)
}

// ValidatePublicInput validates the public input parameters against the schema.
func (t *Tool) ValidatePublicInput(params map[string]interface{}) error {
	publicSchema, err := t.Metadata.PublicInputSchema()
	if err != nil {
		return fmt.Errorf("failed to get public input schema: %w", err)
	}

	// Check required parameters
	if required, ok := publicSchema["required"].([]interface{}); ok {
		for _, req := range required {
			if reqStr, ok := req.(string); ok {
				if _, exists := params[reqStr]; !exists {
					return fmt.Errorf("required parameter missing: %s", reqStr)
				}
			}
		}
	}

	// Check for unknown parameters (if additionalProperties is false)
	if additionalProps, ok := publicSchema["additionalProperties"].(bool); ok && !additionalProps {
		if props, ok := publicSchema["properties"].(map[string]interface{}); ok {
			for key := range params {
				if _, exists := props[key]; !exists {
					return fmt.Errorf("unknown parameter: %s", key)
				}
			}
		}
	}

	return nil
}

// Execute executes the tool with the given parameters.
func (t *Tool) Execute(ctx context.Context, publicParams map[string]interface{}, config map[string]interface{}) (interface{}, error) {
	// Validate public input
	if err := t.ValidatePublicInput(publicParams); err != nil {
		return nil, fmt.Errorf("input validation failed: %w", err)
	}

	// Check availability
	if t.IsAvailable != nil {
		if !t.IsAvailable(config) {
			return nil, fmt.Errorf("tool %s is not available", t.Metadata.Name)
		}
	} else if !t.Metadata.IsAvailable(config) {
		return nil, fmt.Errorf("tool %s is not available: data source not configured", t.Metadata.Name)
	}

	// Extract injected parameters
	injectedParams := make(map[string]interface{})
	if t.ExtractParams != nil {
		var err error
		injectedParams, err = t.ExtractParams(config)
		if err != nil {
			return nil, fmt.Errorf("failed to extract injected parameters: %w", err)
		}
	} else {
		injectedParams, _ = t.Metadata.ExtractParams(config)
	}

	// Merge public and injected parameters
	mergedParams := make(map[string]interface{})
	for k, v := range publicParams {
		mergedParams[k] = v
	}
	for k, v := range injectedParams {
		mergedParams[k] = v
	}

	// Execute the tool
	return t.Run(ctx, mergedParams)
}

// Registry manages tool registration and discovery.
// This is inspired by OpenSRE's tool registry system.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]*Tool
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*Tool),
	}
}

// Register registers a tool in the registry.
func (r *Registry) Register(tool *Tool) error {
	if tool == nil {
		return fmt.Errorf("tool cannot be nil")
	}

	if err := tool.Metadata.Validate(); err != nil {
		return fmt.Errorf("invalid tool metadata: %w", err)
	}

	if tool.Run == nil {
		return fmt.Errorf("tool run function cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if tool already exists
	if _, exists := r.tools[tool.Metadata.Name]; exists {
		return fmt.Errorf("tool %s already registered", tool.Metadata.Name)
	}

	r.tools[tool.Metadata.Name] = tool
	return nil
}

// Unregister removes a tool from the registry.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; !exists {
		return fmt.Errorf("tool %s not found", name)
	}

	delete(r.tools, name)
	return nil
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (*Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, exists := r.tools[name]
	if !exists {
		return nil, fmt.Errorf("tool %s not found", name)
	}

	return tool, nil
}

// List returns all registered tool names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// ListByCategory returns tool names filtered by category.
func (r *Registry) ListByCategory(category string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0)
	for name, tool := range r.tools {
		if tool.Metadata.Category == category {
			names = append(names, name)
		}
	}
	return names
}

// ListBySource returns tool names filtered by data source.
func (r *Registry) ListBySource(source DataSourceType) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0)
	for name, tool := range r.tools {
		if tool.Metadata.Source == source {
			names = append(names, name)
		}
	}
	return names
}

// ListSafe returns only safe tools (no confirmation required).
func (r *Registry) ListSafe() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0)
	for name, tool := range r.tools {
		if tool.Metadata.IsSafe() {
			names = append(names, name)
		}
	}
	return names
}

// GetToolSpecsForLLM returns tool specifications formatted for LLM consumption.
// This excludes injected parameters from the schemas.
func (r *Registry) GetToolSpecsForLLM(config map[string]interface{}) ([]map[string]interface{}, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	specs := make([]map[string]interface{}, 0, len(r.tools))

	for _, tool := range r.tools {
		// Check availability
		isAvailable := true
		if tool.IsAvailable != nil {
			isAvailable = tool.IsAvailable(config)
		} else {
			isAvailable = tool.Metadata.IsAvailable(config)
		}

		if !isAvailable {
			continue
		}

		// Get public schema
		publicSchema, err := tool.Metadata.PublicInputSchema()
		if err != nil {
			return nil, fmt.Errorf("failed to get public schema for %s: %w", tool.Metadata.Name, err)
		}

		spec := map[string]interface{}{
			"name":        tool.Metadata.Name,
			"description": tool.Metadata.Description,
			"input_schema": map[string]interface{}{
				"type":       "object",
				"properties": publicSchema["properties"],
			},
		}

		// Add required fields if present
		if required, ok := publicSchema["required"]; ok {
			spec["input_schema"].(map[string]interface{})["required"] = required
		}

		specs = append(specs, spec)
	}

	return specs, nil
}

// Clear removes all tools from the registry.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools = make(map[string]*Tool)
}

// Count returns the number of registered tools.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.tools)
}
