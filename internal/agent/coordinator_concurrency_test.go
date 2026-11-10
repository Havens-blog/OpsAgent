package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/llm"
	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

// cancelLLM 在 Chat 里阻塞到 ctx 取消，用于验证综合继承取消
type cancelLLM struct {
	mu    sync.Mutex
	calls int
}

func (c *cancelLLM) Chat(ctx context.Context, _ []llm.ClaudeMessage) (string, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	<-ctx.Done()
	return "", ctx.Err()
}

func (c *cancelLLM) ChatWithTools(_ context.Context, _ []llm.ClaudeMessage, _ []llm.ClaudeTool) (string, []string, error) {
	return "", nil, nil
}

func (c *cancelLLM) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

var evidenceIDPattern = regexp.MustCompile(`证据ID: ([0-9a-f]+)`)

// evidenceLLM 读取提示词里的证据 ID，按 mode 返回不同形态的综合输出
type evidenceLLM struct {
	mode string
}

func (e *evidenceLLM) Chat(_ context.Context, msgs []llm.ClaudeMessage) (string, error) {
	var ids []string
	for _, m := range evidenceIDPattern.FindAllStringSubmatch(msgs[0].Content, -1) {
		ids = append(ids, m[1])
	}
	switch e.mode {
	case "prose":
		return "根因就是数据库", nil
	case "fake":
		return `{"verdict":"determined","root_cause_claim":0,"claims":[{"text":"y","evidence_ids":["deadbeef00000000"]}]}`, nil
	case "single":
		if len(ids) < 1 {
			return "{}", nil
		}
		return fmt.Sprintf(`{"verdict":"determined","root_cause_claim":0,"claims":[{"text":"CPU 打满","evidence_ids":[%q]}]}`, ids[0]), nil
	case "unbound":
		// 真实证据 ID，但根因命题缺失（root_cause_claim 无效）
		if len(ids) < 2 {
			return "{}", nil
		}
		return fmt.Sprintf(`{"verdict":"determined","root_cause_claim":-1,"claims":[{"text":"CPU 打满","evidence_ids":[%q]},{"text":"慢查询暴增","evidence_ids":[%q]}]}`, ids[0], ids[1]), nil
	case "root_single":
		// 根因 claim 只引用单一专家，另一个专家只出现在非根因 claim 上
		if len(ids) < 2 {
			return "{}", nil
		}
		return fmt.Sprintf(`{"verdict":"determined","root_cause_claim":0,"claims":[{"text":"CPU 打满","evidence_ids":[%q]},{"text":"慢查询暴增","evidence_ids":[%q]}]}`, ids[0], ids[1]), nil
	case "conflict":
		// 两个真实专家 ID，但根因命题标记 conflict=true
		if len(ids) < 2 {
			return "{}", nil
		}
		return fmt.Sprintf(`{"verdict":"determined","root_cause_claim":2,"claims":[{"text":"CPU 打满","evidence_ids":[%q]},{"text":"慢查询暴增","evidence_ids":[%q]},{"text":"慢查询导致 CPU 打满","evidence_ids":[%q,%q],"conflict":true}]}`, ids[0], ids[1], ids[0], ids[1]), nil
	default:
		if len(ids) < 2 {
			return "{}", nil
		}
		return fmt.Sprintf(`说明如下：{"verdict":"determined","root_cause_claim":2,"claims":[{"text":"CPU 打满","evidence_ids":[%q]},{"text":"慢查询暴增","evidence_ids":[%q]},{"text":"慢查询导致 CPU 打满","evidence_ids":[%q,%q]}]}`, ids[0], ids[1], ids[0], ids[1]), nil
	}
}

func (e *evidenceLLM) ChatWithTools(_ context.Context, _ []llm.ClaudeMessage, _ []llm.ClaudeTool) (string, []string, error) {
	return "", nil, nil
}

// fakeSpecialist 是可控结果的专家桩，用于验证 Coordinator 的委派与汇总逻辑
type fakeSpecialist struct {
	name     string
	response string
	fail     bool
	err      error
	calls    int
	mu       sync.Mutex
}

