// Package tool provides tool metadata and registration functionality.
package tool

import (
	"encoding/json"
	"fmt"

	"github.com/yourusername/OpsAgent/internal/guardrails"
)

// DataSourceType represents the type of data source a tool interacts with.
type DataSourceType string

const (
	// DataSourceTypeSLS represents Alibaba Cloud SLS
	DataSourceTypeSLS DataSourceType = "aliyun_sls"
	// DataSourceTypeTLS represents Volcengine TLS
	DataSourceTypeTLS DataSourceType = "volcengine_tls"
	// DataSourceTypeLTS represents Huawei Cloud LTS
	DataSourceTypeLTS DataSourceType = "huawei_lts"
	// DataSourceTypeARMS represents Alibaba Cloud ARMS
	DataSourceTypeARMS DataSourceType = "aliyun_arms"
	// DataSourceTypePrometheus represents Prometheus
	DataSourceTypePrometheus DataSourceType = "prometheus"
	// DataSourceTypeInfluxDB represents InfluxDB
	DataSourceTypeInfluxDB DataSourceType = "influxdb"
	// DataSourceTypeElasticsearch represents Elasticsearch
	DataSourceTypeElasticsearch DataSourceType = "elasticsearch"
	// DataSourceTypeGeneric represents generic data sources
	DataSourceTypeGeneric DataSourceType = "generic"
)

// ToolMetadata contains metadata about a tool.
// This is inspired by OpenSRE's tool metadata system.
type ToolMetadata struct {
	Name              string              // Unique tool identifier
	Description       string              // Human-readable description
	DisplayName       string              `json:"display_name,omitempty"`       // Optional display name
	Source            DataSourceType      // Data source type
	InputSchema       json.RawMessage     // JSON Schema for input parameters
	OutputSchema      json.RawMessage     `json:"output_schema,omitempty"`     // JSON Schema for output
	InjectedParams    []string            // Parameters injected at runtime (not exposed to LLM)
	Tier              guardrails.ExecutionTier // Execution tier
	SideEffect        guardrails.SideEffectLevel // Side effect level
	RequiresApproval  bool                // Whether approval is required
	ApprovalReason    string              `json:"approval_reason,omitempty"`   // Reason for approval requirement
	ApprovalExpirySec int                 `json:"approval_expiry_sec,omitempty"` // Approval expiry time
	ApprovalScope     string              `json:"approval_scope,omitempty"`    // Approval scope (one_shot/session)
	UseCases          []string            `json:"use_cases,omitempty"`         // Usage examples
	Examples          []string            `json:"examples,omitempty"`          // Positive examples
	AntiExamples      []string            `json:"anti_examples,omitempty"`     // Negative examples
	Category          string              `json:"category,omitempty"`           // Tool category
	Version           string              `json:"version,omitempty"`            // Tool version
	Deprecated        bool                `json:"deprecated,omitempty"`        // Whether tool is deprecated
	DeprecationReason string              `json:"deprecation_reason,omitempty"` // Reason for deprecation
}

// Validate validates the tool metadata.
func (m *ToolMetadata) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	if m.Description == "" {
		return fmt.Errorf("tool description is required")
	}
	if m.Source == "" {
		return fmt.Errorf("data source is required")
	}
	if m.InputSchema == nil {
		return fmt.Errorf("input schema is required")
	}
	if !m.Tier.Validate() {
		return fmt.Errorf("invalid execution tier: %s", m.Tier)
	}
	if !m.SideEffect.Validate() {
		return fmt.Errorf("invalid side effect level: %s", m.SideEffect)
	}
	if m.RequiresApproval && m.ApprovalReason == "" {
		return fmt.Errorf("approval reason is required when approval is required")
	}
	return nil
}

// PublicInputSchema returns the input schema with injected parameters removed.
// This ensures sensitive parameters (credentials, endpoints) are not exposed to the LLM.
func (m *ToolMetadata) PublicInputSchema() (map[string]interface{}, error) {
	var schema map[string]interface{}
	if err := json.Unmarshal(m.InputSchema, &schema); err != nil {
		return nil, fmt.Errorf("failed to unmarshal input schema: %w", err)
	}

	// Create a copy to avoid mutating the original
	publicSchema := make(map[string]interface{})
	for k, v := range schema {
		publicSchema[k] = v
	}

	// Remove injected parameters from properties
	if props, ok := publicSchema["properties"].(map[string]interface{}); ok {
		for _, injected := range m.InjectedParams {
			delete(props, injected)
		}
	}

	// Remove injected parameters from required array
	if required, ok := publicSchema["required"].([]interface{}); ok {
		filtered := []interface{}{}
		for _, r := range required {
			if reqStr, ok := r.(string); ok {
				if !contains(m.InjectedParams, reqStr) {
					filtered = append(filtered, r)
				}
			}
		}
		publicSchema["required"] = filtered
	}

	return publicSchema, nil
}

// IsAvailable checks if the tool is available based on the provided configuration.
func (m *ToolMetadata) IsAvailable(config map[string]interface{}) bool {
	sourceConfig, ok := config[string(m.Source)]
	if !ok {
		return false
	}

	// Check if the source configuration has the required fields
	configMap, ok := sourceConfig.(map[string]interface{})
	if !ok {
		return false
	}

	// Basic availability check - at least one field should be present
	return len(configMap) > 0
}

// ExtractParams extracts injected parameters from the configuration.
func (m *ToolMetadata) ExtractParams(config map[string]interface{}) (map[string]interface{}, error) {
	params := make(map[string]interface{})

	sourceConfig, ok := config[string(m.Source)]
	if !ok {
		return params, nil
	}

	configMap, ok := sourceConfig.(map[string]interface{})
	if !ok {
		return params, nil
	}

	// Extract injected parameters
	for _, param := range m.InjectedParams {
		if val, exists := configMap[param]; exists {
			params[param] = val
		}
	}

	return params, nil
}

// RequiresConfirmation returns true if the tool requires user confirmation.
func (m *ToolMetadata) RequiresConfirmation() bool {
	return m.RequiresApproval || m.Tier.RequiresConfirmation() || m.SideEffect.RequiresConfirmation()
}

// IsSafe returns true if the tool is considered safe.
func (m *ToolMetadata) IsSafe() bool {
	return m.Tier.IsSafe() && m.SideEffect.IsSafe()
}

// contains checks if a string slice contains a specific string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
