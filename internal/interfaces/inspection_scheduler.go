package interfaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Havens-blog/OpsAgent/internal/agent"
	"github.com/Havens-blog/OpsAgent/internal/observability"
	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

const (
	// maxInspectionRuns 运行记录保留上限，超出后丢弃最旧的记录
	maxInspectionRuns = 500
	// inspectionRunTimeout 单次定时巡检的执行时限
	inspectionRunTimeout = 5 * time.Minute
	// inspectionTickInterval 调度检查间隔；按分钟去重，保证每分钟至多触发一次且不漏分钟
	inspectionTickInterval = 30 * time.Second

	planStatusActive   = "active"
	planStatusFinished = "finished"
	runStatusInterrupt = "interrupted"
)

var validInspectionTypes = map[string]bool{
	"cloud_resources": true,
	"elasticsearch":   true,
	"compliance":      true,
}

// inspectionPlan 是持久化的定时巡检计划
type inspectionPlan struct {
	ID             string    `json:"id"`
	TenantID       int64     `json:"tenant_id"`
	Cron           string    `json:"cron"`
	InspectionType string    `json:"inspection_type"`
	StopCondition  string    `json:"stop_condition,omitempty"`
	MaxRuns        int       `json:"max_runs,omitempty"`
	Runs           int       `json:"runs"`
	Status         string    `json:"status"`
	LastFiredAt    time.Time `json:"last_fired_at"`
	LastRunStatus  string    `json:"last_run_status,omitempty"`
	// RunningSince 非零表示执行中；重启时据此补记中断的运行
	RunningSince time.Time `json:"running_since"`
	CreatedAt    time.Time `json:"created_at"`
}

// inspectionRun 是一次定时巡检的运行记录
type inspectionRun struct {
	PlanID     string    `json:"plan_id"`
	TenantID   int64     `json:"tenant_id"`
	Status     string    `json:"status"` // success | unmet | failed | interrupted
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

type persistedInspectionPlans struct {
	Plans map[string]*inspectionPlan `json:"plans"`
	Runs  []inspectionRun            `json:"runs"`
}

// inspectionPlanStore 保存巡检计划与运行记录；path 非空时每次变更落盘
type inspectionPlanStore struct {
	mu    sync.Mutex
	path  string
	plans map[string]*inspectionPlan
	runs  []inspectionRun
}

func newMemoryInspectionPlanStore() *inspectionPlanStore {
	return &inspectionPlanStore{plans: make(map[string]*inspectionPlan)}
}

// newFileBackedInspectionPlanStore 加载快照并恢复重启前中断的运行
func newFileBackedInspectionPlanStore(path string) (*inspectionPlanStore, error) {
	if path == "" {
		return nil, fmt.Errorf("inspection plan store path is required")
	}
	s := &inspectionPlanStore{path: path, plans: make(map[string]*inspectionPlan)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *inspectionPlanStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read inspection plans %s: %w", s.path, err)
	}
	var snap persistedInspectionPlans
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("parse inspection plans %s: %w", s.path, err)
	}
	for id, p := range snap.Plans {
		if p != nil {
			s.plans[id] = p
		}
	}
	s.runs = snap.Runs

	s.mu.Lock()
	defer s.mu.Unlock()
	recovered := false
	now := time.Now()
	for _, p := range s.plans {
		if p.RunningSince.IsZero() {
			continue
		}
		s.appendRunLocked(inspectionRun{
			PlanID:     p.ID,
			TenantID:   p.TenantID,
			Status:     runStatusInterrupt,
			Error:      "服务重启，运行被中断",
			StartedAt:  p.RunningSince,
			FinishedAt: now,
		})
		p.RunningSince = time.Time{}
		recovered = true
	}
	if recovered {
		return s.persistLocked()
	}
	return nil
}

// persistLocked 写临时文件后原子替换，调用方须持有锁
func (s *inspectionPlanStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create inspection plan dir: %w", err)
	}
	data, err := json.MarshalIndent(persistedInspectionPlans{Plans: s.plans, Runs: s.runs}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode inspection plans: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create inspection plan temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write inspection plan temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close inspection plan temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace inspection plans: %w", err)
	}
	return nil
}

func (s *inspectionPlanStore) appendRunLocked(run inspectionRun) {
	s.runs = append(s.runs, run)
	if len(s.runs) > maxInspectionRuns {
		s.runs = append([]inspectionRun(nil), s.runs[len(s.runs)-maxInspectionRuns:]...)
	}
}

