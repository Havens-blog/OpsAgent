package interfaces

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/agent"
)

// fakeInspector 记录调用次数并返回预置结果
type fakeInspector struct {
	mu     sync.Mutex
	calls  int
	result *agent.ExecutionResult
	err    error
}

func (f *fakeInspector) Execute(_ context.Context, _ *agent.Task) (*agent.ExecutionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.result, f.err
}

func (f *fakeInspector) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// startTestWorkers 启动执行池但不启动 ticker，返回停止函数（等待 worker 收尾）
func startTestWorkers(sc *inspectionScheduler) func() {
	ctx, cancel := context.WithCancel(context.Background())
	sc.jobs = make(chan inspectionJob, inspectionJobQueue)
	for i := 0; i < inspectionWorkerCount; i++ {
		sc.wg.Add(1)
		go sc.worker(ctx)
	}
	return func() {
		cancel()
		close(sc.jobs)
		sc.wg.Wait()
	}
}

// waitForRuns 轮询等待某计划的运行记录达到 n 条
func waitForRuns(t *testing.T, st *inspectionPlanStore, tid int64, planID string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runs, _ := st.runsFor(tid, planID); len(runs) >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("plan %s runs did not reach %d in time", planID, n)
}

func everyMinutePlan(id string, tid int64) *inspectionPlan {
	return &inspectionPlan{
		ID:             id,
		TenantID:       tid,
		Cron:           "* * * * *",
		InspectionType: "compliance",
		Status:         planStatusActive,
		CreatedAt:      time.Now(),
	}
}

// TestClaimDueOncePerMinute 同一分钟只触发一次，执行中不重复触发
func TestClaimDueOncePerMinute(t *testing.T) {
	st := newMemoryInspectionPlanStore()
	if err := st.create(everyMinutePlan("p1", 1)); err != nil {
		t.Fatalf("create: %v", err)
	}
	now := time.Date(2026, 11, 6, 9, 0, 10, 0, time.UTC)

	due, _ := st.claimDue(now)
	if len(due) != 1 {
		t.Fatalf("first claim = %d, want 1", len(due))
	}
	if again, _ := st.claimDue(now.Add(20 * time.Second)); len(again) != 0 {
		t.Errorf("same minute claimed twice: %d", len(again))
	}

	// 下一分钟但上次还在执行，仍不触发
	if busy, _ := st.claimDue(now.Add(time.Minute)); len(busy) != 0 {
		t.Errorf("running plan claimed again: %d", len(busy))
	}
	_ = st.complete("p1", inspectionRun{PlanID: "p1", Status: string(agent.InspectionRunUnmet)})
	if next, _ := st.claimDue(now.Add(time.Minute)); len(next) != 1 {
		t.Errorf("next minute claim = %d, want 1", len(next))
	}
}

// TestCompleteFinishesPlan 达到最大次数或满足停止条件时结束，执行失败不结束
func TestCompleteFinishesPlan(t *testing.T) {
	st := newMemoryInspectionPlanStore()

	maxed := everyMinutePlan("max", 1)
	maxed.MaxRuns = 2
	stopped := everyMinutePlan("stop", 1)
	stopped.StopCondition = "no_critical"
	_ = st.create(maxed)
	_ = st.create(stopped)

	_ = st.complete("max", inspectionRun{Status: string(agent.InspectionRunUnmet)})
	_ = st.complete("max", inspectionRun{Status: string(agent.InspectionRunUnmet)})
	_ = st.complete("stop", inspectionRun{Status: string(agent.InspectionRunFailed)})

	got := map[string]inspectionPlan{}
	for _, p := range st.list(1) {
		got[p.ID] = p
	}
	if got["max"].Status != planStatusFinished || got["max"].Runs != 2 {
		t.Errorf("max plan = %+v, want finished after 2 runs", got["max"])
	}
	if got["stop"].Status != planStatusActive {
		t.Errorf("failed run must not finish plan, got %+v", got["stop"])
	}

	_ = st.complete("stop", inspectionRun{Status: string(agent.InspectionRunSuccess)})
	for _, p := range st.list(1) {
		if p.ID == "stop" && p.Status != planStatusFinished {
			t.Errorf("stop condition met must finish plan, got %+v", p)
		}
	}
}

// TestInspectionPlanStoreRestartRecovery 重启后计划保留，中断的运行补记为 interrupted
func TestInspectionPlanStoreRestartRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plans.json")
	st, err := newFileBackedInspectionPlanStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = st.create(everyMinutePlan("p1", 3))
	if due, _ := st.claimDue(time.Now()); len(due) != 1 {
		t.Fatalf("claim = %d, want 1", len(due))
	}

	// 模拟进程在执行中退出后重启
	reopened, err := newFileBackedInspectionPlanStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	plans := reopened.list(3)
	if len(plans) != 1 || !plans[0].RunningSince.IsZero() || plans[0].Status != planStatusActive {
		t.Fatalf("plan after restart = %+v, want active and not running", plans)
	}
	runs, ok := reopened.runsFor(3, "p1")
	if !ok || len(runs) != 1 || runs[0].Status != runStatusInterrupt {
		t.Fatalf("runs after restart = %+v, want one interrupted run", runs)
	}

	// 恢复结果已落盘，再次打开不重复补记
	third, err := newFileBackedInspectionPlanStore(path)
	if err != nil {
		t.Fatalf("third open: %v", err)
	}
	if runs, _ := third.runsFor(3, "p1"); len(runs) != 1 {
		t.Errorf("recovery duplicated: %d runs", len(runs))
	}
}

