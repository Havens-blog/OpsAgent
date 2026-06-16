// Package guardrails provides security guardrails for agent operations.
package guardrails

import "fmt"

// SideEffectLevel defines the level of side effects a tool may have.
// This is inspired by OpenSRE's side effect classification.
type SideEffectLevel string

const (
	// SideEffectNone represents tools with no side effects.
	// Examples: data validation, format conversion, pure computation
	SideEffectNone SideEffectLevel = "none"

	// SideEffectReadOnly represents tools that only read data.
	// Examples: query logs, fetch metrics, read configurations
	SideEffectReadOnly SideEffectLevel = "read_only"

	// SideEffectMutating represents tools that modify state but are contained within the system.
	// Examples: update internal cache, modify local files, update database records
	SideEffectMutating SideEffectLevel = "mutating"

	// SideEffectExternal represents tools that have external effects.
	// Examples: restart services, delete cloud resources, send notifications
	SideEffectExternal SideEffectLevel = "external"
)

// String returns the string representation of the side effect level.
func (s SideEffectLevel) String() string {
	return string(s)
}

// IsSafe returns true if this level represents safe operations.
func (s SideEffectLevel) IsSafe() bool {
	return s == SideEffectNone || s == SideEffectReadOnly
}

// RequiresConfirmation returns true if this level requires user confirmation.
func (s SideEffectLevel) RequiresConfirmation() bool {
	return s == SideEffectMutating || s == SideEffectExternal
}

// Validate checks if the side effect level is valid.
func (s SideEffectLevel) Validate() bool {
	switch s {
	case SideEffectNone, SideEffectReadOnly, SideEffectMutating, SideEffectExternal:
		return true
	default:
		return false
	}
}

// ToExecutionTier converts a side effect level to an execution tier.
func (s SideEffectLevel) ToExecutionTier() ExecutionTier {
	switch s {
	case SideEffectNone, SideEffectReadOnly:
		return TierSafe
	case SideEffectMutating, SideEffectExternal:
		return TierElevated
	default:
		return TierSafe
	}
}

// DefaultSideEffectLevel returns the default side effect level for tools.
func DefaultSideEffectLevel() SideEffectLevel {
	return SideEffectReadOnly
}

// SideEffectLevelFromString parses a string into a SideEffectLevel.
func SideEffectLevelFromString(s string) (SideEffectLevel, error) {
	switch s {
	case string(SideEffectNone):
		return SideEffectNone, nil
	case string(SideEffectReadOnly):
		return SideEffectReadOnly, nil
	case string(SideEffectMutating):
		return SideEffectMutating, nil
	case string(SideEffectExternal):
		return SideEffectExternal, nil
	default:
		return SideEffectReadOnly, fmt.Errorf("invalid side effect level: %s", s)
	}
}
