package chat

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

func conversation(id, title string) *Conversation {
	return &Conversation{ID: id, Title: title}
}

func message(id, role, content string) *Message {
	return &Message{ID: id, Role: role, Content: content}
}

// TestConversationStoreTenantIsolation verifies conversations and messages are
// bucketed per tenant and never leak across tenants.
func TestConversationStoreTenantIsolation(t *testing.T) {
	st := NewMemoryConversationStore()

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

	t.Run("list is tenant-scoped", func(t *testing.T) {
		a, _ := st.List(ctxA)
		if len(a) != 1 || a[0].ID != "conv-a" {
			t.Fatalf("tenant A list = %+v, want only conv-a", a)
		}
		b, _ := st.List(ctxB)
		if len(b) != 1 || b[0].ID != "conv-b" {
			t.Fatalf("tenant B list = %+v, want only conv-b", b)
		}
	})

	t.Run("cross-tenant get is not-found", func(t *testing.T) {
		if _, err := st.Get(ctxA, "conv-b"); err == nil {
			t.Error("tenant A must NOT see tenant B's conv-b")
		}
		if _, err := st.Get(ctxB, "conv-a"); err == nil {
			t.Error("tenant B must NOT see tenant A's conv-a")
		}
	})

	t.Run("cross-tenant delete is not-found", func(t *testing.T) {
		if err := st.Delete(ctxA, "conv-b"); err == nil {
			t.Error("tenant A must NOT delete tenant B's conv-b")
		}
	})

	t.Run("cross-tenant listMessages is not-found", func(t *testing.T) {
		if _, err := st.ListMessages(ctxB, "conv-a"); err == nil {
			t.Error("tenant B must NOT list tenant A's messages")
		}
	})

	t.Run("messages are returned in insertion order", func(t *testing.T) {
		msgs, err := st.ListMessages(ctxA, "conv-a")
		if err != nil {
			t.Fatalf("list messages: %v", err)
		}
		if len(msgs) != 1 || msgs[0].ID != "m-a" {
			t.Fatalf("messages = %+v, want [m-a]", msgs)
		}
	})
}

// TestConversationStoreListOrder verifies conversations sort by UpdatedAt desc.
func TestConversationStoreListOrder(t *testing.T) {
	st := NewMemoryConversationStore()
	ctx := tenant.WithID(context.Background(), 1)

	older := conversation("older", "older")
	older.UpdatedAt = time.Now().Add(-time.Hour)
	newer := conversation("newer", "newer")
	newer.UpdatedAt = time.Now()

	_ = st.Create(ctx, older)
	_ = st.Create(ctx, newer)

	list, err := st.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].ID != "newer" || list[1].ID != "older" {
		t.Fatalf("list order = %v, want [newer older]", list)
	}
}

// TestConversationStoreUpdateTitle verifies renaming within a tenant and
// cross-tenant not-found isolation.
func TestConversationStoreUpdateTitle(t *testing.T) {
	st := NewMemoryConversationStore()
	ctxA := tenant.WithID(context.Background(), 1)
	ctxB := tenant.WithID(context.Background(), 2)

	_ = st.Create(ctxA, conversation("conv-a", "old"))
	_ = st.Create(ctxB, conversation("conv-b", "B"))

	if err := st.UpdateTitle(ctxA, "conv-a", "renamed"); err != nil {
		t.Fatalf("update title: %v", err)
	}

	conv, err := st.Get(ctxA, "conv-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if conv.Title != "renamed" {
		t.Fatalf("title = %q, want renamed", conv.Title)
	}
	if conv.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set after rename")
	}

	if err := st.UpdateTitle(ctxA, "conv-b", "nope"); err == nil {
		t.Error("tenant A must NOT rename tenant B's conv-b")
	}
	if err := st.UpdateTitle(ctxB, "conv-a", "nope"); err == nil {
		t.Error("tenant B must NOT rename tenant A's conv-a")
	}
}

// TestConversationStoreAddMessageTouchesUpdatedAt verifies AddMessage bumps the
// conversation timestamp so newly active conversations surface to the top.
func TestConversationStoreAddMessageTouchesUpdatedAt(t *testing.T) {
	st := NewMemoryConversationStore()
	ctx := tenant.WithID(context.Background(), 1)

	_ = st.Create(ctx, conversation("a", "a"))
	_ = st.Create(ctx, conversation("b", "b"))
	time.Sleep(time.Millisecond)
	_ = st.AddMessage(ctx, "a", message("m1", "user", "hi"))

	list, _ := st.List(ctx)
	if list[0].ID != "a" {
		t.Fatalf("expected conv-a on top after AddMessage, got %s", list[0].ID)
	}
}