// TestRunsForTenantIsolation 跨租户读取运行记录按不存在处理
func TestRunsForTenantIsolation(t *testing.T) {
	st := newMemoryInspectionPlanStore()
	_ = st.create(everyMinutePlan("p1", 1))
	if _, ok := st.runsFor(2, "p1"); ok {
		t.Error("tenant 2 must not see tenant 1 plan runs")
	}
	if len(st.list(2)) != 0 {
		t.Error("tenant 2 must not list tenant 1 plans")
	}
}

// TestClassifyScheduledRun 区分执行失败与停止条件未达成
func TestClassifyScheduledRun(t *testing.T) {
	cases := []struct {
		name string
		res  *agent.ExecutionResult
		err  error
		want string
	}{
		{"exec error", nil, errors.New("boom"), "failed"},
		{"nil result", nil, nil, "failed"},
		{"agent failure", &agent.ExecutionResult{Success: false, Error: "源未接入"}, nil, "failed"},
		{"unmet", &agent.ExecutionResult{Success: true, Data: map[string]interface{}{"run_status": agent.InspectionRunUnmet}}, nil, "unmet"},
		{"success default", &agent.ExecutionResult{Success: true}, nil, "success"},
	}
	for _, tc := range cases {
		if got, _ := classifyScheduledRun(tc.res, tc.err); got != tc.want {
			t.Errorf("%s: status = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestSchedulerTickExecutesAndRecords 时钟触发执行巡检并写入运行记录
func TestSchedulerTickExecutesAndRecords(t *testing.T) {
	st := newMemoryInspectionPlanStore()
	plan := everyMinutePlan("p1", 1)
	plan.MaxRuns = 1
	_ = st.create(plan)
	runner := &fakeInspector{result: &agent.ExecutionResult{
		Success: true,
		Data:    map[string]interface{}{"run_status": agent.InspectionRunUnmet},
	}}
	sc := &inspectionScheduler{store: st, runner: runner}
	stop := startTestWorkers(sc)
	defer stop()

	now := time.Now()
	sc.tick(context.Background(), now)
	sc.tick(context.Background(), now.Add(time.Minute))

	waitForRuns(t, st, 1, "p1", 1)
	if runner.callCount() != 1 {
		t.Errorf("runner calls = %d, want 1 (finished after max_runs)", runner.callCount())
	}
	runs, _ := st.runsFor(1, "p1")
	if len(runs) != 1 || runs[0].Status != "unmet" {
		t.Errorf("runs = %+v, want one unmet run", runs)
	}
	if p := st.list(1)[0]; p.Status != planStatusFinished {
		t.Errorf("plan status = %q, want finished", p.Status)
	}
}

// TestSchedulerWithoutInspectorRecordsFailure 巡检专家不可用时记为失败而不是静默跳过
func TestSchedulerWithoutInspectorRecordsFailure(t *testing.T) {
	st := newMemoryInspectionPlanStore()
	_ = st.create(everyMinutePlan("p1", 1))
	sc := &inspectionScheduler{store: st}
	stop := startTestWorkers(sc)
	defer stop()

	sc.tick(context.Background(), time.Now())
	waitForRuns(t, st, 1, "p1", 1)
	runs, _ := st.runsFor(1, "p1")
	if len(runs) != 1 || runs[0].Status != "failed" || runs[0].Error == "" {
		t.Errorf("runs = %+v, want one failed run with reason", runs)
	}
}

// TestInspectionPlanHandlers 创建校验、列表与运行记录接口
func TestInspectionPlanHandlers(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}

	post := func(body map[string]interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/inspection/plans", bytes.NewReader(b))
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		return w
	}

	bad := []map[string]interface{}{
		{"cron": "bad", "inspection_type": "compliance"},
		{"cron": "* * * * *", "inspection_type": "unknown"},
		{"cron": "* * * * *", "inspection_type": "compliance", "stop_condition": "whatever"},
		{"cron": "* * * * *", "inspection_type": "compliance", "max_runs": -1},
	}
	for _, body := range bad {
		if w := post(body); w.Code != http.StatusBadRequest {
			t.Errorf("body %v: status = %d, want 400", body, w.Code)
		}
	}

	w := post(map[string]interface{}{"cron": "0 9 * * 1-5", "inspection_type": "compliance", "stop_condition": "score>=90", "max_runs": 5})
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", w.Code, w.Body.String())
	}
	var created struct {
		Plan inspectionPlan `json:"plan"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Plan.ID == "" {
		t.Fatalf("decode created plan: %v (%s)", err, w.Body.String())
	}

	list := httptest.NewRecorder()
	server.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/plans", nil))
	var listed struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(list.Body.Bytes(), &listed)
	if list.Code != http.StatusOK || listed.Count != 1 {
		t.Errorf("list status=%d count=%d, want 200/1", list.Code, listed.Count)
	}

	runs := httptest.NewRecorder()
	server.ServeHTTP(runs, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/plans/"+created.Plan.ID+"/runs", nil))
	if runs.Code != http.StatusOK {
		t.Errorf("runs status = %d, want 200", runs.Code)
	}
	missing := httptest.NewRecorder()
	server.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/plans/nope/runs", nil))
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing plan runs status = %d, want 404", missing.Code)
	}
}

// TestInspectionSchedulerStartStop 调度器可启动、重复启动无副作用、停止后退出
func TestInspectionSchedulerStartStop(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}
	server.startInspectionScheduler()
	server.startInspectionScheduler()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	server.stopInspectionScheduler(ctx)
	if ctx.Err() != nil {
		t.Fatal("scheduler did not stop in time")
	}
	server.stopInspectionScheduler(context.Background())
}

// TestInspectionCreateRollsBackOnPersistFailure 创建落盘失败不得留下幽灵计划
func TestInspectionCreateRollsBackOnPersistFailure(t *testing.T) {
	st := newMemoryInspectionPlanStore()
	failErr := errors.New("disk full")
	st.persistFn = func() error { return failErr }

	if err := st.create(everyMinutePlan("p1", 1)); !errors.Is(err, failErr) {
		t.Fatalf("create err = %v, want persist failure", err)
	}
	if got := st.list(1); len(got) != 0 {
		t.Fatalf("list = %+v, want no ghost plan after failed create", got)
	}

	// 恢复后重试成功
	st.persistFn = nil
	if err := st.create(everyMinutePlan("p1", 1)); err != nil {
		t.Fatalf("retry create: %v", err)
	}
	if got := st.list(1); len(got) != 1 {
		t.Fatalf("list after retry = %d, want 1", len(got))
	}
}

// TestInspectionTickSkipsOnClaimPersistFailure claim 落盘失败时不执行任何计划
func TestInspectionTickSkipsOnClaimPersistFailure(t *testing.T) {
	st := newMemoryInspectionPlanStore()
	_ = st.create(everyMinutePlan("p1", 1))
	runner := &fakeInspector{result: &agent.ExecutionResult{Success: true, Response: "ok"}}
	sc := &inspectionScheduler{store: st, runner: runner}

	st.persistFn = func() error { return errors.New("disk full") }
	now := time.Date(2026, 11, 6, 9, 0, 10, 0, time.UTC)
	sc.tick(context.Background(), now)
	if runner.callCount() != 0 {
		t.Fatalf("runner calls = %d, want 0 when claim is not durable", runner.callCount())
	}

	// claim 失败不得把计划粘滞为执行中
	got := st.list(1)
	if len(got) != 1 || !got[0].RunningSince.IsZero() || !got[0].LastFiredAt.IsZero() {
		t.Fatalf("plan state after failed claim = %+v, want not running and not fired", got)
	}
}

// perPlanInspector 按计划 ID 返回不同延迟，用于模拟慢巡检与快巡检并发
type perPlanInspector struct {
	mu    sync.Mutex
	slow  time.Duration
	fast  time.Duration
	calls int
}

func (p *perPlanInspector) Execute(ctx context.Context, task *agent.Task) (*agent.ExecutionResult, error) {
	d := p.fast
	if strings.Contains(task.ID, "slow") {
		d = p.slow
	}
	select {
	case <-time.After(d):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return &agent.ExecutionResult{Success: true, Data: map[string]interface{}{"run_status": agent.InspectionRunSuccess}}, nil
}

// TestSchedulerSlowRunDoesNotBlockOthers 慢巡检只占用一个 worker，不阻塞其它计划的扫描与执行
func TestSchedulerSlowRunDoesNotBlockOthers(t *testing.T) {
	st := newMemoryInspectionPlanStore()
	_ = st.create(everyMinutePlan("slow", 1))
	_ = st.create(everyMinutePlan("fast", 1))

	runner := &perPlanInspector{slow: time.Second, fast: 0}
	sc := &inspectionScheduler{store: st, runner: runner}
	stop := startTestWorkers(sc)
	defer stop()

	now := time.Now()
	start := time.Now()
	sc.tick(context.Background(), now)
	// tick 应立即返回，不等待 slow 执行完（否则整个调度循环被慢巡检阻塞）
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("tick blocked for %v, want async submit", elapsed)
	}

	// fast 快速完成，slow 最终完成：证明两者并发执行，慢任务不阻塞快任务
	waitForRuns(t, st, 1, "fast", 1)
	waitForRuns(t, st, 1, "slow", 1)
}