func (f *fakeSpecialist) Execute(_ context.Context, _ *Task) (*ExecutionResult, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return &ExecutionResult{
		Success:   !f.fail,
		Response:  f.response,
		ToolsUsed: []string{f.name + "_tool"},
	}, nil
}

func (f *fakeSpecialist) GetConfig() *AgentConfig {
	return &AgentConfig{Name: f.name, Type: AgentTypeMonitor, Description: f.name}
}

func (f *fakeSpecialist) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newTestCoordinator(t *testing.T, specialists ...*fakeSpecialist) *CoordinatorAgent {
	t.Helper()
	settings := &config.Settings{
		Environment:   config.EnvDevelopment,
		LLM:           config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
		Observability: config.ObservabilityConfig{LogLevel: "info", LogFormat: "json"},
		Security:      config.SecurityConfig{EnableAuditLog: false},
		DataSources:   make(map[string]interface{}),
	}
	coord, err := NewCoordinatorAgent(settings)
	if err != nil {
		t.Fatalf("NewCoordinatorAgent failed: %v", err)
	}
	t.Cleanup(func() { coord.Shutdown(context.Background()) })
	for _, s := range specialists {
		if err := coord.RegisterAgent(s.name, s); err != nil {
			t.Fatalf("RegisterAgent(%s) failed: %v", s.name, err)
		}
	}
	return coord
}

func newDelegationTask(id string) *Task {
	return &Task{ID: id, Description: "test", Params: map[string]interface{}{}, CreatedAt: time.Now(), Status: "pending"}
}

// TestDelegateTaskConcurrentUniqueIDs 并发委派时提交 ID 必须唯一，且状态全部写完
func TestDelegateTaskConcurrentUniqueIDs(t *testing.T) {
	spec := &fakeSpecialist{name: "monitor", response: "ok"}
	coord := newTestCoordinator(t, spec)
	task := newDelegationTask("concurrent")

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := coord.DelegateTask(context.Background(), "monitor", task); err != nil {
				t.Errorf("DelegateTask failed: %v", err)
			}
		}()
	}
	wg.Wait()

	statuses := coord.GetTaskStatus(task.ID)
	if len(statuses) != n {
		t.Fatalf("expected %d submissions, got %d", n, len(statuses))
	}
	seen := make(map[string]bool, n)
	for _, s := range statuses {
		if seen[s.TaskID] {
			t.Fatalf("duplicate submission id %s", s.TaskID)
		}
		seen[s.TaskID] = true
		if s.Status != "completed" {
			t.Errorf("submission %s status = %q, want completed", s.TaskID, s.Status)
		}
	}
}

// TestDelegateTasksAsyncKeepsFailures 并行委派对每个委派都返回一个提交，含未注册专家
func TestDelegateTasksAsyncKeepsFailures(t *testing.T) {
	ok := &fakeSpecialist{name: "monitor", response: "cpu 打满"}
	coord := newTestCoordinator(t, ok)

	subs := coord.DelegateTasksAsync(context.Background(), []TaskDelegation{
		{AgentName: "monitor", Task: newDelegationTask("p-monitor")},
		{AgentName: "ghost", Task: newDelegationTask("p-ghost")},
	})
	if len(subs) != 2 {
		t.Fatalf("expected 2 submissions, got %d", len(subs))
	}
	if subs[0].AgentName != "monitor" || !subs[0].succeeded() {
		t.Errorf("expected monitor to succeed first, got %+v", subs[0])
	}
	if subs[1].AgentName != "ghost" || subs[1].Status != "failed" || subs[1].Error == "" {
		t.Errorf("expected ghost failed submission with error, got %+v", subs[1])
	}
}

