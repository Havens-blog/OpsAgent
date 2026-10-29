// Package interfaces provides tests for restoring pending approvals.
package interfaces

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/llm"
)

// getConversationRaw GETs a conversation and returns the decoded raw fields.
func getConversationRaw(t *testing.T, server *APIServer, convID string) map[string]json.RawMessage {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+convID, nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get conversation status %d body %s", w.Code, w.Body.String())
	}
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal conversation: %v", err)
	}
	return resp
}

// TestGetConversationRestoresPendingApproval verifies a page refresh can
// recover the pending approval, and that it disappears once rejected.
func TestGetConversationRestoresPendingApproval(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{"service": "demo-service"})
	client := &toolCallingClient{
		toolCalls: []llm.ToolCall{
			{ID: "call-restart", Type: "function", Function: llm.FunctionCall{Name: "restart_service", Arguments: string(args)}},
		},
	}
	server := newToolServer(t, client)
	id := createConversation(t, server, "restore")

	events := parseSSE(t, sendMessageWithClientID(t, server, id, "请重启 demo-service", "cid-restore"))
	var approval *sseEvent
	for i := range events {
		if events[i].Type == "approval_required" {
			approval = &events[i]
		}
	}
	if approval == nil {
		t.Fatalf("expected approval_required event, got %+v", events)
	}

	resp := getConversationRaw(t, server, id)
	var pending []PendingApproval
	if err := json.Unmarshal(resp["pending_approvals"], &pending); err != nil {
		t.Fatalf("unmarshal pending_approvals: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending_approvals = %+v, want 1", pending)
	}
	p := pending[0]
	if p.ApprovalID != approval.ApprovalID || p.ToolName != approval.ToolName || p.Reason != approval.Reason {
		t.Fatalf("pending = %+v, event = %+v", p, *approval)
	}
	if !reflect.DeepEqual(p.Params, approval.Params) {
		t.Fatalf("params = %v, event params = %v", p.Params, approval.Params)
	}
	if p.ClientMessageID != "cid-restore" {
		t.Fatalf("client_message_id = %q, want cid-restore", p.ClientMessageID)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+approval.ApprovalID+"/reject", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reject status %d body %s", w.Code, w.Body.String())
	}

	resp = getConversationRaw(t, server, id)
	if got := string(resp["pending_approvals"]); got != "[]" {
		t.Fatalf("pending_approvals after reject = %s, want []", got)
	}
}

// TestGetConversationNoPendingApprovals verifies an empty JSON array is
// returned when the conversation has no approvals.
func TestGetConversationNoPendingApprovals(t *testing.T) {
	server := newConversationServer(t, nil)
	id := createConversation(t, server, "empty")

	resp := getConversationRaw(t, server, id)
	if got := string(resp["pending_approvals"]); got != "[]" {
		t.Fatalf("pending_approvals = %s, want []", got)
	}
}

// TestListPendingForConversation covers scope, status, expiry filtering and ordering.
func TestListPendingForConversation(t *testing.T) {
	store := NewPendingApprovalStore()
	now := time.Now()

	seed := func(id string, tenantID, userID int64, convID, status string, createdAt, expiresAt time.Time) {
		if err := store.Create(&PendingApproval{
			ApprovalID:     id,
			ToolName:       "restart_service",
			ConversationID: convID,
			TenantID:       tenantID,
			UserID:         userID,
			Status:         status,
			CreatedAt:      createdAt,
			ExpiresAt:      expiresAt,
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	live := now.Add(5 * time.Minute)

	seed("ap-c", 1, 2, "conv-1", "pending", now, live)
	seed("ap-b", 1, 2, "conv-1", "pending", now, live)
	seed("ap-a", 1, 2, "conv-1", "pending", now.Add(time.Second), live)
	seed("ap-first", 1, 2, "conv-1", "pending", now.Add(-time.Minute), live)
	seed("ap-other-conv", 1, 2, "conv-2", "pending", now, live)
	seed("ap-other-user", 1, 3, "conv-1", "pending", now, live)
	seed("ap-other-tenant", 9, 2, "conv-1", "pending", now, live)
	seed("ap-expired", 1, 2, "conv-1", "pending", now, now.Add(-time.Second))
	seed("ap-approved", 1, 2, "conv-1", "approved", now, live)
	seed("ap-rejected", 1, 2, "conv-1", "rejected", now, live)

	got := store.ListPendingForConversation(1, 2, "conv-1")
	ids := make([]string, 0, len(got))
	for _, p := range got {
		ids = append(ids, p.ApprovalID)
	}
	want := []string{"ap-first", "ap-b", "ap-c", "ap-a"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}

	empty := store.ListPendingForConversation(1, 2, "conv-none")
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty result = %#v, want non-nil empty slice", empty)
	}
}
