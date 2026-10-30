package interfaces

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/guardrails"
)

// TestFileBackedApprovalsRoundTrip verifies pending approvals survive a
// reload while rejected and expired ones are pruned at load time.
func TestFileBackedApprovalsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approvals.json")
	store, err := NewFileBackedPendingApprovalStore(path)
	if err != nil {
		t.Fatalf("NewFileBackedPendingApprovalStore: %v", err)
	}

	now := time.Now()
	seed := func(id, status string, expiresAt time.Time) {
		t.Helper()
		if err := store.Create(&PendingApproval{
			ApprovalID: id,
			ToolName:   "restart_service",
			Params:     map[string]interface{}{"service": "svc"},
			Reason:     "重启服务可能影响线上流量",
			TenantID:   1,
			UserID:     2,
			ParamHash:  guardrails.ParamHash(map[string]interface{}{"service": "svc"}),
			Scope:      guardrails.ScopeOneShot,
			ExpiresAt:  expiresAt,
			Status:     status,
			CreatedAt:  now,
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	seed("ap-pending", "pending", now.Add(5*time.Minute))
	seed("ap-rejected", "rejected", now.Add(5*time.Minute))
	seed("ap-expired", "pending", now.Add(-time.Minute))

	// Reopen from the same path: the new store reads only the persisted file,
	// never the previous in-memory copy.
	reopened, err := NewFileBackedPendingApprovalStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	pending, err := reopened.Get("ap-pending")
	if err != nil {
		t.Fatalf("get pending: %v", err)
	}
	if pending.Status != "pending" || pending.ToolName != "restart_service" || pending.Scope != guardrails.ScopeOneShot {
		t.Fatalf("pending = %+v", pending)
	}

	if _, err := reopened.Get("ap-rejected"); err == nil {
		t.Fatalf("rejected entry was not pruned at load")
	}
	if _, err := reopened.Get("ap-expired"); err == nil {
		t.Fatalf("expired entry was not pruned at load")
	}
}

// TestFileBackedApprovalsCorruptFile verifies a corrupt snapshot fails loudly.
func TestFileBackedApprovalsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approvals.json")
	if err := os.WriteFile(path, []byte("{not-valid-json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	if _, err := NewFileBackedPendingApprovalStore(path); err == nil {
		t.Fatalf("expected error for corrupt snapshot, got nil")
	}
}

// TestFileBackedApprovalsEmptyPath verifies an empty path is rejected.
func TestFileBackedApprovalsEmptyPath(t *testing.T) {
	if _, err := NewFileBackedPendingApprovalStore(""); err == nil {
		t.Fatalf("expected error for empty path, got nil")
	}
}
