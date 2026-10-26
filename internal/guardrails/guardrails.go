// Package guardrails provides security fence mechanisms for safe agent execution.
// This module integrates execution tiers, approval mechanisms, side effect classification,
// and capability toggles to ensure safe and controlled agent operations.
package guardrails

import (
	"context"
	"fmt"
	"sync"
)

// CapabilityManager manages capability toggles.
type CapabilityManager struct {
	mu           sync.RWMutex
	capabilities map[string]bool // false means disabled
}

// NewCapabilityManager creates a new capability manager.
func NewCapabilityManager() *CapabilityManager {
	return &CapabilityManager{
		capabilities: make(map[string]bool),
	}
}

// IsEnabled checks if a capability is enabled.
func (m *CapabilityManager) IsEnabled(capability string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Default to enabled if not explicitly disabled
	enabled, exists := m.capabilities[capability]
	if !exists {
		return true
	}
	return enabled
}

// Enable enables a capability.
func (m *CapabilityManager) Enable(capability string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.capabilities == nil {
		m.capabilities = make(map[string]bool)
	}
	m.capabilities[capability] = true
}

// Disable disables a capability.
func (m *CapabilityManager) Disable(capability string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.capabilities == nil {
		m.capabilities = make(map[string]bool)
	}
	m.capabilities[capability] = false
}

// GetDisabled returns a list of disabled capabilities.
func (m *CapabilityManager) GetDisabled() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var disabled []string
	for capability, enabled := range m.capabilities {
		if !enabled {
			disabled = append(disabled, capability)
		}
	}
	return disabled
}

// CheckExecution represents a request to check if execution is allowed.
type CheckExecution struct {
	ToolName     string
	Operation    string
	Tier         ExecutionTier
	SideEffect   SideEffectLevel
	Capabilities []string
}

// ExecutionResult represents the result of an execution check.
type ExecutionResult struct {
	Allowed bool
	Reason  string
}

// Guardrails provides the complete guardrails system.
type Guardrails struct {
	approvals    *ApprovalManager
	capabilities *CapabilityManager
	mu           sync.RWMutex
}

// New creates a new guardrails instance.
func New() *Guardrails {
	return &Guardrails{
		approvals:    NewApprovalManager(nil),
		capabilities: NewCapabilityManager(),
	}
}

// CanExecute checks if an operation can be executed.
func (g *Guardrails) CanExecute(ctx context.Context, check CheckExecution) ExecutionResult {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// 1. Check tier approval
	if check.Tier.RequiresConfirmation() {
		// For elevated tiers, we need to check approval
		// This is a simplified check - in production, you'd check against the actual store
		return ExecutionResult{
			Allowed: false,
			Reason:  fmt.Sprintf("Tool '%s' requires approval", check.ToolName),
		}
	}

	// 2. Check capabilities
	for _, capability := range check.Capabilities {
		if !g.capabilities.IsEnabled(capability) {
			return ExecutionResult{
				Allowed: false,
				Reason:  fmt.Sprintf("Capability '%s' is disabled", capability),
			}
		}
	}

	// 3. Check side effects
	if check.SideEffect.RequiresConfirmation() && check.Tier == TierSafe {
		return ExecutionResult{
			Allowed: false,
			Reason:  "Mutating operation requires elevated tier",
		}
	}

	return ExecutionResult{
		Allowed: true,
	}
}

// Approvals returns the approval manager.
func (g *Guardrails) Approvals() *ApprovalManager {
	return g.approvals
}

// Capabilities returns the capability manager.
func (g *Guardrails) Capabilities() *CapabilityManager {
	return g.capabilities
}

// LogExecution logs an execution attempt.
func (g *Guardrails) LogExecution(ctx context.Context, check CheckExecution, result ExecutionResult, err error) {
	// In production, this would write to audit log
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Log format: tool, operation, tier, side_effect, allowed, reason
	entry := map[string]interface{}{
		"tool":        check.ToolName,
		"operation":   check.Operation,
		"tier":        check.Tier.String(),
		"side_effect": check.SideEffect,
		"allowed":     result.Allowed,
		"reason":      result.Reason,
	}

	if err != nil {
		entry["error"] = err.Error()
	}

	// Would send to audit logger
	_ = entry
}