// TestExecutePlanParallelPartialResult 并行时一个专家失败，其他专家的结论必须保留
func TestExecutePlanParallelPartialResult(t *testing.T) {
	ok := &fakeSpecialist{name: "monitor", response: "订单服务 P99 飙升"}
	bad := &fakeSpecialist{name: "log_analyst", err: errors.New("SLS 超时")}
	coord := newTestCoordinator(t, ok, bad)
	task := newDelegationTask("parallel")

	plan := &ExecutionPlan{
		Strategy: "parallel",
		Delegations: []TaskDelegation{
			coord.newDelegation("monitor", "monitor", task, nil),
			coord.newDelegation("log_analyst", "log_analyst", task, nil),
		},
	}
	result := coord.executePlan(context.Background(), task, plan)

	if result.Success {
		t.Error("expected overall failure when one specialist fails")
	}
	if !strings.Contains(result.Response, "订单服务 P99 飙升") {
		t.Errorf("successful specialist output dropped: %q", result.Response)
	}
	if !strings.Contains(result.Response, "SLS 超时") {
		t.Errorf("failure reason missing: %q", result.Response)
	}
	if partial, _ := result.Data["partial"].(bool); !partial {
		t.Error("expected Data[partial]=true")
	}
	failed, _ := result.Data["failed_agents"].([]string)
	if len(failed) != 1 || failed[0] != "log_analyst" {
		t.Errorf("failed_agents = %v, want [log_analyst]", failed)
	}
	if len(result.ToolsUsed) != 1 || result.ToolsUsed[0] != "monitor_tool" {
		t.Errorf("ToolsUsed = %v, want [monitor_tool]", result.ToolsUsed)
	}
}

// TestExecutePlanSequentialStopsButKeepsEarlier 顺序执行遇失败即停，但保留前面的结果
func TestExecutePlanSequentialStopsButKeepsEarlier(t *testing.T) {
	first := &fakeSpecialist{name: "monitor", response: "发现异常"}
	failing := &fakeSpecialist{name: "log_analyst", response: "日志源未配置", fail: true}
	never := &fakeSpecialist{name: "inspector", response: "不该执行"}
	coord := newTestCoordinator(t, first, failing, never)
	task := newDelegationTask("sequential")

	plan := &ExecutionPlan{
		Strategy: "sequential",
		Delegations: []TaskDelegation{
			coord.newDelegation("monitor", "monitor", task, nil),
			coord.newDelegation("log_analyst", "log_analyst", task, nil),
			coord.newDelegation("inspector", "inspector", task, nil),
		},
	}
	result := coord.executePlan(context.Background(), task, plan)

	if result.Success {
		t.Error("expected failure")
	}
	if never.callCount() != 0 {
		t.Error("inspector must not run after an earlier failure")
	}
	if !strings.Contains(result.Response, "发现异常") || !strings.Contains(result.Response, "日志源未配置") {
		t.Errorf("expected earlier result and failure reason, got %q", result.Response)
	}
}

// TestExecutePlanAllSucceed 全部成功时整体成功且非 partial
func TestExecutePlanAllSucceed(t *testing.T) {
	a := &fakeSpecialist{name: "monitor", response: "A"}
	b := &fakeSpecialist{name: "log_analyst", response: "B"}
	coord := newTestCoordinator(t, a, b)
	task := newDelegationTask("all-ok")

	plan := &ExecutionPlan{
		Strategy: "parallel",
		Delegations: []TaskDelegation{
			coord.newDelegation("monitor", "monitor", task, nil),
			coord.newDelegation("log_analyst", "log_analyst", task, nil),
		},
	}
	result := coord.executePlan(context.Background(), task, plan)

	if !result.Success || result.Error != "" {
		t.Fatalf("expected success, got success=%v error=%q", result.Success, result.Error)
	}
	if partial, _ := result.Data["partial"].(bool); partial {
		t.Error("expected Data[partial]=false")
	}
	if result.Response != "[monitor]: A\n\n[log_analyst]: B" {
		t.Errorf("unexpected response order/format: %q", result.Response)
	}
	if result.Data["synthesized"] != false {
		t.Errorf("synthesized = %v, want false without an LLM", result.Data["synthesized"])
	}
	if n, _ := result.Data["evidence_count"].(int); n != 2 {
		t.Errorf("evidence_count = %v, want 2", result.Data["evidence_count"])
	}
}

