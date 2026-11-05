package chat

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/llm"
)

func TestParseGoalResult(t *testing.T) {
	r, err := parseGoalResult(`说明：{"met":false,"blocker":"missing_evidence","reasoning":"还差日志"}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.Met || r.Blocker != BlockerMissingEvidence || r.Reasoning != "还差日志" {
		t.Errorf("unexpected result: %+v", r)
	}

	met, err := parseGoalResult(`{"met":true,"blocker":"","reasoning":""}`)
	if err != nil {
		t.Fatalf("parse met: %v", err)
	}
	if !met.Met {
		t.Error("expected met")
	}
}

func TestParseGoalResultNoJSON(t *testing.T) {
	if _, err := parseGoalResult("no json here"); err == nil {
		t.Error("expected error for non-JSON")
	}
}

func TestParseGoalResultUnknownBlocker(t *testing.T) {
	r, err := parseGoalResult(`{"blocker":"weird"}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.Blocker != BlockerGoalNotMetYet {
		t.Errorf("blocker = %q, want goal_not_met_yet", r.Blocker)
	}
}

func TestGoalControllerDecide(t *testing.T) {
	t.Run("non-goal blocker stops", func(t *testing.T) {
		c := newGoalController()
		cont, reason := c.decide(GoalResult{Blocker: BlockerMissingEvidence})
		if cont || reason != "缺少证据" {
			t.Errorf("cont=%v reason=%q", cont, reason)
		}
	})

	t.Run("goal_not_met_yet continues up to max", func(t *testing.T) {
		c := newGoalController()
		for i := 0; i < 8; i++ {
			cont, _ := c.decide(GoalResult{Blocker: BlockerGoalNotMetYet, Reasoning: "第" + string(rune('a'+i)) + "次"})
			if !cont {
				t.Fatalf("round %d should continue", i+1)
			}
		}
		if cont, _ := c.decide(GoalResult{Blocker: BlockerGoalNotMetYet, Reasoning: "再来"}); cont {
			t.Error("should stop after max continuations")
		}
	})

	t.Run("stall breaker after repeated no-progress", func(t *testing.T) {
		c := newGoalController()
		c.decide(GoalResult{Blocker: BlockerGoalNotMetYet, Reasoning: "same"})
		c.decide(GoalResult{Blocker: BlockerGoalNotMetYet, Reasoning: "same"})
		if cont, reason := c.decide(GoalResult{Blocker: BlockerGoalNotMetYet, Reasoning: "same"}); cont || reason != "连续无进展熔断" {
			t.Errorf("cont=%v reason=%q, want stall breaker", cont, reason)
		}
	})

	t.Run("met stops", func(t *testing.T) {
		c := newGoalController()
		if cont, _ := c.decide(GoalResult{Met: true}); cont {
			t.Error("met should stop")
		}
	})
}

func TestBuildGoalEvalPrompt(t *testing.T) {
	p := buildGoalEvalPrompt("修好编译", "草稿", []ToolEvidence{
		{ToolName: "query_logs", Status: "success", ResultDigest: map[string]interface{}{"total": 3}},
		{ToolName: "query_metrics", Status: "failed", Error: "超时"},
	})
	for _, want := range []string{"修好编译", "草稿", "query_logs [success]", `"total":3`, "query_metrics [failed] 错误: 超时"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if empty := buildGoalEvalPrompt("g", "d", nil); !strings.Contains(empty, "（无）") {
		t.Errorf("prompt without evidence should say none: %q", empty)
	}
}

func TestWithGoal(t *testing.T) {
	if GoalFrom(context.Background()) != "" {
		t.Error("goal should be empty by default")
	}
	ctx := WithGoal(context.Background(), "  跑完测试 ")
	if GoalFrom(ctx) != "跑完测试" {
		t.Errorf("GoalFrom = %q, want trimmed goal", GoalFrom(ctx))
	}
}

func TestEvaluateGoal(t *testing.T) {
	svc := NewServiceWithClient(&summaryClient{summary: `{"met":false,"blocker":"goal_not_met_yet","reasoning":"还没跑测试"}`}, "test")
	gr, _, err := svc.evaluateGoal(context.Background(), "跑完测试", "草稿", nil)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if gr.Blocker != BlockerGoalNotMetYet || gr.Reasoning != "还没跑测试" {
		t.Errorf("unexpected goal result: %+v", gr)
	}
}

// goalLoopClient 工具轮不调工具，评估按顺序返回预置结果，各调用带固定 usage
type goalLoopClient struct {
	mu        sync.Mutex
	evals     []string
	evalCalls int
	toolCalls int
}

func (c *goalLoopClient) Chat(_ context.Context, _ []llm.Message) (*llm.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	reply := c.evals[len(c.evals)-1]
	if c.evalCalls < len(c.evals) {
		reply = c.evals[c.evalCalls]
	}
	c.evalCalls++
	return &llm.ChatResponse{
		Choices: []llm.Choice{{Message: llm.ResponseMessage{Role: "assistant", Content: reply}}},
		Usage:   llm.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5},
	}, nil
}

func (c *goalLoopClient) ChatWithTools(_ context.Context, _ []llm.Message, _ []llm.Tool) (*llm.ChatResponse, error) {
	c.mu.Lock()
	c.toolCalls++
	c.mu.Unlock()
	return &llm.ChatResponse{
		Choices: []llm.Choice{{Message: llm.ResponseMessage{Role: "assistant", Content: "草稿"}}},
		Usage:   llm.Usage{PromptTokens: 6, CompletionTokens: 4, TotalTokens: 10},
	}, nil
}

func (c *goalLoopClient) ChatStream(_ context.Context, _ []llm.Message, handler func(llm.ChatChunk)) (*llm.ChatResponse, error) {
	handler(llm.ChatChunk{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "答"}}}})
	return &llm.ChatResponse{Usage: llm.Usage{PromptTokens: 12, CompletionTokens: 8, TotalTokens: 20}}, nil
}

