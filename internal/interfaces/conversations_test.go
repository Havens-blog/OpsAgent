// Package interfaces provides tests for REST API handlers.
package interfaces

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/chat"
	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/llm"
)

type fakeChatClient struct {
	chunks []llm.ChatChunk
	err    error
}

func (f *fakeChatClient) Chat(ctx context.Context, messages []llm.Message) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (f *fakeChatClient) ChatWithTools(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (f *fakeChatClient) ChatStream(ctx context.Context, messages []llm.Message, handler func(llm.ChatChunk)) error {
	if f.err != nil {
		return f.err
	}
	for _, c := range f.chunks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		handler(c)
	}
	return nil
}

type blockingChatClient struct{}

func (b *blockingChatClient) Chat(ctx context.Context, messages []llm.Message) (*llm.ChatResponse, error) {
	return nil, nil
}

func (b *blockingChatClient) ChatWithTools(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	return nil, nil
}

func (b *blockingChatClient) ChatStream(ctx context.Context, messages []llm.Message, handler func(llm.ChatChunk)) error {
	<-ctx.Done()
	return ctx.Err()
}

func newConversationServer(t *testing.T, client chat.ChatClient) *APIServer {
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
		server.setChatService(chat.NewServiceWithClient(client, "test-model"))
	}
	return server
}

func createConversation(t *testing.T, server *APIServer, title string) string {
	t.Helper()
	payload := []byte(`{}`)
	if title != "" {
		payload, _ = json.Marshal(map[string]string{"title": title})
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", bytes.NewReader(payload))
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

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var ev sseEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events
}

// TestConversationCRUD covers create/list/get/delete endpoints.
func TestConversationCRUD(t *testing.T) {
	server := newConversationServer(t, nil)

	t.Run("create defaults title", func(t *testing.T) {
		id := createConversation(t, server, "")
		conv, err := server.conversationStore.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get created conversation: %v", err)
		}
		if conv.Title != "新对话" {
			t.Fatalf("title = %q, want %q", conv.Title, "新对话")
		}
	})

	t.Run("list returns conversations", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("list status %d body %s", w.Code, w.Body.String())
		}
		var resp struct {
			Conversations []chat.Conversation `json:"conversations"`
			Count         int                 `json:"count"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal list: %v", err)
		}
		if resp.Count != len(resp.Conversations) {
			t.Fatalf("count %d != len %d", resp.Count, len(resp.Conversations))
		}
	})

	t.Run("get returns conversation and messages", func(t *testing.T) {
		id := createConversation(t, server, "get-me")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+id, nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("get status %d body %s", w.Code, w.Body.String())
		}
		var resp struct {
			Conversation chat.Conversation `json:"conversation"`
			Messages     []chat.Message    `json:"messages"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal get: %v", err)
		}
		if resp.Conversation.ID != id || len(resp.Messages) != 0 {
			t.Fatalf("unexpected get payload: %+v", resp)
		}
	})

	t.Run("delete removes conversation", func(t *testing.T) {
		id := createConversation(t, server, "delete-me")
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/"+id, nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("delete status %d body %s", w.Code, w.Body.String())
		}

		getReq := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+id, nil)
		getW := httptest.NewRecorder()
		server.ServeHTTP(getW, getReq)
		if getW.Code != http.StatusNotFound {
			t.Fatalf("get after delete status %d, want 404", getW.Code)
		}
	})
}

