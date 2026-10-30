package interfaces

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFileBackedAgentTaskStoreRoundTrip verifies completed and
// awaiting_approval tasks survive a reload unchanged, while a running task is
// rewritten to failed with the interrupted-by-restart error.
func TestFileBackedAgentTaskStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	store, err := NewFileBackedAgentTaskStore(path)
	if err != nil {
		t.Fatalf("NewFileBackedAgentTaskStore: %v", err)
	}

	ctx := withIdentity(httptest.NewRequest(http.MethodGet, "/", nil), 1, 2).Context()

	seed := func(task *AgentTask) {
		t.Helper()
		if err := store.Create(ctx, task); err != nil {
			t.Fatalf("create %s: %v", task.ID, err)
		}
	}

	seed(&AgentTask{
		ID:          "t-completed",
		AgentID:     "test-agent",
		Description: "finished",
		Status:      TaskStatusCompleted,
		Result:      successResult(),
	})
	seed(&AgentTask{
		ID:          "t-awaiting",
		AgentID:     "test-agent",
		Description: "needs approval",
		Status:      TaskStatusAwaitingApproval,
		ApprovalID:  "ap-1",
	})
	seed(&AgentTask{
		ID:          "t-running",
		AgentID:     "test-agent",
		Description: "in flight",
		Status:      TaskStatusRunning,
	})

	beforeReopen := time.Now()
	reopened, err := NewFileBackedAgentTaskStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	completed, err := reopened.Get(ctx, "t-completed")
	if err != nil {
		t.Fatalf("get completed: %v", err)
	}
	if completed.Status != TaskStatusCompleted || completed.Result == nil || !completed.Result.Success {
		t.Fatalf("completed = %+v", completed)
	}

	awaiting, err := reopened.Get(ctx, "t-awaiting")
	if err != nil {
		t.Fatalf("get awaiting: %v", err)
	}
	if awaiting.Status != TaskStatusAwaitingApproval || awaiting.ApprovalID != "ap-1" {
		t.Fatalf("awaiting = %+v", awaiting)
	}

	running, err := reopened.Get(ctx, "t-running")
	if err != nil {
		t.Fatalf("get running: %v", err)
	}
	if running.Status != TaskStatusFailed {
		t.Fatalf("running status = %q, want %q", running.Status, TaskStatusFailed)
	}
	if !strings.Contains(running.Error, "interrupted by server restart") {
		t.Fatalf("running error = %q", running.Error)
	}
	if running.UpdatedAt.Before(beforeReopen) {
		t.Fatalf("running updated_at not refreshed: %v", running.UpdatedAt)
	}
}

// TestFileBackedAgentTaskStoreCorruptFile verifies a corrupt snapshot fails
// loudly instead of silently starting empty.
func TestFileBackedAgentTaskStoreCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, []byte("{not-valid-json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	if _, err := NewFileBackedAgentTaskStore(path); err == nil {
		t.Fatalf("expected error for corrupt snapshot, got nil")
	}
}

// TestFileBackedAgentTaskStoreEmptyPath verifies an empty path is rejected.
func TestFileBackedAgentTaskStoreEmptyPath(t *testing.T) {
	if _, err := NewFileBackedAgentTaskStore(""); err == nil {
		t.Fatalf("expected error for empty path, got nil")
	}
}