func goalStreamSetup(evals ...string) (*Service, *goalLoopClient, StreamHandler, *[]string) {
	client := &goalLoopClient{evals: evals}
	svc := NewServiceWithClient(client, "test")
	svc.SetToolProvider(&fakeToolProvider{specs: []llm.Tool{{Type: "function", Function: llm.FunctionDefinition{Name: "noop"}}}})
	var stops []string
	h := StreamHandler{OnGoalStop: func(reason string) error {
		stops = append(stops, reason)
		return nil
	}}
	return svc, client, h, &stops
}

// TestStreamWithToolsGoalStopReportsBlocker 阻塞原因不丢弃，评估 usage 计入总量
func TestStreamWithToolsGoalStopReportsBlocker(t *testing.T) {
	svc, client, h, stops := goalStreamSetup(`{"met":false,"blocker":"missing_evidence","reasoning":"缺日志证据"}`)
	ctx := WithGoal(context.Background(), "定位根因")

	_, _, _, usage, err := svc.StreamWithTools(ctx, []Message{{Role: "user", Content: "查一下"}}, h)
	if err != nil {
		t.Fatalf("StreamWithTools: %v", err)
	}
	if client.evalCalls != 1 || client.toolCalls != 1 {
		t.Errorf("evalCalls=%d toolCalls=%d, want 1/1", client.evalCalls, client.toolCalls)
	}
	if len(*stops) != 1 || (*stops)[0] != "缺少证据：缺日志证据" {
		t.Errorf("stop reasons = %v, want [缺少证据：缺日志证据]", *stops)
	}
	// 工具轮 10 + 评估 5 + 最终流 20
	if usage.TotalTokens != 35 {
		t.Errorf("usage.TotalTokens = %d, want 35 (eval usage must be counted)", usage.TotalTokens)
	}
}

// TestStreamWithToolsGoalContinuesUntilMet 未达成时续跑，达成后停止并回传原因
func TestStreamWithToolsGoalContinuesUntilMet(t *testing.T) {
	svc, client, h, stops := goalStreamSetup(
		`{"met":false,"blocker":"goal_not_met_yet","reasoning":"还差指标"}`,
		`{"met":true,"blocker":"","reasoning":""}`,
	)
	ctx := WithGoal(context.Background(), "定位根因")

	_, _, _, usage, err := svc.StreamWithTools(ctx, []Message{{Role: "user", Content: "查一下"}}, h)
	if err != nil {
		t.Fatalf("StreamWithTools: %v", err)
	}
	if client.toolCalls != 2 || client.evalCalls != 2 {
		t.Errorf("toolCalls=%d evalCalls=%d, want 2/2", client.toolCalls, client.evalCalls)
	}
	if len(*stops) != 1 || (*stops)[0] != "目标已达成" {
		t.Errorf("stop reasons = %v, want [目标已达成]", *stops)
	}
	// 两轮工具 20 + 两次评估 10 + 最终流 20
	if usage.TotalTokens != 50 {
		t.Errorf("usage.TotalTokens = %d, want 50", usage.TotalTokens)
	}
}

// TestStreamWithToolsNoGoalSkipsEvaluation 未携带目标时不评估、不回传停止原因
func TestStreamWithToolsNoGoalSkipsEvaluation(t *testing.T) {
	svc, client, h, stops := goalStreamSetup(`{"met":true}`)

	if _, _, _, _, err := svc.StreamWithTools(context.Background(), []Message{{Role: "user", Content: "查一下"}}, h); err != nil {
		t.Fatalf("StreamWithTools: %v", err)
	}
	if client.evalCalls != 0 {
		t.Errorf("evalCalls = %d, want 0 without goal", client.evalCalls)
	}
	if len(*stops) != 0 {
		t.Errorf("stop reasons = %v, want none without goal", *stops)
	}
}
