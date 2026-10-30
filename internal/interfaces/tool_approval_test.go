// Package interfaces provides tests for REST API handlers.
package interfaces

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/chat"
	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/llm"
	"github.com/Havens-blog/OpsAgent/internal/tenant"
	"github.com/Havens-blog/OpsAgent/internal/tool"
)

type toolCallingClient struct {
	toolCalls []llm.ToolCall
	chunks    []llm.ChatChunk
}

func (c *toolCallingClient) Chat(ctx context.Context, messages []llm.Message) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (c *toolCallingClient) ChatWithTools(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	// Multi-round loop: once a tool result is already in the transcript, stop
	// requesting tools so the final answer flows out instead of looping.
	for _, m := range messages {
		if m.Role == "tool" {
			return &llm.ChatResponse{
				Choices: []llm.Choice{
					{Message: llm.ResponseMessage{Role: "assistant", Content: ""}},
				},
			}, nil
		}
	}
	return &llm.ChatResponse{
		Choices: []llm.Choice{
			{Message: llm.ResponseMessage{Role: "assistant", ToolCalls: c.toolCalls}},
		},
	}, nil
}

func (c *toolCallingClient) ChatStream(ctx context.Context, messages []llm.Message, handler func(llm.ChatChunk)) error {
	for _, ch := range c.chunks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		handler(ch)
	}
	return nil
}

func newToolServer(t *testing.T, client chat.ChatClient) *APIServer {
	t.Helper()
	server, err := NewAPIServer(&config.Settings{
		Environment: config.EnvDevelopment,
		Server:      config.ServerConfig{Host: "localhost", Port: 8080},
	})
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}
	t.Cleanup(func() { server.Shutdown(context.Background()) })
	if client != nil {
		svc := chat.NewServiceWithClient(client, "test-model")
		// Stand-in for the bound specialist tools initAgents collects on a
		// real server (there the chat registry gets the agents' BoundTools).
		testTool := &tool.Tool{
			Metadata: &tool.ToolMetadata{
				Name:        "analyze_logs",
				Description: "test analyze tool",
				Source:      tool.DataSourceTypeGeneric,
				InputSchema: []byte(`{"type":"object","properties":{"service":{"type":"string"}}}`),
				Tier:        guardrails.TierSafe,
				SideEffect:  guardrails.SideEffectReadOnly,
				Category:    "logs",
			},
			IsAvailable: func(map[string]interface{}) bool { return true },
			Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return map[string]interface{}{"status": "analyzed", "service": params["service"]}, nil
			},
		}
		svc.SetToolProvider(newRegistryToolProvider(buildChatToolRegistry([]*tool.Tool{testTool}, nil), nil, server.approvalMgr))
		server.setChatService(svc)
	}
	return server
}

func withIdentity(r *http.Request, tenantID, userID int64) *http.Request {
	ctx := tenant.WithID(r.Context(), tenantID)
	ctx = context.WithValue(ctx, eiamUIDKey, userID)
	return r.WithContext(ctx)
}

func postMessage(t *testing.T, server *APIServer, convID, content string) []sseEvent {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"content": content})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stream status %d body %s", w.Code, w.Body.String())
	}
	return parseSSE(t, w.Body.String())
}

// TestSendMessageToolCall verifies the two-pass tool-calling flow: the model
// first returns a tool call, the tool result is streamed, then the final answer.
func TestSendMessageToolCall(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{
		"service":    "order-service",
		"start_time": "2026-01-01T00:00:00Z",
		"end_time":   "2026-01-01T01:00:00Z",
	})
	client := &toolCallingClient{
		toolCalls: []llm.ToolCall{
			{ID: "call-1", Type: "function", Function: llm.FunctionCall{Name: "analyze_logs", Arguments: string(args)}},
		},
		chunks: []llm.ChatChunk{
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "分析"}}}},
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "完成"}}}},
		},
	}
	server := newToolServer(t, client)
	id := createConversation(t, server, "tool")

	events := postMessage(t, server, id, "分析日志")

	if len(events) != 5 {
		t.Fatalf("events = %+v, want 5 (tool_call, tool_result, delta, delta, done)", events)
	}
	if events[0].Type != "tool_call" || events[0].ToolName != "analyze_logs" || events[0].Status != "running" || events[0].CallID != "call-1" {
		t.Fatalf("tool_call event = %+v", events[0])
	}
	if events[1].Type != "tool_result" || events[1].ToolName != "analyze_logs" || events[1].Status != "success" || events[1].Result == "" {
		t.Fatalf("tool_result event = %+v", events[1])
	}
	if events[2].Type != "delta" || events[2].Content != "分析" {
		t.Fatalf("delta event = %+v", events[2])
	}
	if events[4].Type != "done" {
		t.Fatalf("done event = %+v", events[4])
	}

	msgs, err := server.conversationStore.ListMessages(context.Background(), id)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 || msgs[1].Content != "分析完成" {
		t.Fatalf("messages = %+v", msgs)
	}
}

