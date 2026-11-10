package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// runTwoExperts 两专家并行执行，可选追加一个失败专家
func runTwoExperts(t *testing.T, mode string, withFailure bool) (*CoordinatorAgent, *ExecutionResult) {
	t.Helper()
	specs := []*fakeSpecialist{
		{name: "monitor", response: "CPU 打满"},
		{name: "log_analyst", response: "慢查询暴增"},
	}
	if withFailure {
		specs = append(specs, &fakeSpecialist{name: "inspector", err: errors.New("巡检源未接入")})
	}
	coord := newTestCoordinator(t, specs...)
	coord.llmClient = &evidenceLLM{mode: mode}
	task := newDelegationTask("gate-" + mode)

	plan := &ExecutionPlan{Strategy: "parallel"}
	for _, s := range specs {
		plan.Delegations = append(plan.Delegations, coord.newDelegation(s.name, s.name, task, nil))
	}
	return coord, coord.executePlan(context.Background(), task, plan)
}

// TestSynthesisSingleSourceIsUndetermined 只引用单一专家证据时不得认定根因
func TestSynthesisSingleSourceIsUndetermined(t *testing.T) {
	_, result := runTwoExperts(t, "single", false)
	if result.Data["synthesized"] != true {
		t.Fatalf("synthesized = %v, want true", result.Data["synthesized"])
	}
	if !strings.Contains(result.Response, "根因结论：未确定") {
		t.Errorf("response = %q, want undetermined verdict", result.Response)
	}
	if strings.Contains(result.Response, "根因结论：CPU 打满") {
		t.Errorf("single-source claim must not be crowned root cause: %q", result.Response)
	}
}

// TestSynthesisRejectsFabricatedEvidence 引用不存在的证据 ID 时拒绝综合，回退确定性拼接
func TestSynthesisRejectsFabricatedEvidence(t *testing.T) {
	_, result := runTwoExperts(t, "fake", false)
	if result.Data["synthesized"] != false {
		t.Errorf("synthesized = %v, want false for fabricated evidence", result.Data["synthesized"])
	}
	if result.Response != "[monitor]: CPU 打满\n\n[log_analyst]: 慢查询暴增" {
		t.Errorf("response = %q, want deterministic fallback", result.Response)
	}
}

// TestSynthesisRejectsUnstructuredOutput 非结构化输出不能直接呈现给用户
func TestSynthesisRejectsUnstructuredOutput(t *testing.T) {
	_, result := runTwoExperts(t, "prose", false)
	if result.Data["synthesized"] != false {
		t.Errorf("synthesized = %v, want false for prose output", result.Data["synthesized"])
	}
	if strings.Contains(result.Response, "根因就是数据库") {
		t.Errorf("unverified prose leaked into response: %q", result.Response)
	}
}

// TestSynthesisAlwaysDisclosesFailedCoverage 综合成功时仍由程序附加失败覆盖
func TestSynthesisAlwaysDisclosesFailedCoverage(t *testing.T) {
	_, result := runTwoExperts(t, "cross", true)
	if result.Data["synthesized"] != true {
		t.Fatalf("synthesized = %v, want true", result.Data["synthesized"])
	}
	if !strings.Contains(result.Response, "根因结论：慢查询导致 CPU 打满") {
		t.Errorf("response missing cross-evidence verdict: %q", result.Response)
	}
	if !strings.Contains(result.Response, "⚠️ 取证覆盖不完整：inspector 未完成") {
		t.Errorf("response missing coverage gap: %q", result.Response)
	}
}

// TestAggregateBackfillsEvidenceIDs 专家 Finding 回填本次证据 ID
func TestAggregateBackfillsEvidenceIDs(t *testing.T) {
	_, result := runTwoExperts(t, "cross", false)
	subs, _ := result.Data["submissions"].([]*TaskSubmission)
	ids, _ := result.Data["evidence_ids"].([]string)
	if len(subs) != 2 || len(ids) != 2 {
		t.Fatalf("submissions=%d evidence_ids=%d, want 2/2", len(subs), len(ids))
	}
	for i, sub := range subs {
		f := sub.Result.Finding
		if f == nil || len(f.EvidenceIDs) != 1 || f.EvidenceIDs[0] != ids[i] {
			t.Errorf("submission %s finding = %+v, want evidence %s", sub.AgentName, f, ids[i])
		}
	}
}

// TestBuildSynthesisPromptIncludesCoverage 综合输入包含证据 ID、置信度、时间窗与失败覆盖
func TestBuildSynthesisPromptIncludesCoverage(t *testing.T) {
	task := newDelegationTask("p")
	task.Params["service"] = "order-service"
	task.Params["start_time"] = "2026-11-05T10:00:00Z"
	task.Params["end_time"] = "2026-11-05T10:30:00Z"

	ok := &TaskSubmission{AgentName: "monitor", Result: &ExecutionResult{
		Success: true,
		Finding: &StructuredFinding{Conclusion: "CPU 打满", Confidence: 0.7},
	}}
	bad := &TaskSubmission{AgentName: "log_analyst", Status: "failed", Error: "SLS 超时"}

	prompt := buildSynthesisPrompt(task, []*TaskSubmission{ok}, []*TaskSubmission{bad}, map[*TaskSubmission]string{ok: "abc123"})
	for _, want := range []string{"证据ID: abc123", "置信度 0.70", "order-service", "2026-11-05T10:00:00Z", "未完成的取证", "log_analyst：SLS 超时"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// TestSynthesisRootCauseMustBindToClaim 根因命题缺失（root_cause_claim 无效）时，
// 即使两个专家都提供了真实证据，也不得作为确定结论呈现，应回退确定性拼接。
func TestSynthesisRootCauseMustBindToClaim(t *testing.T) {
	_, result := runTwoExperts(t, "unbound", false)
	if result.Data["synthesized"] != false {
		t.Errorf("synthesized = %v, want false for unbound root cause", result.Data["synthesized"])
	}
	if strings.Contains(result.Response, "根因结论：") && !strings.Contains(result.Response, "根因结论：未确定") {
		t.Errorf("unbound root cause must not be rendered as determined: %q", result.Response)
	}
}

// TestSynthesisRootCauseNeedsOwnCrossEvidence 根因命题只引用单一专家时，
// 即使其他 claim 覆盖了第二个专家，也不得认定为确定根因。
func TestSynthesisRootCauseNeedsOwnCrossEvidence(t *testing.T) {
	_, result := runTwoExperts(t, "root_single", false)
	if result.Data["synthesized"] != true {
		t.Fatalf("synthesized = %v, want true (claims are valid)", result.Data["synthesized"])
	}
	if !strings.Contains(result.Response, "根因结论：未确定") {
		t.Errorf("response = %q, want undetermined for single-source root cause", result.Response)
	}
	if strings.Contains(result.Response, "根因结论：CPU 打满") {
		t.Errorf("single-source root cause must not be crowned: %q", result.Response)
	}
}

// TestSynthesisConflictIsUndetermined 根因命题标记冲突时，即使引用两个专家也不得确定
func TestSynthesisConflictIsUndetermined(t *testing.T) {
	_, result := runTwoExperts(t, "conflict", false)
	if result.Data["synthesized"] != true {
		t.Fatalf("synthesized = %v, want true (claims are valid)", result.Data["synthesized"])
	}
	if !strings.Contains(result.Response, "根因结论：未确定") {
		t.Errorf("response = %q, want undetermined for conflicting root cause", result.Response)
	}
	if strings.Contains(result.Response, "根因结论：慢查询导致 CPU 打满") {
		t.Errorf("conflicting root cause must not be crowned: %q", result.Response)
	}
}
