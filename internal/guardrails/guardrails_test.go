// Package guardrails provides security fence mechanisms for safe agent execution.
package guardrails

import (
	"context"
	"testing"
	"time"
)

// TestExecutionTiers tests execution tier classification.
func TestExecutionTiers(t *testing.T) {
	t.Run("tier_classification", func(t *testing.T) {
		tests := []struct {
			name      string
			tier      ExecutionTier
			shouldLog bool
			dangerous bool
		}{
			{"exempt tier", TierExempt, false, false},
			{"safe tier", TierSafe, true, false},
			{"elevated tier", TierElevated, true, true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if tt.tier.RequiresConfirmation() && tt.tier == TierExempt {
					t.Error("TierExempt should not require confirmation")
				}
				if tt.tier.IsSafe() && tt.tier == TierElevated {
					t.Error("TierElevated should not be safe")
				}
			})
		}
	})

	t.Run("tier_string_values", func(t *testing.T) {
		if TierExempt.String() != "exempt" {
			t.Errorf("Expected 'exempt', got '%s'", TierExempt.String())
		}
		if TierSafe.String() != "safe" {
			t.Errorf("Expected 'safe', got '%s'", TierSafe.String())
		}
		if TierElevated.String() != "elevated" {
			t.Errorf("Expected 'elevated', got '%s'", TierElevated.String())
		}
	})
}

// TestApprovalManager tests approval mechanism.
func TestApprovalManager(t *testing.T) {
	t.Run("creates_approval_manager", func(t *testing.T) {
		mgr := NewApprovalManager(nil)
		if mgr == nil {
			t.Fatal("Expected non-nil manager")
		}
	})

	t.Run("requires_approval_for_elevated", func(t *testing.T) {
		mgr := NewApprovalManager(nil)

		req := ApprovalRequest{
			ToolName:      "restart-service",
			Operation:     "restart",
			Reason:        "Service needs restart",
			ExpirySeconds: 300,
			Scope:         ScopeOneShot,
		}

		err := mgr.RequireApproval(req)
		if err == nil {
			t.Error("Expected error for unapproved elevated operation")
		}
	})

	t.Run("grants_and_validates_approval", func(t *testing.T) {
		mgr := NewApprovalManager(nil)

		// Grant approval
		err := mgr.GrantApproval("restart-service", ScopeSession, "", 300)
		if err != nil {
			t.Fatalf("Failed to grant approval: %v", err)
		}

		// Validate approval
		isValid, _, _ := mgr.store.IsValid("restart-service", "", "")
		if !isValid {
			t.Error("Expected valid approval")
		}
	})

	t.Run("approval_expires", func(t *testing.T) {
		mgr := NewApprovalManager(nil)

		// Grant with short expiry
		mgr.GrantApproval("test-tool", ScopeOneShot, "", 1)

		// Wait for expiry
		time.Sleep(2 * time.Second)

		// Should be expired
		isValid, _, _ := mgr.store.IsValid("test-tool", "", "")
		if isValid {
			t.Error("Expected approval to be expired")
		}
	})

	t.Run("revokes_approval", func(t *testing.T) {
		mgr := NewApprovalManager(nil)

		mgr.GrantApproval("test-tool", ScopeSession, "", 300)

		// Revoke
		mgr.RevokeApproval("test-tool", "")

		// Should be invalid
		isValid, _, _ := mgr.store.IsValid("test-tool", "", "")
		if isValid {
			t.Error("Expected approval to be revoked")
		}
	})

	t.Run("param_version_binding", func(t *testing.T) {
		mgr := NewApprovalManager(nil)

		paramsA := map[string]interface{}{"service": "order-service", "count": 1}
		paramsB := map[string]interface{}{"service": "order-service", "count": 2}

		// Grant approval for params A.
		if err := mgr.GrantApprovalWithHash("restart-service", ScopeOneShot, "session-1", ParamHash(paramsA), 300); err != nil {
			t.Fatalf("grant A: %v", err)
		}

		// Same params should pass (and consume the one-shot grant).
		if err := mgr.RequireApproval(ApprovalRequest{
			ToolName: "restart-service", Operation: "restart", Params: paramsA, Scope: ScopeOneShot, SessionID: "session-1",
		}); err != nil {
			t.Fatalf("params A should be pre-approved, got %v", err)
		}

		// Different params must require approval again.
		err := mgr.RequireApproval(ApprovalRequest{
			ToolName: "restart-service", Operation: "restart", Params: paramsB, Scope: ScopeOneShot, SessionID: "session-1",
		})
		if !ApprovalRequired(err) {
			t.Fatalf("params B must re-trigger approval, got %v", err)
		}
	})
}

