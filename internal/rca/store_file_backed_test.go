package rca

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

// TestFileBackedRCAReportStoreRoundTrip verifies reports survive a reload and
// remain tenant-isolated: both tenants read back their own report with the
// same fields, and cross-tenant Get stays not-found.
func TestFileBackedRCAReportStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rca.json")
	st, err := NewFileBackedRCAReportStore(path)
	if err != nil {
		t.Fatalf("NewFileBackedRCAReportStore: %v", err)
	}

	ctxA := tenant.WithID(context.Background(), 1)
	ctxB := tenant.WithID(context.Background(), 2)

	ra := report("inc-a")
	ra.Incident.Description = "order-service latency"
	ra.Confidence = 0.87
	ra.Warnings = []string{"logs: partial"}

	rb := report("inc-b")
	rb.Incident.Severity = "P2"

	if err := st.Store(ctxA, ra); err != nil {
		t.Fatalf("store tenant A: %v", err)
	}
	if err := st.Store(ctxB, rb); err != nil {
		t.Fatalf("store tenant B: %v", err)
	}

	// Reopen from the same path: the new store reads only the persisted file,
	// never the previous in-memory copy.
	reopened, err := NewFileBackedRCAReportStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	gotA, err := reopened.Get(ctxA, "inc-a")
	if err != nil {
		t.Fatalf("get tenant A report: %v", err)
	}
	if gotA.Incident.ID != "inc-a" || gotA.Incident.Description != "order-service latency" ||
		gotA.Confidence != 0.87 || len(gotA.Warnings) != 1 || gotA.Warnings[0] != "logs: partial" {
		t.Fatalf("tenant A report = %+v, fields did not round-trip", gotA)
	}

	gotB, err := reopened.Get(ctxB, "inc-b")
	if err != nil {
		t.Fatalf("get tenant B report: %v", err)
	}
	if gotB.Incident.ID != "inc-b" || gotB.Incident.Severity != "P2" {
		t.Fatalf("tenant B report = %+v, fields did not round-trip", gotB)
	}

	if _, err := reopened.Get(ctxA, "inc-b"); err == nil {
		t.Error("tenant A must NOT see tenant B's report inc-b")
	}
	if _, err := reopened.Get(ctxB, "inc-a"); err == nil {
		t.Error("tenant B must NOT see tenant A's report inc-a")
	}
}

// TestFileBackedRCAReportStoreCorruptFile verifies a corrupt snapshot fails
// loudly instead of silently discarding existing data.
func TestFileBackedRCAReportStoreCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rca.json")
	if err := os.WriteFile(path, []byte("{not-valid-json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	if _, err := NewFileBackedRCAReportStore(path); err == nil {
		t.Fatalf("expected error for corrupt snapshot, got nil")
	}
}

// TestFileBackedRCAReportStoreEmptyPath verifies an empty path is rejected.
func TestFileBackedRCAReportStoreEmptyPath(t *testing.T) {
	if _, err := NewFileBackedRCAReportStore(""); err == nil {
		t.Fatalf("expected error for empty path, got nil")
	}
}