// TestExecutePlanSynthesizesWithLLM 有 LLM 且多名专家成功时，用一次 LLM 综合结论
func TestExecutePlanSynthesizesWithLLM(t *testing.T) {
	a := &fakeSpecialist{name: "monitor", response: "CPU 打满"}
	b := &fakeSpecialist{name: "log_analyst", response: "慢查询暴增"}
	coord := newTestCoordinator(t, a, b)
	coord.llmClient = &evidenceLLM{mode: "cross"}
	task := newDelegationTask("synth")

	plan := &ExecutionPlan{
		Strategy: "parallel",
		Delegations: []TaskDelegation{
			coord.newDelegation("monitor", "monitor", task, nil),
			coord.newDelegation("log_analyst", "log_analyst", task, nil),
		},
	}
	result := coord.executePlan(context.Background(), task, plan)

	if !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if !strings.Contains(result.Response, "根因结论：慢查询导致 CPU 打满") || !strings.Contains(result.Response, "证据：") {
		t.Errorf("response = %q, want evidence-bound synthesized conclusion", result.Response)
	}
	if result.Data["synthesized"] != true {
		t.Errorf("synthesized = %v, want true", result.Data["synthesized"])
	}
	if n, _ := result.Data["evidence_count"].(int); n != 2 {
		t.Errorf("evidence_count = %v, want 2", result.Data["evidence_count"])
	}
}

// TestRecordEvidencePrefersStructuredFinding 证据优先采用专家结构化结论
func TestRecordEvidencePrefersStructuredFinding(t *testing.T) {
	coord := newTestCoordinator(t)
	task := newDelegationTask("inc-1")

	sub := &TaskSubmission{
		AgentName: "monitor",
		Status:    "completed",
		Result: &ExecutionResult{
			Success:  true,
			Response: "完整的 RCA 报告正文",
			Finding:  &StructuredFinding{Conclusion: "订单服务 P99 飙升", Confidence: 0.8},
		},
	}
	id := coord.recordEvidence(context.Background(), task, sub)
	ev, ok := coord.evidence.Get(id)
	if !ok {
		t.Fatalf("evidence %q not found", id)
	}
	if ev.Description != "订单服务 P99 飙升" {
		t.Errorf("evidence description = %q, want structured conclusion", ev.Description)
	}
	if ev.Confidence != 0.8 {
		t.Errorf("evidence confidence = %v, want 0.8", ev.Confidence)
	}
}

// TestRecordEvidenceFallsBackToResponse 无 Finding 时回退到完整响应
func TestRecordEvidenceFallsBackToResponse(t *testing.T) {
	coord := newTestCoordinator(t)
	task := newDelegationTask("inc-2")

	sub := &TaskSubmission{
		AgentName: "log_analyst",
		Status:    "completed",
		Result:    &ExecutionResult{Success: true, Response: "完整日志分析"},
	}
	id := coord.recordEvidence(context.Background(), task, sub)
	ev, ok := coord.evidence.Get(id)
	if !ok || ev.Description != "完整日志分析" {
		t.Fatalf("expected fallback to response, got %+v", ev)
	}
}

// TestRecordEvidenceSeparatesIncidents 不同事件不同结论不得串到同一条证据
func TestRecordEvidenceSeparatesIncidents(t *testing.T) {
	coord := newTestCoordinator(t)

	mk := func(conclusion string, conf float64) *TaskSubmission {
		return &TaskSubmission{
			AgentName: "monitor",
			Status:    "completed",
			Result: &ExecutionResult{
				Success: true,
				Finding: &StructuredFinding{Conclusion: conclusion, Confidence: conf},
			},
		}
	}

	idA := coord.recordEvidence(context.Background(), newDelegationTask("inc-A"), mk("CPU 饱和", 0.7))
	idB := coord.recordEvidence(context.Background(), newDelegationTask("inc-B"), mk("连接池耗尽", 0.9))
	if idA == idB {
		t.Fatal("两个事件不得共享证据 ID")
	}
	evA, _ := coord.evidence.Get(idA)
	evB, _ := coord.evidence.Get(idB)
	if evA.IncidentID != "inc-A" || evA.Description != "CPU 饱和" || evA.Confidence != 0.7 {
		t.Errorf("evA = %+v", evA)
	}
	if evB.IncidentID != "inc-B" || evB.Description != "连接池耗尽" || evB.Confidence != 0.9 {
		t.Errorf("evB = %+v", evB)
	}

	// 相同输入幂等，复用同一证据 ID
	if id := coord.recordEvidence(context.Background(), newDelegationTask("inc-A"), mk("CPU 饱和", 0.7)); id != idA {
		t.Errorf("幂等写入应复用 ID，got %q want %q", id, idA)
	}
}

