package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Havens-blog/OpsAgent/internal/llm"
)

// GoalBlocker 是目标未达成时的带类型阻塞原因
type GoalBlocker string

const (
	BlockerGoalNotMetYet   GoalBlocker = "goal_not_met_yet"
	BlockerMissingEvidence GoalBlocker = "missing_evidence"
	BlockerNeedsUserInput  GoalBlocker = "needs_user_input"
	BlockerRunFailed       GoalBlocker = "run_failed"
	BlockerExternalWait    GoalBlocker = "external_wait"
)

// reason 返回阻塞原因的中文说明
func (b GoalBlocker) reason() string {
	switch b {
	case BlockerMissingEvidence:
		return "缺少证据"
	case BlockerNeedsUserInput:
		return "需要用户输入"
	case BlockerRunFailed:
		return "执行失败"
	case BlockerExternalWait:
		return "等待外部条件"
	case BlockerGoalNotMetYet:
		return "目标尚未达成"
	default:
		return string(b)
	}
}

// GoalResult 是评估器对当前目标的判定
type GoalResult struct {
	Met       bool        `json:"met"`
	Blocker   GoalBlocker `json:"blocker"`
	Reasoning string      `json:"reasoning"`
}

// parseGoalResult 容错解析评估器返回的 JSON：允许模型在前后裹上说明文字
func parseGoalResult(raw string) (GoalResult, error) {
	raw = strings.TrimSpace(raw)
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end <= start {
		return GoalResult{}, fmt.Errorf("goal evaluator returned no JSON")
	}
	var r GoalResult
	if err := json.Unmarshal([]byte(raw[start:end+1]), &r); err != nil {
		return GoalResult{}, fmt.Errorf("malformed goal result: %w", err)
	}
	r.Blocker = normalizeBlocker(string(r.Blocker))
	if r.Met {
		r.Blocker = ""
	}
	return r, nil
}

func normalizeBlocker(s string) GoalBlocker {
	switch GoalBlocker(strings.TrimSpace(s)) {
	case BlockerMissingEvidence, BlockerNeedsUserInput, BlockerRunFailed, BlockerExternalWait, BlockerGoalNotMetYet:
		return GoalBlocker(strings.TrimSpace(s))
	default:
		return BlockerGoalNotMetYet
	}
}

// goalController 控制目标续跑：仅 goal_not_met_yet 续跑，上限 maxContinuations，
// 连续 stallLimit 次相同的无进展评估熔断
type goalController struct {
	maxContinuations int
	stallLimit       int
	continuations    int
	stalls           int
	lastReasoning    string
}

func newGoalController() *goalController {
	return &goalController{maxContinuations: 8, stallLimit: 2}
}

// decide 返回是否续跑，以及停止原因（续跑时为空）
func (c *goalController) decide(r GoalResult) (bool, string) {
	if r.Met {
		return false, ""
	}
	if r.Blocker != BlockerGoalNotMetYet {
		return false, r.Blocker.reason()
	}
	c.continuations++
	if c.continuations > c.maxContinuations {
		return false, "达到最大续跑次数"
	}
	if r.Reasoning == c.lastReasoning {
		c.stalls++
	} else {
		c.stalls = 1
		c.lastReasoning = r.Reasoning
	}
	if c.stalls >= c.stallLimit {
		return false, "连续无进展熔断"
	}
	return true, ""
}

// buildGoalEvalPrompt 渲染目标评估提示词
func buildGoalEvalPrompt(goal, draft string) string {
	return fmt.Sprintf(
		"你是目标达成度评估器。目标：%s\n\n助手当前草稿：\n%s\n\n"+
			"请判断目标是否已达成，只返回 JSON：\n"+
			"{\"met\": true|false, \"blocker\": \"goal_not_met_yet|missing_evidence|needs_user_input|run_failed|external_wait\", \"reasoning\": \"一句话说明\"}",
		goal, draft,
	)
}

// evaluateGoal 调用一次 LLM 评估助手草稿是否达成当前目标
func (s *Service) evaluateGoal(ctx context.Context, draft string) (GoalResult, error) {
	resp, err := s.client.Chat(ctx, []llm.Message{{
		Role:    "user",
		Content: buildGoalEvalPrompt(s.goal, draft),
	}})
	if err != nil {
		return GoalResult{}, err
	}
	if len(resp.Choices) == 0 {
		return GoalResult{}, fmt.Errorf("empty goal evaluation response")
	}
	return parseGoalResult(resp.Choices[0].Message.Content)
}

// maybeContinueForGoal 返回是否续跑，以及续跑时注入的提示
func (s *Service) maybeContinueForGoal(ctx context.Context, draft string, ctl *goalController) (bool, string) {
	gr, err := s.evaluateGoal(ctx, draft)
	if err != nil {
		return false, ""
	}
	cont, _ := ctl.decide(gr)
	if !cont {
		return false, ""
	}
	return true, fmt.Sprintf("目标尚未达成：%s\n请继续完成：%s", gr.Reasoning, s.goal)
}
