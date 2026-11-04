package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/llm"
)

// summaryClient 返回固定摘要或错误的 ChatClient 桩
type summaryClient struct {
	summary string
	err     error
}

func (c *summaryClient) Chat(_ context.Context, _ []llm.Message) (*llm.ChatResponse, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.ResponseMessage{Role: "assistant", Content: c.summary}}}}, nil
}

func (c *summaryClient) ChatWithTools(_ context.Context, _ []llm.Message, _ []llm.Tool) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (c *summaryClient) ChatStream(_ context.Context, _ []llm.Message, _ func(llm.ChatChunk)) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func longHistory(n int) []Message {
	out := make([]Message, 0, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		out = append(out, Message{Role: role, Content: strings.Repeat("x", 8)})
	}
	return out
}

func TestCompactIfNeededUnderThreshold(t *testing.T) {
	svc := NewServiceWithClient(&summaryClient{summary: "不该被调用"}, "test")
	msgs := longHistory(contextCompactionThreshold)
	got := svc.compactIfNeeded(context.Background(), msgs)
	if len(got) != len(msgs) {
		t.Fatalf("len = %d, want %d (no compaction)", len(got), len(msgs))
	}
}

func TestCompactIfNeededSummarizes(t *testing.T) {
	svc := NewServiceWithClient(&summaryClient{summary: "早期结论汇总"}, "test")
	msgs := longHistory(30)
	got := svc.compactIfNeeded(context.Background(), msgs)

	if len(got) != contextKeepRecent+1 {
		t.Fatalf("len = %d, want %d (summary + recent)", len(got), contextKeepRecent+1)
	}
	if !strings.Contains(got[0].Content, "早期结论汇总") {
		t.Errorf("first message missing summary: %q", got[0].Content)
	}
	if !strings.Contains(got[0].Content, "[对话历史摘要]") {
		t.Errorf("summary message missing label: %q", got[0].Content)
	}
}

func TestCompactIfNeededErrorFallsBack(t *testing.T) {
	svc := NewServiceWithClient(&summaryClient{err: errors.New("boom")}, "test")
	msgs := longHistory(30)
	got := svc.compactIfNeeded(context.Background(), msgs)
	if len(got) != len(msgs) {
		t.Fatalf("len = %d, want %d (fallback to original)", len(got), len(msgs))
	}
}

func TestBuildHistorySummaryPromptIncludesEvidenceIDs(t *testing.T) {
	older := []Message{{
		Role:     "assistant",
		Content:  "已查询 WAF",
		Evidence: []ToolEvidence{{EvidenceID: "ev-1"}},
	}}
	prompt := buildHistorySummaryPrompt(older)
	if !strings.Contains(prompt, "已查询 WAF") || !strings.Contains(prompt, "ev-1") {
		t.Errorf("prompt missing content or evidence id: %q", prompt)
	}
}
