// Package guardrails provides security guardrails for agent operations.
package guardrails

// ExecutionTier defines the security level for tool execution.
// This is inspired by OpenSRE's execution tier system.
type ExecutionTier string

const (
	// TierExempt represents meta-commands that require no confirmation.
	// Examples: /help, /exit, /status
	TierExempt ExecutionTier = "exempt"

	// TierSafe represents read-only operations that are always allowed.
	// Examples: query logs, fetch metrics, list resources
	TierSafe ExecutionTier = "safe"

	// TierElevated represents operations that modify state and require confirmation.
	// Examples: restart services, modify configurations, delete resources
	TierElevated ExecutionTier = "elevated"
)

// String returns the string representation of the execution tier.
func (t ExecutionTier) String() string {
	return string(t)
}

// RequiresConfirmation returns true if this tier requires user confirmation.
func (t ExecutionTier) RequiresConfirmation() bool {
	return t == TierElevated
}

// IsSafe returns true if this tier represents safe operations.
func (t ExecutionTier) IsSafe() bool {
	return t == TierSafe || t == TierExempt
}

// Validate checks if the execution tier is valid.
func (t ExecutionTier) Validate() bool {
	switch t {
	case TierExempt, TierSafe, TierElevated:
		return true
	default:
		return false
	}
}

// DefaultTier returns the default execution tier for tools.
func DefaultTier() ExecutionTier {
	return TierSafe
}
