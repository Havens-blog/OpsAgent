package interfaces

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/agent"
	"github.com/Havens-blog/OpsAgent/internal/rca/engine"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

// fakeInvestigator 记录调查计划并返回预置结果
type fakeInvestigator struct {
	mu     sync.Mutex
	agents []string
	task   *agent.Task
	result *agent.ExecutionResult
}

func (f *fakeInvestigator) Investigate(_ context.Context, task *agent.Task, agents []string) *agent.ExecutionResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.task = task
	f.agents = append([]string(nil), agents...)
	return f.result
}

func (f *fakeInvestigator) planned() ([]string, *agent.Task) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.agents, f.task
}

func monitorOKLogFailed(rep *models.RCAReport) *agent.ExecutionResult {
	return &agent.ExecutionResult{
		Success:  false,
		Response: "[monitor]: 连接池耗尽\n\n[log_analyst] 执行失败: SLS 超时",
		Data: map[string]interface{}{
			"submissions": []*agent.TaskSubmission{
				{AgentName: "monitor", Status: "completed", Result: &agent.ExecutionResult{
					Success: true,
					Data:    map[string]interface{}{"report": rep},
				}},
				{AgentName: "log_analyst", Status: "failed", Error: "SLS 超时"},
			},
			"failed_agents": []string{"log_analyst"},
		},
	}
}

func newEmptyEngine(t *testing.T) *engine.Engine {
	t.Helper()
	eng, err := engine.NewEngine(
		engine.WithLogSource(sources.NewEmptyLogSource()),
		engine.WithMetricSource(sources.NewEmptyMetricSource()),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng
}

func testIncident(id string) *models.Incident {
	return &models.Incident{
		ID:               id,
		StartTime:        time.Now().Add(-10 * time.Minute),
		EndTime:          time.Now(),
		Severity:         "P2",
		AffectedServices: []string{"order-service"},
		Description:      "订单服务错误率升高",
		Labels:           map[string]string{"status": "running"},
	}
}

// TestInvestigateIncidentUsesExpertsAndDisclosesGaps 多专家调查复用 monitor 报告，并由程序披露失败覆盖
func TestInvestigateIncidentUsesExpertsAndDisclosesGaps(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}
	monitorRep := &models.RCAReport{Confidence: 0.8}
	fake := &fakeInvestigator{result: monitorOKLogFailed(monitorRep)}
	server.investigator = fake
	server.investigationAgents = []string{"monitor", "log_analyst"}

	inc := testIncident("inc-b21")
	report, err := server.investigateIncident(context.Background(), inc)
	if err != nil {
		t.Fatalf("investigateIncident: %v", err)
	}

	agents, task := fake.planned()
	if len(agents) != 2 || agents[0] != "monitor" || agents[1] != "log_analyst" {
		t.Errorf("planned agents = %v, want [monitor log_analyst]", agents)
	}
	if task == nil || task.Params["service"] != "order-service" || task.Params["start_time"] == "" {
		t.Errorf("task params not normalized: %+v", task)
	}
	if report.Confidence != 0.8 || report.Incident != inc {
		t.Errorf("expected monitor report bound to original incident, got %+v", report)
	}
	if !strings.Contains(report.Investigation, "连接池耗尽") {
		t.Errorf("investigation missing expert finding: %q", report.Investigation)
	}
	if !strings.Contains(report.Investigation, "log_analyst 未完成") {
		t.Errorf("investigation missing coverage gap: %q", report.Investigation)
	}
}

// TestInvestigateIncidentFallsBackWithoutMonitorReport RCA 专家无报告时回退确定性引擎，保留调查结论
func TestInvestigateIncidentFallsBackWithoutMonitorReport(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}
	server.rcaEngine = newEmptyEngine(t)
	server.investigator = &fakeInvestigator{result: &agent.ExecutionResult{
		Response: "[log_analyst]: 无异常日志",
		Data:     map[string]interface{}{"failed_agents": []string{"monitor"}},
	}}
	server.investigationAgents = []string{"monitor", "log_analyst"}

	report, err := server.investigateIncident(context.Background(), testIncident("inc-fallback"))
	if err != nil {
		t.Fatalf("investigateIncident: %v", err)
	}
	if !strings.Contains(report.Investigation, "monitor 未完成") {
		t.Errorf("fallback report lost investigation gap: %q", report.Investigation)
	}
}

// TestInvestigateIncidentSingleExpertUsesEngine 专家不足两个时不走 Coordinator
func TestInvestigateIncidentSingleExpertUsesEngine(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}
	server.rcaEngine = newEmptyEngine(t)
	fake := &fakeInvestigator{}
	server.investigator = fake
	server.investigationAgents = []string{"monitor"}

	report, err := server.investigateIncident(context.Background(), testIncident("inc-single"))
	if err != nil {
		t.Fatalf("investigateIncident: %v", err)
	}
	if agents, _ := fake.planned(); agents != nil {
		t.Errorf("investigator should not run with one expert, got %v", agents)
	}
	if report.Investigation != "" {
		t.Errorf("engine path should not carry investigation, got %q", report.Investigation)
	}
}

// TestWatchAlertWebhookRunsInvestigation 真实 webhook 入口进入多专家调查，存储结果含覆盖缺口
func TestWatchAlertWebhookRunsInvestigation(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}
	fake := &fakeInvestigator{result: monitorOKLogFailed(&models.RCAReport{Confidence: 0.6})}
	server.investigator = fake
	server.investigationAgents = []string{"monitor", "log_analyst"}

	body, _ := json.Marshal(map[string]interface{}{
		"alarm": map[string]interface{}{
			"tenantId":           "7",
			"eventId":            "evt-b21",
			"rule_name":          "HighErrorRate",
			"severity":           "P2",
			"labels":             map[string]interface{}{"service": "order-service"},
			"first_trigger_time": time.Now().Add(-5 * time.Minute).Unix(),
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/watchalert", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook status = %d, body %s", w.Code, w.Body.String())
	}

	ctx := tenant.WithID(context.Background(), 7)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rep, err := server.rcaStore.Get(ctx, "evt-b21"); err == nil && rep.Investigation != "" {
			if !strings.Contains(rep.Investigation, "log_analyst 未完成") {
				t.Fatalf("stored investigation missing gap: %q", rep.Investigation)
			}
			if agents, _ := fake.planned(); len(agents) != 2 {
				t.Fatalf("planned agents = %v, want 2", agents)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("webhook did not store an investigation result in time")
}
