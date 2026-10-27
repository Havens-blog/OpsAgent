package agent

import (
	"context"

	"github.com/Havens-blog/OpsAgent/internal/llm"
)

// stubLLM is a deterministic llm.AgentLLM for unit tests. It avoids hitting a
// real provider so tests that exercise Execute's routing logic stay fast and
// hermetic. The default-branch path (no task "type") delegates to the base
// agent's LLM call, which these tests do not want to make for real.
type stubLLM struct {
	reply string
}

func (s *stubLLM) Chat(_ context.Context, _ []llm.ClaudeMessage) (string, error) {
	return s.reply, nil
}

func (s *stubLLM) ChatWithTools(_ context.Context, _ []llm.ClaudeMessage, _ []llm.ClaudeTool) (string, error) {
	return s.reply, nil
}

// useStubLLM injects a deterministic LLM into a BaseAgent so unit tests do not
// call a real provider. Safe because tests share the agent package.
func useStubLLM(b *BaseAgent) {
	b.llmClient = &stubLLM{reply: "stub response"}
}
