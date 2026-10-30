package interfaces

import (
	"testing"
	"time"
)

func TestListPendingUserScoped(t *testing.T) {
	s := NewPendingApprovalStore()
	now := time.Now()
	exp := now.Add(5 * time.Minute)

	if err := s.Create(&PendingApproval{ApprovalID: "a1", TenantID: 3, UserID: 11, Status: "pending", ExpiresAt: exp, CreatedAt: now}); err != nil {
		t.Fatalf("create a1: %v", err)
	}
	if err := s.Create(&PendingApproval{ApprovalID: "a2", TenantID: 3, UserID: 22, Status: "pending", ExpiresAt: exp, CreatedAt: now}); err != nil {
		t.Fatalf("create a2: %v", err)
	}

	if got := s.ListPending(3, 11); len(got) != 1 || got[0].ApprovalID != "a1" {
		t.Fatalf("user 11 list = %+v, want only a1", got)
	}
	if got := s.ListPending(3, 22); len(got) != 1 || got[0].ApprovalID != "a2" {
		t.Fatalf("user 22 list = %+v, want only a2", got)
	}
	if got := s.ListPending(3, 99); len(got) != 0 {
		t.Fatalf("unknown user list = %+v, want empty", got)
	}
}