// TestExecutePlanSingleExpertSkipsSynthesis 单专家不额外发起 LLM 综合，直接回传结论
func TestExecutePlanSingleExpertSkipsSynthesis(t *testing.T) {
	a := &fakeSpecialist{name: "monitor", response: "唯一结论"}
	coord := newTestCoordinator(t, a)
	coord.llmClient = &stubLLM{reply: "不该被使用"}
	task := newDelegationTask("single")

	plan := &ExecutionPlan{
		Strategy: "sequential",
		Delegations: []TaskDelegation{
			coord.newDelegation("monitor", "monitor", task, nil),
		},
	}
	result := coord.executePlan(context.Background(), task, plan)

	if result.Response != "[monitor]: 唯一结论" {
		t.Errorf("response = %q, want expert conclusion without synthesis", result.Response)
	}
	if result.Data["synthesized"] != false {
		t.Errorf("synthesized = %v, want false for single expert", result.Data["synthesized"])
	}
}

func twoOKSubs() []*TaskSubmission {
	return []*TaskSubmission{
		{AgentName: "monitor", Status: "completed", Result: &ExecutionResult{Success: true, Response: "A"}},
		{AgentName: "log_analyst", Status: "completed", Result: &ExecutionResult{Success: true, Response: "B"}},
	}
}

// TestSynthesizeSkipsCancelledParent 父 context 已取消时不再发起综合 LLM 调用
func TestSynthesizeSkipsCancelledParent(t *testing.T) {
	llmClient := &cancelLLM{}
	coord := newTestCoordinator(t)
	coord.llmClient = llmClient

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	outcome := coord.synthesize(ctx, newDelegationTask("t"), twoOKSubs(), []string{"[monitor]: A\n\n[log_analyst]: B"}, nil)
	if outcome.Synthesized {
		t.Error("cancelled parent must not synthesize")
	}
	if outcome.Text != "[monitor]: A\n\n[log_analyst]: B" {
		t.Errorf("text = %q, want deterministic fallback", outcome.Text)
	}
	if llmClient.callCount() != 0 {
		t.Errorf("LLM called %d times, want 0", llmClient.callCount())
	}
}

// TestSynthesizeHonorsParentCancellation 综合进行中取消父 context，应立即回退而不是跑满 30 秒
func TestSynthesizeHonorsParentCancellation(t *testing.T) {
	llmClient := &cancelLLM{}
	coord := newTestCoordinator(t)
	coord.llmClient = llmClient

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var outcome synthesisOutcome
	go func() {
		outcome = coord.synthesize(ctx, newDelegationTask("t"), twoOKSubs(), []string{"parts"}, nil)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("synthesize did not return after parent cancellation")
	}
	if outcome.Synthesized {
		t.Error("should fall back to deterministic on cancellation")
	}
	if outcome.Text != "parts" {
		t.Errorf("text = %q, want fallback parts", outcome.Text)
	}
}

// TestGetTaskStatusReturnsSnapshot 返回快照，外部改动不污染协调器内部状态
func TestGetTaskStatusReturnsSnapshot(t *testing.T) {
	spec := &fakeSpecialist{name: "monitor", response: "ok"}
	coord := newTestCoordinator(t, spec)
	task := newDelegationTask("snap")

	if _, err := coord.DelegateTask(context.Background(), "monitor", task); err != nil {
		t.Fatalf("DelegateTask: %v", err)
	}

	first := coord.GetTaskStatus(task.ID)
	if len(first) != 1 {
		t.Fatalf("expected 1 submission, got %d", len(first))
	}
	first[0].Status = "tampered"
	if first[0].Result != nil {
		first[0].Result.Success = false
		first[0].Result.Response = "tampered"
	}

	second := coord.GetTaskStatus(task.ID)
	if second[0].Status == "tampered" {
		t.Error("returned snapshot shared internal status")
	}
	if second[0].Result == nil || !second[0].Result.Success {
		t.Errorf("internal result should stay intact, got %+v", second[0].Result)
	}
	if second[0].Result != nil && second[0].Result.Response == "tampered" {
		t.Error("returned snapshot shared internal result")
	}
}