// TestApprovalRequiredThenApprove verifies the approval round-trip: the first
// send stops at approval_required, and after approval the retry executes.
func TestApprovalRequiredThenApprove(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{"service": "payment-service"})
	client := &toolCallingClient{
		toolCalls: []llm.ToolCall{
			{ID: "call-restart", Type: "function", Function: llm.FunctionCall{Name: "restart_service", Arguments: string(args)}},
		},
		chunks: []llm.ChatChunk{
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "已重启"}}}},
		},
	}
	server := newToolServer(t, client)
	id := createConversation(t, server, "approval")

	first := postMessage(t, server, id, "重启服务")

	var approvalID string
	for _, ev := range first {
		if ev.Type == "approval_required" {
			approvalID = ev.ApprovalID
		}
	}
	if approvalID == "" {
		t.Fatalf("expected approval_required event, got %+v", first)
	}

	msgs, _ := server.conversationStore.ListMessages(context.Background(), id)
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("expected only the user message after approval pause, got %+v", msgs)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+approvalID+"/approve", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("approve status %d body %s", w.Code, w.Body.String())
	}

	second := postMessage(t, server, id, "重启服务")
	foundToolResult := false
	for _, ev := range second {
		if ev.Type == "tool_result" && ev.Status == "success" {
			foundToolResult = true
		}
	}
	if !foundToolResult {
		t.Fatalf("expected successful tool_result after approval, got %+v", second)
	}

	msgs, _ = server.conversationStore.ListMessages(context.Background(), id)
	if len(msgs) < 2 || msgs[len(msgs)-1].Role != "assistant" || msgs[len(msgs)-1].Content != "已重启" {
		t.Fatalf("expected assistant answer after approval, got %+v", msgs)
	}
}

// TestApprovalEndpoints covers list/approve/reject/cross-tenant/expired.
func TestApprovalEndpoints(t *testing.T) {
	server := newToolServer(t, nil)
	now := time.Now()

	seed := func(id string, tenantID, userID int64, expiresAt time.Time) {
		server.pendingApprovals.Create(&PendingApproval{
			ApprovalID:     id,
			ToolName:       "restart_service",
			Params:         map[string]interface{}{"service": "svc"},
			Reason:         "重启服务可能影响线上流量",
			ConversationID: "conv-1",
			TenantID:       tenantID,
			UserID:         userID,
			ParamHash:      guardrails.ParamHash(map[string]interface{}{"service": "svc"}),
			Scope:          guardrails.ScopeOneShot,
			ExpirySeconds:  300,
			ExpiresAt:      expiresAt,
			Status:         "pending",
			CreatedAt:      now,
		})
	}

	t.Run("list tenant pending", func(t *testing.T) {
		seed("ap-list", 1, 2, now.Add(5*time.Minute))
		req := withIdentity(httptest.NewRequest(http.MethodGet, "/api/v1/approvals", nil), 1, 2)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("list status %d body %s", w.Code, w.Body.String())
		}
		var resp struct {
			Approvals []PendingApproval `json:"approvals"`
			Count     int               `json:"count"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal list: %v", err)
		}
		if resp.Count != 1 || resp.Approvals[0].ApprovalID != "ap-list" {
			t.Fatalf("list = %+v", resp)
		}
	})

	t.Run("approve grants", func(t *testing.T) {
		seed("ap-approve", 1, 2, now.Add(5*time.Minute))
		req := withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/approvals/ap-approve/approve", nil), 1, 2)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("approve status %d body %s", w.Code, w.Body.String())
		}
		p, _ := server.pendingApprovals.Get("ap-approve")
		if p.Status != "approved" {
			t.Fatalf("status = %q", p.Status)
		}
		if err := server.approvalMgr.RequireApproval(guardrails.ApprovalRequest{
			ToolName: "restart_service", Operation: "execute",
			Params: map[string]interface{}{"service": "svc"},
			Scope:  guardrails.ScopeOneShot, SessionID: "conv-1",
		}); err != nil {
			t.Fatalf("expected granted approval, got %v", err)
		}
	})

	t.Run("reject denies", func(t *testing.T) {
		seed("ap-reject", 1, 2, now.Add(5*time.Minute))
		req := withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/approvals/ap-reject/reject", nil), 1, 2)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("reject status %d body %s", w.Code, w.Body.String())
		}
		p, _ := server.pendingApprovals.Get("ap-reject")
		if p.Status != "rejected" {
			t.Fatalf("status = %q", p.Status)
		}
		if err := server.approvalMgr.RequireApproval(guardrails.ApprovalRequest{
			ToolName: "restart_service", Operation: "execute",
			Params: map[string]interface{}{"service": "svc"},
			Scope:  guardrails.ScopeOneShot, SessionID: "conv-1",
		}); !guardrails.ApprovalRequired(err) {
			t.Fatalf("expected approval required after reject, got %v", err)
		}
	})

	t.Run("cross tenant forbidden", func(t *testing.T) {
		seed("ap-tenant", 1, 2, now.Add(5*time.Minute))
		req := withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/approvals/ap-tenant/approve", nil), 2, 2)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("cross-tenant status = %d, want 403", w.Code)
		}
	})

	t.Run("expired gone", func(t *testing.T) {
		seed("ap-expired", 1, 2, now.Add(-time.Minute))
		req := withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/approvals/ap-expired/approve", nil), 1, 2)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code != http.StatusGone {
			t.Fatalf("expired status = %d, want 410", w.Code)
		}
	})
}
