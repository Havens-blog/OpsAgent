package chat

import (
	"context"
	"strings"
	"testing"
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
	p := buildGoalEvalPrompt("修好编译", "草稿")
	if !strings.Contains(p, "修好编译") || !strings.Contains(p, "草稿") {
		t.Errorf("prompt missing goal or draft: %q", p)
	}
}

func TestEvaluateGoal(t *testing.T) {
	svc := NewServiceWithClient(&summaryClient{summary: `{"met":false,"blocker":"goal_not_met_yet","reasoning":"还没跑测试"}`}, "test")
	svc.SetGoal("跑完测试")
	gr, err := svc.evaluateGoal(context.Background(), "草稿")
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if gr.Blocker != BlockerGoalNotMetYet || gr.Reasoning != "还没跑测试" {
		t.Errorf("unexpected goal result: %+v", gr)
	}
}