// TestGetTaskStatusDeepCopiesNestedResult 快照深拷贝 Data/Finding/ToolsUsed/Metadata
func TestGetTaskStatusDeepCopiesNestedResult(t *testing.T) {
	coord := newTestCoordinator(t)
	task := newDelegationTask("deep")

	coord.mu.Lock()
	coord.subTasks[task.ID] = []*TaskSubmission{{
		TaskID: "sub-deep",
		Status: "completed",
		Result: &ExecutionResult{
			Success:   true,
			Response:  "ok",
			Data:      map[string]interface{}{"confidence": 0.9},
			ToolsUsed: []string{"query_logs"},
			Metadata:  map[string]interface{}{"duration": "1s"},
			Finding:   &StructuredFinding{Conclusion: "cpu 打满", Confidence: 0.8, EvidenceIDs: []string{"ev-1"}},
		},
	}}
	coord.mu.Unlock()

	snap := coord.GetTaskStatus(task.ID)[0]
	snap.Result.ToolsUsed[0] = "tampered"
	snap.Result.Finding.Conclusion = "tampered"
	snap.Result.Finding.EvidenceIDs[0] = "tampered"
	snap.Result.Data["confidence"] = 0.1
	snap.Result.Data["extra"] = "tampered"
	snap.Result.Metadata["duration"] = "tampered"

	internal := coord.GetTaskStatus(task.ID)[0]
	if internal.Result.ToolsUsed[0] != "query_logs" {
		t.Errorf("ToolsUsed polluted: %v", internal.Result.ToolsUsed)
	}
	if internal.Result.Finding.Conclusion != "cpu 打满" {
		t.Errorf("Finding.Conclusion polluted: %q", internal.Result.Finding.Conclusion)
	}
	if internal.Result.Finding.EvidenceIDs[0] != "ev-1" {
		t.Errorf("Finding.EvidenceIDs polluted: %v", internal.Result.Finding.EvidenceIDs)
	}
	if internal.Result.Data["confidence"] != 0.9 {
		t.Errorf("Data polluted: %v", internal.Result.Data)
	}
	if _, ok := internal.Result.Data["extra"]; ok {
		t.Errorf("Data leaked extra key: %v", internal.Result.Data)
	}
	if internal.Result.Metadata["duration"] != "1s" {
		t.Errorf("Metadata polluted: %v", internal.Result.Metadata)
	}
}

// TestRecordEvidenceIsolatesTenants 跨租户同事件 ID 证据隔离
func TestRecordEvidenceIsolatesTenants(t *testing.T) {
	coord := newTestCoordinator(t)
	mk := func() *TaskSubmission {
		return &TaskSubmission{
			AgentName: "monitor",
			Status:    "completed",
			Result: &ExecutionResult{
				Success: true,
				Finding: &StructuredFinding{Conclusion: "连接池耗尽", Confidence: 0.9},
			},
		}
	}

	ctxA := tenant.WithID(context.Background(), 1)
	ctxB := tenant.WithID(context.Background(), 2)

	idA := coord.recordEvidence(ctxA, newDelegationTask("ext-42"), mk())
	idB := coord.recordEvidence(ctxB, newDelegationTask("ext-42"), mk())
	if idA == idB {
		t.Fatal("跨租户同事件 ID 不得共享证据 ID")
	}
	evA, _ := coord.evidence.Get(idA)
	evB, _ := coord.evidence.Get(idB)
	if evA.TenantID != 1 || evB.TenantID != 2 {
		t.Errorf("tenant not preserved: %+v / %+v", evA, evB)
	}
}
