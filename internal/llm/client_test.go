package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(serverURL string) *Client {
	return NewClient(&Config{
		BaseURL: serverURL,
		APIKey:  "test-key",
		Model:   "test-model",
	})
}

func collectDeltas(chunks *[]string) func(ChatChunk) {
	return func(chunk ChatChunk) {
		for _, c := range chunk.Choices {
			if c.Delta != nil && c.Delta.Content != "" {
				*chunks = append(*chunks, c.Delta.Content)
			}
		}
	}
}

func TestHandleStreamEmptyChoicesFinalChunk(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"你好"}}]}`,
		`data: {"id":"2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"世界"}}]}`,
		`data: {"id":"3","object":"chat.completion.chunk","choices":[],"usage":{"total_tokens":10}}`,
		`data: [DONE]`,
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer server.Close()

	var deltas []string
	resp, err := newTestClient(server.URL).ChatStream(context.Background(), nil, collectDeltas(&deltas))
	if err != nil {
		t.Fatalf("ChatStream returned error: %v", err)
	}
	if got := strings.Join(deltas, ""); got != "你好世界" {
		t.Fatalf("expected deltas 你好世界, got %q", got)
	}
	if resp == nil {
		t.Fatal("expected non-nil ChatResponse")
	}
	if resp.Usage.TotalTokens != 10 {
		t.Fatalf("expected TotalTokens 10, got %d", resp.Usage.TotalTokens)
	}
}

func TestHandleStreamNoSpaceAfterData(t *testing.T) {
	sse := `data:{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"ok"}}]}` + "\n\n" + "data: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer server.Close()

	var deltas []string
	_, err := newTestClient(server.URL).ChatStream(context.Background(), nil, collectDeltas(&deltas))
	if err != nil {
		t.Fatalf("ChatStream returned error: %v", err)
	}
	if got := strings.Join(deltas, ""); got != "ok" {
		t.Fatalf("expected ok, got %q", got)
	}
}

func TestHandleStreamNoValidChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	_, err := newTestClient(server.URL).ChatStream(context.Background(), nil, func(ChatChunk) {})
	if err == nil || !strings.Contains(err.Error(), "no valid response received") {
		t.Fatalf("expected no valid response received, got %v", err)
	}
}
