package chat

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

// TestPersistentConversationStoreRoundTrip verifies that conversations and
// messages survive a reopen, and that tenant isolation still holds after load.
func TestPersistentConversationStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversations.json")
	st, err := NewPersistentConversationStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	ctxA := tenant.WithID(context.Background(), 1)
	ctxB := tenant.WithID(context.Background(), 2)

	if err := st.Create(ctxA, conversation("conv-a", "A")); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := st.Create(ctxB, conversation("conv-b", "B")); err != nil {
		t.Fatalf("create B: %v", err)
	}
	if err := st.AddMessage(ctxA, "conv-a", message("m-a", "user", "hello")); err != nil {
		t.Fatalf("add message A: %v", err)
	}

	reopened, err := NewPersistentConversationStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	a, err := reopened.List(ctxA)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(a) != 1 || a[0].ID != "conv-a" {
		t.Fatalf("tenant A list = %+v, want only conv-a", a)
	}

	b, err := reopened.List(ctxB)
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(b) != 1 || b[0].ID != "conv-b" {
		t.Fatalf("tenant B list = %+v, want only conv-b", b)
	}

	msgs, err := reopened.ListMessages(ctxA, "conv-a")
	if err != nil {
		t.Fatalf("list messages A: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "m-a" || msgs[0].Content != "hello" {
		t.Fatalf("messages A = %+v, want one m-a/hello", msgs)
	}

	if _, err := reopened.Get(ctxA, "conv-b"); err == nil {
		t.Error("tenant A must NOT see tenant B's conv-b after reload")
	}
	if _, err := reopened.ListMessages(ctxB, "conv-a"); err == nil {
		t.Error("tenant B must NOT list tenant A's messages after reload")
	}
}

// TestPersistentConversationStoreCorruptFile verifies a corrupt snapshot is
// reported as an error instead of being silently dropped.
func TestPersistentConversationStoreCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	if _, err := NewPersistentConversationStore(path); err == nil {
		t.Fatal("expected error for corrupt conversation store file")
	}
}