// TestRenameConversation covers PATCH /api/v1/conversations/{id}.
func TestRenameConversation(t *testing.T) {
	server := newConversationServer(t, nil)
	id := createConversation(t, server, "before")

	t.Run("renames conversation", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]string{"title": "after"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/"+id, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("patch status %d body %s", w.Code, w.Body.String())
		}
		var resp struct {
			Conversation chat.Conversation `json:"conversation"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal patch: %v", err)
		}
		if resp.Conversation.Title != "after" {
			t.Fatalf("title = %q, want after", resp.Conversation.Title)
		}
	})

	t.Run("rejects empty title", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]string{"title": "   "})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/"+id, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("patch empty title status %d, want 400", w.Code)
		}
	})

	t.Run("missing conversation is not-found", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]string{"title": "x"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/does-not-exist", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("patch missing status %d, want 404", w.Code)
		}
	})
}

// TestSendMessageStreaming verifies delta events stream in order and a done
// event ends the stream with the assistant message persisted.
func TestSendMessageStreaming(t *testing.T) {
	server := newConversationServer(t, &fakeChatClient{
		chunks: []llm.ChatChunk{
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "你"}}}},
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "好"}}}},
		},
	})
	id := createConversation(t, server, "stream")

	payload, _ := json.Marshal(map[string]string{"content": "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+id+"/messages", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("stream status %d body %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}

	events := parseSSE(t, w.Body.String())
	if len(events) != 3 {
		t.Fatalf("events = %+v, want 3 (delta, delta, done)", events)
	}
	if events[0].Type != "delta" || events[0].Content != "你" {
		t.Fatalf("event[0] = %+v", events[0])
	}
	if events[1].Type != "delta" || events[1].Content != "好" {
		t.Fatalf("event[1] = %+v", events[1])
	}
	if events[2].Type != "done" || events[2].ConversationID != id || events[2].MessageID == "" {
		t.Fatalf("event[2] = %+v", events[2])
	}

	msgs, err := server.conversationStore.ListMessages(context.Background(), id)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Fatalf("user message = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "你好" {
		t.Fatalf("assistant message = %+v", msgs[1])
	}
}

// sendMessageWithClientID POSTs a message to the handler and returns the raw
// SSE body, failing the test on a non-200 response.
func sendMessageWithClientID(t *testing.T, server *APIServer, convID, content, clientMessageID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"content":           content,
		"client_message_id": clientMessageID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("send message status %d body %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

// TestSendMessageIdempotentByClientMessageID verifies a resent message with the
// same client_message_id does not duplicate the persisted user message.
func TestSendMessageIdempotentByClientMessageID(t *testing.T) {
	server := newConversationServer(t, &fakeChatClient{
		chunks: []llm.ChatChunk{
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "你"}}}},
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "好"}}}},
		},
	})
	id := createConversation(t, server, "idempotent")

	sendMessageWithClientID(t, server, id, "hello", "cid-1")
	sendMessageWithClientID(t, server, id, "hello", "cid-1")

	msgs, err := server.conversationStore.ListMessages(context.Background(), id)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}

	users := 0
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}
		users++
		if m.ClientMessageID != "cid-1" {
			t.Fatalf("user client_message_id = %q, want cid-1", m.ClientMessageID)
		}
	}
	if users != 1 {
		t.Fatalf("user messages = %d, want 1; messages = %+v", users, msgs)
	}
	// First send persists user + assistant; the idempotent retry persists only a
	// second assistant reply, never a second user message.
	if len(msgs) != 3 {
		t.Fatalf("total messages = %d, want 3 (1 user + 2 assistant); messages = %+v", len(msgs), msgs)
	}
}

// TestSendMessageDifferentClientMessageIDAppends verifies distinct
// client_message_id values persist distinct user messages.
func TestSendMessageDifferentClientMessageIDAppends(t *testing.T) {
	server := newConversationServer(t, &fakeChatClient{
		chunks: []llm.ChatChunk{
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "你"}}}},
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "好"}}}},
		},
	})
	id := createConversation(t, server, "distinct")

	sendMessageWithClientID(t, server, id, "hello", "cid-1")
	sendMessageWithClientID(t, server, id, "hello", "cid-2")

	msgs, err := server.conversationStore.ListMessages(context.Background(), id)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}

	users := 0
	for _, m := range msgs {
		if m.Role == "user" {
			users++
		}
	}
	if users != 2 {
		t.Fatalf("user messages = %d, want 2; messages = %+v", users, msgs)
	}
}

// TestSendMessageError verifies an LLM error is reported as an SSE error event
// (HTTP remains 200) and no assistant message is persisted.
func TestSendMessageError(t *testing.T) {
	server := newConversationServer(t, &fakeChatClient{err: context.DeadlineExceeded})
	id := createConversation(t, server, "error")

	payload, _ := json.Marshal(map[string]string{"content": "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+id+"/messages", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("stream status %d body %s", w.Code, w.Body.String())
	}

	events := parseSSE(t, w.Body.String())
	if len(events) != 1 || events[0].Type != "error" || events[0].Message == "" {
		t.Fatalf("events = %+v, want one error event", events)
	}

	msgs, _ := server.conversationStore.ListMessages(context.Background(), id)
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("expected only the persisted user message, got %+v", msgs)
	}
}

// TestSendMessageCancel verifies client cancellation stops streaming without a
// done event and without persisting an assistant reply.
func TestSendMessageCancel(t *testing.T) {
	server := newConversationServer(t, &blockingChatClient{})
	id := createConversation(t, server, "cancel")

	payload, _ := json.Marshal(map[string]string{"content": "hello"})
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+id+"/messages", bytes.NewReader(payload)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		server.ServeHTTP(w, req)
		close(done)
	}()

	cancel()
	<-done

	if w.Code != http.StatusOK {
		t.Fatalf("stream status %d body %s", w.Code, w.Body.String())
	}

	events := parseSSE(t, w.Body.String())
	for _, ev := range events {
		if ev.Type == "done" || ev.Type == "error" {
			t.Fatalf("unexpected event after cancel: %+v", ev)
		}
	}

	msgs, _ := server.conversationStore.ListMessages(context.Background(), id)
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("expected only the persisted user message, got %+v", msgs)
	}
}