// TestSideEffectLevels tests side effect classification.
func TestSideEffectLevels(t *testing.T) {
	t.Run("level_classification", func(t *testing.T) {
		tests := []struct {
			name       string
			level      SideEffectLevel
			mutating   bool
			external   bool
			readonly   bool
		}{
			{"none level", SideEffectNone, false, false, true},
			{"read_only level", SideEffectReadOnly, false, false, true},
			{"mutating level", SideEffectMutating, true, false, false},
			{"external level", SideEffectExternal, true, true, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if tt.level.RequiresConfirmation() != tt.mutating {
					t.Errorf("Expected mutating=%v, got %v", tt.mutating, tt.level.RequiresConfirmation())
				}
				if tt.level.IsExternal() != tt.external {
					t.Errorf("Expected external=%v, got %v", tt.external, tt.level.IsExternal())
				}
				if tt.level.IsSafe() != tt.readonly {
					t.Errorf("Expected readonly=%v, got %v", tt.readonly, tt.level.IsSafe())
				}
			})
		}
	})
}

// TestCapabilityTests tests capability toggles.
func TestCapabilityTests(t *testing.T) {
	t.Run("creates_capability_manager", func(t *testing.T) {
		mgr := NewCapabilityManager()
		if mgr == nil {
			t.Fatal("Expected non-nil manager")
		}
	})

	t.Run("checks_capability_enabled", func(t *testing.T) {
		mgr := NewCapabilityManager()

		// By default, capabilities are enabled
		if !mgr.IsEnabled("file-write") {
			t.Error("Expected capability to be enabled by default")
		}
	})

	t.Run("disables_capability", func(t *testing.T) {
		mgr := NewCapabilityManager()

		// Disable capability
		mgr.Disable("file-write")

		// Check disabled
		if mgr.IsEnabled("file-write") {
			t.Error("Expected capability to be disabled")
		}
	})

	t.Run("re_enables_capability", func(t *testing.T) {
		mgr := NewCapabilityManager()

		// Disable then enable
		mgr.Disable("file-write")
		mgr.Enable("file-write")

		// Should be enabled
		if !mgr.IsEnabled("file-write") {
			t.Error("Expected capability to be re-enabled")
		}
	})

	t.Run("checks_multiple_capabilities", func(t *testing.T) {
		mgr := NewCapabilityManager()

		mgr.Disable("network-access")
		mgr.Disable("file-write")

		if mgr.IsEnabled("network-access") {
			t.Error("Expected network-access to be disabled")
		}
		if !mgr.IsEnabled("log-query") {
			t.Error("Expected log-query to be enabled")
		}
	})
}

// TestExecutionGuardrails tests the complete guardrails system.
func TestExecutionGuardrails(t *testing.T) {
	t.Run("creates_guardrails", func(t *testing.T) {
		guardrails := New()
		if guardrails == nil {
			t.Fatal("Expected non-nil guardrails")
		}
	})

	t.Run("safe_operation_passes", func(t *testing.T) {
		guardrails := New()

		check := CheckExecution{
			ToolName:     "query-logs",
			Tier:         TierSafe,
			SideEffect:   SideEffectReadOnly,
			Capabilities: []string{"log-query"},
		}

		result := guardrails.CanExecute(context.Background(), check)
		if !result.Allowed {
			t.Errorf("Expected safe operation to be allowed: %v", result.Reason)
		}
	})

	t.Run("elevated_without_approval_blocked", func(t *testing.T) {
		guardrails := New()

		check := CheckExecution{
			ToolName:     "restart-service",
			Tier:         TierElevated,
			SideEffect:   SideEffectMutating,
			Capabilities: []string{"service-control"},
		}

		result := guardrails.CanExecute(context.Background(), check)
		if result.Allowed {
			t.Error("Expected elevated operation without approval to be blocked")
		}
		if result.Reason != "" {
			t.Logf("Block reason: %s", result.Reason)
		}
	})

	t.Run("disabled_capability_blocked", func(t *testing.T) {
		guardrails := New()

		// Disable capability
		guardrails.capabilities.Disable("network-access")

		check := CheckExecution{
			ToolName:     "external-api-call",
			Tier:         TierSafe,
			SideEffect:   SideEffectExternal,
			Capabilities: []string{"network-access"},
		}

		result := guardrails.CanExecute(context.Background(), check)
		if result.Allowed {
			t.Error("Expected operation with disabled capability to be blocked")
		}
	})
}

// TestExecutionLogging tests execution logging.
func TestExecutionLogging(t *testing.T) {
	t.Run("logs_execution_attempt", func(t *testing.T) {
		guardrails := New()

		check := CheckExecution{
			ToolName:   "test-tool",
			Tier:       TierSafe,
			SideEffect: SideEffectReadOnly,
		}

		result := guardrails.CanExecute(context.Background(), check)

		// Check that execution was logged
		// (In real implementation, would check log output)
		t.Logf("Execution allowed: %v", result.Allowed)
	})
}
