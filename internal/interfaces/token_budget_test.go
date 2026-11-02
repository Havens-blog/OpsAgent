// Package interfaces provides tests for REST API handlers.
package interfaces

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/chat"
	"github.com/Havens-blog/OpsAgent/internal/llm"
)

type budgetRecordingClient struct {
	budget int64
}

func (c *budgetRecordingClient) Chat(ctx context.Context, messages []llm.Message) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (c *budgetRecordingClient) ChatWithTools(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (c *budgetRecordingClient) ChatStream(ctx context.Context, messages []llm.Message, handler func(llm.ChatChunk)) (*llm.ChatResponse, error) {
	c.budget = chat.TokenBudgetFrom(ctx)
	handler(llm.ChatChunk{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "ok"}}}})
	return &llm.ChatResponse{}, nil
}

type budgetToolProvider struct{}

func (p *budgetToolProvider) ToolSpecsForLLM() ([]llm.Tool, error) {
	return []llm.Tool{{Type: "function", Function: llm.FunctionDefinition{Name: "noop"}}}, nil
}

func (p *budgetToolProvider) ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{"ok": true}, nil
}

func (p *budgetToolProvider) ToolSource(name string) string {
	return "test"
}

type budgetToolLoopClient struct {
	calls  int
	budget int64
}

func (c *budgetToolLoopClient) Chat(ctx context.Context, messages []llm.Message) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (c *budgetToolLoopClient) ChatWithTools(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	c.calls++
	c.budget = chat.TokenBudgetFrom(ctx)
	return &llm.ChatResponse{
		Choices: []llm.Choice{{Message: llm.ResponseMessage{
			Role:      "assistant",
			ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "noop", Arguments: "{}"}}},
		}}},
		Usage: llm.Usage{PromptTokens: 40, CompletionTokens: 20, TotalTokens: 60},
	}, nil
}

func (c *budgetToolLoopClient) ChatStream(ctx context.Context, messages []llm.Message, handler func(llm.ChatChunk)) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func createConversationAs(t *testing.T, server *APIServer, tenantID, userID int64, title string) string {
	t.Helper()
	payload := []byte(`{}`)
	if title != "" {
		payload, _ = json.Marshal(map[string]string{"title": title})
	}
	req := withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(payload)), tenantID, userID)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create conversation status %d body %s", w.Code, w.Body.String())
	}
	var resp struct {
		Conversation chat.Conversation `json:"conversation"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal conversation: %v", err)
	}
	return resp.Conversation.ID
}

func sendMessageAs(t *testing.T, server *APIServer, tenantID, userID int64, convID, content string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"content": content})
	req := withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", bytes.NewReader(body)), tenantID, userID)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	return w
}

func TestSendMessageInjectsTenantTokenBudget(t *testing.T) {
	client := &budgetRecordingClient{}
	server := newConversationServer(t, client)
	server.tenantTokenLimit = 100
	server.usage.addLLM(1, 2, llm.Usage{TotalTokens: 40})

	id := createConversationAs(t, server, 1, 2, "budget")
	w := sendMessageAs(t, server, 1, 2, id, "hello")

	if w.Code != http.StatusOK {
		t.Fatalf("stream status %d body %s", w.Code, w.Body.String())
	}
	if client.budget != 60 {
		t.Fatalf("injected budget = %d, want 60", client.budget)
	}
}

func TestSendMessageTokenBudgetExceededEmitsError(t *testing.T) {
	client := &budgetToolLoopClient{}
	server := newConversationServer(t, nil)
	svc := chat.NewServiceWithClient(client, "test")
	svc.SetToolProvider(&budgetToolProvider{})
	server.setChatService(svc)
	server.tenantTokenLimit = 100
	server.usage.addLLM(1, 2, llm.Usage{TotalTokens: 40})

	id := createConversationAs(t, server, 1, 2, "exceed")
	w := sendMessageAs(t, server, 1, 2, id, "hello")

	if w.Code != http.StatusOK {
		t.Fatalf("stream status %d body %s", w.Code, w.Body.String())
	}

	events := parseSSE(t, w.Body.String())
	sawError := false
	for _, ev := range events {
		if ev.Type == "done" {
			t.Fatalf("unexpected done event: %+v", ev)
		}
		if ev.Type == "error" {
			sawError = true
			if ev.Message != "租户 token 用量已达上限，请稍后再试" {
				t.Fatalf("error message = %q", ev.Message)
			}
		}
	}
	if !sawError {
		t.Fatalf("events = %+v, want error event", events)
	}
	if client.calls != 1 {
		t.Fatalf("ChatWithTools calls = %d, want 1", client.calls)
	}
	if client.budget != 60 {
		t.Fatalf("injected budget = %d, want 60", client.budget)
	}
}
