package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/Havens-blog/OpsAgent/internal/llm"
)

const (
	// contextCompactionThreshold 触发上下文压缩的历史消息数
	contextCompactionThreshold = 24
	// contextKeepRecent 压缩后保留的最近消息数
	contextKeepRecent = 8
)

// compactIfNeeded 在历史过长时把早期轮次摘要化，保留最近 contextKeepRecent 条。
// 摘要失败时回退到原始历史，保证不因压缩丢上下文。
func (s *Service) compactIfNeeded(ctx context.Context, messages []Message) []Message {
	if len(messages) <= contextCompactionThreshold || contextKeepRecent <= 0 {
		return messages
	}
	older := messages[:len(messages)-contextKeepRecent]
	recent := messages[len(messages)-contextKeepRecent:]

	summary, err := s.summarizeHistory(ctx, older)
	if err != nil || strings.TrimSpace(summary) == "" {
		return messages
	}

	out := make([]Message, 0, contextKeepRecent+1)
	out = append(out, Message{Role: "assistant", Content: "[对话历史摘要]\n" + summary})
	out = append(out, recent...)
	return out
}

// summarizeHistory 用一次 LLM 调用把较早的消息压成简洁摘要。
func (s *Service) summarizeHistory(ctx context.Context, older []Message) (string, error) {
	resp, err := s.client.Chat(ctx, []llm.Message{{
		Role:    "user",
		Content: buildHistorySummaryPrompt(older),
	}})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty summary response")
	}
	return resp.Choices[0].Message.Content, nil
}

// buildHistorySummaryPrompt 渲染待压缩的历史，保留角色、正文与证据 ID。
func buildHistorySummaryPrompt(older []Message) string {
	var b strings.Builder
	b.WriteString("请把下面的对话历史压缩成简洁摘要，保留关键事实、已确认结论以及提到的证据 ID。\n\n")
	for _, m := range older {
		b.WriteString("角色: ")
		b.WriteString(m.Role)
		b.WriteString("\n")
		b.WriteString(m.Content)
		if len(m.Evidence) > 0 {
			b.WriteString("\n证据ID:")
			for _, ev := range m.Evidence {
				if ev.EvidenceID != "" {
					b.WriteString(" ")
					b.WriteString(ev.EvidenceID)
				}
			}
		}
		b.WriteString("\n\n")
	}
	return b.String()
}
