package rca

import (
	"context"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

func report(id string) *models.RCAReport {
	return &models.RCAReport{
		Incident: &models.Incident{ID: id, Labels: map[string]string{"status": "running"}},
	}
}

// TestStoreTenantIsolation verifies that reports are bucketed per tenant and
// never leak across tenants: list sees only own tenant, get/updateStatus of
// another tenant's report returns not-found (no existence leak).
func TestStoreTenantIsolation(t *testing.T) {
	st := NewMemoryRCAReportStore()

	ctxA := tenant.WithID(context.Background(), 1)
	ctxB := tenant.WithID(context.Background(), 2)

	if err := st.Store(ctxA, report("inc-a")); err != nil {
		t.Fatalf("store A: %v", err)
	}
	if err := st.Store(ctxB, report("inc-b")); err != nil {
		t.Fatalf("store B: %v", err)
	}

	t.Run("list is tenant-scoped", func(t *testing.T) {
		a, _ := st.List(ctxA)
		if len(a) != 1 || a[0].Incident.ID != "inc-a" {
			t.Fatalf("tenant A list = %+v, want only inc-a", a)
		}
		b, _ := st.List(ctxB)
		if len(b) != 1 || b[0].Incident.ID != "inc-b" {
			t.Fatalf("tenant B list = %+v, want only inc-b", b)
		}
	})

	t.Run("cross-tenant get is not-found", func(t *testing.T) {
		if _, err := st.Get(ctxA, "inc-b"); err == nil {
			t.Error("tenant A must NOT see tenant B's report inc-b")
		}
		if _, err := st.Get(ctxB, "inc-a"); err == nil {
			t.Error("tenant B must NOT see tenant A's report inc-a")
		}
		// own-tenant get works
		if _, err := st.Get(ctxA, "inc-a"); err != nil {
			t.Errorf("tenant A must see its own report: %v", err)
		}
	})

	t.Run("cross-tenant updateStatus is not-found", func(t *testing.T) {
		if err := st.UpdateStatus(ctxA, "inc-b", "completed"); err == nil {
			t.Error("tenant A must NOT update tenant B's report")
		}
		// own-tenant update works and does not bleed into the other tenant
		if err := st.UpdateStatus(ctxA, "inc-a", "completed"); err != nil {
			t.Errorf("own-tenant update failed: %v", err)
		}
		rb, _ := st.Get(ctxB, "inc-b")
		if rb.Incident.Labels["status"] != "running" {
			t.Errorf("tenant B report status was mutated across tenants: %v", rb.Incident.Labels)
		}
	})

	t.Run("same incident id in different tenants are distinct", func(t *testing.T) {
		_ = st.Store(ctxA, report("dup"))
		_ = st.Store(ctxB, report("dup"))
		_ = st.UpdateStatus(ctxA, "dup", "completed")
		ra, _ := st.Get(ctxA, "dup")
		rb, _ := st.Get(ctxB, "dup")
		if ra.Incident.Labels["status"] != "completed" {
			t.Errorf("tenant A dup status = %v, want completed", ra.Incident.Labels["status"])
		}
		if rb.Incident.Labels["status"] != "running" {
			t.Errorf("tenant B dup status = %v, want running (isolated)", rb.Incident.Labels["status"])
		}
	})

	t.Run("tenant 0 default bucket works when no tenant set", func(t *testing.T) {
		ctx0 := context.Background() // no tenant => bucket 0
		if err := st.Store(ctx0, report("inc-0")); err != nil {
			t.Fatalf("store tenant 0: %v", err)
		}
		got, err := st.Get(ctx0, "inc-0")
		if err != nil || got.Incident.ID != "inc-0" {
			t.Fatalf("tenant 0 get failed: %v", err)
		}
		// tenant 0 cannot see tenant 1's data
		if _, err := st.Get(ctx0, "inc-a"); err == nil {
			t.Error("tenant 0 must NOT see tenant 1's report")
		}
	})
}