func (s *inspectionPlanStore) create(p *inspectionPlan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *p
	s.plans[p.ID] = &cp
	return s.persistLocked()
}

// list 返回租户下的计划副本，按创建时间排序
func (s *inspectionPlanStore) list(tid int64) []inspectionPlan {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]inspectionPlan, 0, len(s.plans))
	for _, p := range s.plans {
		if p.TenantID == tid {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// runsFor 返回租户下某计划的运行记录；跨租户访问按不存在处理
func (s *inspectionPlanStore) runsFor(tid int64, planID string) ([]inspectionRun, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[planID]
	if !ok || p.TenantID != tid {
		return nil, false
	}
	out := make([]inspectionRun, 0)
	for _, r := range s.runs {
		if r.PlanID == planID {
			out = append(out, r)
		}
	}
	return out, true
}

// claimDue 取出当前分钟到期且未在执行的计划，并标记执行中以防重复触发
func (s *inspectionPlanStore) claimDue(now time.Time) ([]inspectionPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	minute := now.Truncate(time.Minute)
	var due []inspectionPlan
	for _, p := range s.plans {
		if p.Status != planStatusActive || !p.RunningSince.IsZero() {
			continue
		}
		if !p.LastFiredAt.IsZero() && p.LastFiredAt.Truncate(time.Minute).Equal(minute) {
			continue
		}
		if !(&agent.InspectionSchedule{Cron: p.Cron}).Matches(now) {
			continue
		}
		p.LastFiredAt = now
		p.RunningSince = now
		due = append(due, *p)
	}
	if len(due) == 0 {
		return nil, nil
	}
	return due, s.persistLocked()
}

// complete 记录一次运行结果并推进计划状态
func (s *inspectionPlanStore) complete(planID string, run inspectionRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[planID]
	if !ok {
		return fmt.Errorf("inspection plan %s not found", planID)
	}
	p.RunningSince = time.Time{}
	p.Runs++
	p.LastRunStatus = run.Status
	if planFinished(p, run.Status) {
		p.Status = planStatusFinished
	}
	s.appendRunLocked(run)
	return s.persistLocked()
}

// planFinished 达到最大次数，或设置了停止条件且本次满足时结束计划；
// 执行失败不结束计划，留给下一次触发重试
func planFinished(p *inspectionPlan, status string) bool {
	if p.MaxRuns > 0 && p.Runs >= p.MaxRuns {
		return true
	}
	return p.StopCondition != "" && status == string(agent.InspectionRunSuccess)
}

// inspectionScheduler 按时钟驱动执行到期的巡检计划
type inspectionScheduler struct {
	store  *inspectionPlanStore
	runner executableAgent
	logger *observability.Logger
}

// startInspectionScheduler 启动定时巡检调度器，重复调用无副作用
func (s *APIServer) startInspectionScheduler() {
	s.inspectionMu.Lock()
	defer s.inspectionMu.Unlock()
	if s.inspectionPlans == nil || s.inspectionStop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	sc := &inspectionScheduler{store: s.inspectionPlans, runner: s.agents["inspector"], logger: s.logger}
	s.inspectionStop, s.inspectionDone = cancel, done
	go func() {
		defer close(done)
		sc.run(ctx)
	}()
}

// stopInspectionScheduler 停止调度器并等待进行中的巡检收尾，最多等到 ctx 截止
func (s *APIServer) stopInspectionScheduler(ctx context.Context) {
	s.inspectionMu.Lock()
	stop, done := s.inspectionStop, s.inspectionDone
	s.inspectionStop, s.inspectionDone = nil, nil
	s.inspectionMu.Unlock()
	if stop == nil {
		return
	}
	stop()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// run 每隔 inspectionTickInterval 检查一次到期计划，ctx 取消后退出
func (sc *inspectionScheduler) run(ctx context.Context) {
	ticker := time.NewTicker(inspectionTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			sc.tick(ctx, t)
		}
	}
}

// tick 并行执行本分钟到期的计划并等待全部完成
func (sc *inspectionScheduler) tick(ctx context.Context, now time.Time) {
	due, err := sc.store.claimDue(now)
	if err != nil {
		sc.warn("巡检计划状态落盘失败", err)
	}
	var wg sync.WaitGroup
	for _, p := range due {
		wg.Add(1)
		go func(p inspectionPlan) {
			defer wg.Done()
			run := sc.execute(ctx, p, now)
			if err := sc.store.complete(p.ID, run); err != nil {
				sc.warn("巡检运行记录写入失败", err)
			}
		}(p)
	}
	wg.Wait()
}

func (sc *inspectionScheduler) execute(ctx context.Context, p inspectionPlan, now time.Time) inspectionRun {
	run := inspectionRun{PlanID: p.ID, TenantID: p.TenantID, StartedAt: now}
	if sc.runner == nil {
		run.Status = string(agent.InspectionRunFailed)
		run.Error = "巡检专家不可用"
		run.FinishedAt = time.Now()
		return run
	}

	rctx, cancel := context.WithTimeout(tenant.WithID(ctx, p.TenantID), inspectionRunTimeout)
	defer cancel()
	res, err := sc.runner.Execute(rctx, &agent.Task{
		ID:          fmt.Sprintf("insp-%s-%d", p.ID, now.Unix()),
		Description: "定时巡检 " + p.InspectionType,
		Params: map[string]interface{}{
			"type":            "inspection",
			"inspection_type": p.InspectionType,
			"stop_condition":  p.StopCondition,
			"max_runs":        p.MaxRuns,
		},
		CreatedAt: now,
		Status:    "pending",
	})
	run.Status, run.Error = classifyScheduledRun(res, err)
	run.FinishedAt = time.Now()
	return run
}

// classifyScheduledRun 区分执行失败与停止条件未达成
func classifyScheduledRun(res *agent.ExecutionResult, err error) (string, string) {
	if err != nil {
		return string(agent.InspectionRunFailed), err.Error()
	}
	if res == nil {
		return string(agent.InspectionRunFailed), "巡检无结果"
	}
	if !res.Success {
		msg := res.Error
		if msg == "" {
			msg = res.Response
		}
		return string(agent.InspectionRunFailed), msg
	}
	switch v := res.Data["run_status"].(type) {
	case agent.InspectionRunStatus:
		return string(v), ""
	case string:
		if v != "" {
			return v, ""
		}
	}
	return string(agent.InspectionRunSuccess), ""
}

func (sc *inspectionScheduler) warn(msg string, err error) {
	if sc.logger != nil {
		sc.logger.Warn(msg, observability.Err(err))
	}
}

type createInspectionPlanRequest struct {
	Cron           string `json:"cron"`
	InspectionType string `json:"inspection_type"`
	StopCondition  string `json:"stop_condition,omitempty"`
	MaxRuns        int    `json:"max_runs,omitempty"`
}

// createInspectionPlanHandler handles POST /api/v1/inspection/plans.
func (s *APIServer) createInspectionPlanHandler(w http.ResponseWriter, r *http.Request) {
	var req createInspectionPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
		return
	}
	req.Cron = strings.TrimSpace(req.Cron)
	req.StopCondition = strings.TrimSpace(req.StopCondition)
	if err := agent.ValidateCron(req.Cron); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_cron", "Invalid cron expression", err.Error())
		return
	}
	if !validInspectionTypes[req.InspectionType] {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_inspection_type",
			"inspection_type must be cloud_resources, elasticsearch or compliance", "")
		return
	}
	if err := agent.ValidateStopCondition(req.StopCondition); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_stop_condition", "Invalid stop condition", err.Error())
		return
	}
	if req.MaxRuns < 0 {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_max_runs", "max_runs must be >= 0", "")
		return
	}

	plan := &inspectionPlan{
		ID:             uuid.New().String(),
		TenantID:       tenant.IDFrom(r.Context()),
		Cron:           req.Cron,
		InspectionType: req.InspectionType,
		StopCondition:  req.StopCondition,
		MaxRuns:        req.MaxRuns,
		Status:         planStatusActive,
		CreatedAt:      time.Now(),
	}
	if err := s.inspectionPlans.create(plan); err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "store_error", "Failed to persist inspection plan", err.Error())
		return
	}
	s.writeJSONResponse(w, http.StatusCreated, map[string]interface{}{"plan": plan})
}

// listInspectionPlansHandler handles GET /api/v1/inspection/plans.
func (s *APIServer) listInspectionPlansHandler(w http.ResponseWriter, r *http.Request) {
	plans := s.inspectionPlans.list(tenant.IDFrom(r.Context()))
	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{"plans": plans, "count": len(plans)})
}

// listInspectionRunsHandler handles GET /api/v1/inspection/plans/{id}/runs.
func (s *APIServer) listInspectionRunsHandler(w http.ResponseWriter, r *http.Request) {
	runs, ok := s.inspectionPlans.runsFor(tenant.IDFrom(r.Context()), r.PathValue("id"))
	if !ok {
		s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Inspection plan not found", "")
		return
	}
	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{"runs": runs, "count": len(runs)})
}
