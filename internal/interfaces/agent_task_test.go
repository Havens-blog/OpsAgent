// Package interfaces provides tests for REST API handlers.
package interfaces

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/agent"
	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

type fakeExec func(ctx context.Context, task *agent.Task) (*agent.ExecutionResult, error)

func (f fakeExec) Execute(ctx context.Context, task *agent.Task) (*agent.ExecutionResult, error) {
	return f(ctx, task)
}

type taskSubmitResponse struct {
	Task       agentTaskDTO `json:"task"`
	Duplicated bool         `json:"duplicated"`
}

type taskGetResponse struct {
	Task agentTaskDTO `json:"task"`
}

func newTaskServer(t *testing.T, agentID string, exec executableAgent) *APIServer {
	t.Helper()
	server, err := NewAPIServer(&config.Settings{
		Environment: config.EnvDevelopment,
		Server:      config.ServerConfig{Host: "localhost", Port: 8080},
	})
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}
	t.Cleanup(func() { server.Shutdown(context.Background()) })
	server.agents[agentID] = exec
	return server
}

func submitTask(t *testing.T, server *APIServer, tenantID, userID int64, agentID, description, clientTaskID string) (int, taskSubmitResponse) {
	t.Helper()
	payload, _ := json.Marshal(map[string]interface{}{
		"description":    description,
		"client_task_id": clientTaskID,
	})
	req := withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+agentID+"/tasks", bytes.NewReader(payload)), tenantID, userID)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	var resp taskSubmitResponse
	if w.Code == http.StatusCreated || w.Code == http.StatusOK {
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
	}
	return w.Code, resp
}

func getTask(t *testing.T, server *APIServer, tenantID, userID int64, id string) (int, taskGetResponse) {
	t.Helper()
	req := withIdentity(httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+id, nil), tenantID, userID)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	var resp taskGetResponse
	if w.Code == http.StatusOK {
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
	}
	return w.Code, resp
}

func waitForTaskStatus(t *testing.T, server *APIServer, tenantID, userID int64, id, want string) *AgentTask {
	t.Helper()
	ctx := tenant.WithID(context.Background(), tenantID)
	ctx = context.WithValue(ctx, eiamUIDKey, userID)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := server.taskStore.Get(ctx, id)
		if err == nil && task.Status == want {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := server.taskStore.Get(ctx, id)
	t.Fatalf("task %s did not reach status %q (got %+v)", id, want, task)
	return nil
}

func successResult() *agent.ExecutionResult {
	return &agent.ExecutionResult{
		Success:   true,
		Response:  "done",
		Data:      map[string]interface{}{"model": "test-model"},
		ToolsUsed: []string{},
		Duration:  15 * time.Millisecond,
	}
}

// TestSubmitAgentTaskFailedResultMapsToFailed guards the state machine: a call
// that returns a result with Success=false must mark the task failed, not
// completed.
func TestSubmitAgentTaskFailedResultMapsToFailed(t *testing.T) {
	server := newTaskServer(t, "coordinator", fakeExec(func(ctx context.Context, task *agent.Task) (*agent.ExecutionResult, error) {
		return &agent.ExecutionResult{
			Success:  false,
			Error:    "unable to route task to a specialist agent",
			Response: "无法将任务路由到可用专家",
		}, nil
	}))

	code, resp := submitTask(t, server, 1, 1, "coordinator", "ambiguous request", "cid-fail-1")
	if code != http.StatusCreated {
		t.Fatalf("submit status = %d, want %d", code, http.StatusCreated)
	}

	task := waitForTaskStatus(t, server, 1, 1, resp.Task.ID, TaskStatusFailed)
	if task.Error != "unable to route task to a specialist agent" {
		t.Fatalf("task error = %q, want %q", task.Error, "unable to route task to a specialist agent")
	}
}

// TestSubmitAgentTaskLifecycle covers pending -> running -> completed.
func TestSubmitAgentTaskLifecycle(t *testing.T) {
	server := newTaskServer(t, "test-agent", fakeExec(func(ctx context.Context, task *agent.Task) (*agent.ExecutionResult, error) {
		return successResult(), nil
	}))

	code, resp := submitTask(t, server, 1, 2, "test-agent", "describe logs", "ct-lifecycle")
	if code != http.StatusCreated {
		t.Fatalf("submit status = %d body %+v", code, resp)
	}
	if resp.Duplicated {
		t.Fatalf("first submit should not be duplicated")
	}
	if resp.Task.ID == "" || resp.Task.Status != TaskStatusPending {
		t.Fatalf("task = %+v", resp.Task)
	}

	task := waitForTaskStatus(t, server, 1, 2, resp.Task.ID, TaskStatusCompleted)
	if task.Result == nil || !task.Result.Success || task.Result.Response != "done" {
		t.Fatalf("result = %+v", task.Result)
	}

	code, got := getTask(t, server, 1, 2, resp.Task.ID)
	if code != http.StatusOK || got.Task.Status != TaskStatusCompleted || got.Task.Result == nil {
		t.Fatalf("get task = %d %+v", code, got.Task)
	}
}

// TestSubmitAgentTaskIdempotent verifies a duplicate client_task_id returns the
// same task and does not re-execute.
func TestSubmitAgentTaskIdempotent(t *testing.T) {
	var calls int32
	server := newTaskServer(t, "test-agent", fakeExec(func(ctx context.Context, task *agent.Task) (*agent.ExecutionResult, error) {
		atomic.AddInt32(&calls, 1)
		return successResult(), nil
	}))

	code1, first := submitTask(t, server, 1, 2, "test-agent", "describe logs", "ct-idem")
	if code1 != http.StatusCreated || first.Duplicated {
		t.Fatalf("first submit = %d %+v", code1, first)
	}
	code2, second := submitTask(t, server, 1, 2, "test-agent", "describe logs", "ct-idem")
	if code2 != http.StatusOK || !second.Duplicated {
		t.Fatalf("second submit = %d %+v", code2, second)
	}
	if second.Task.ID != first.Task.ID {
		t.Fatalf("duplicate returned different task: %s vs %s", second.Task.ID, first.Task.ID)
	}

	waitForTaskStatus(t, server, 1, 2, first.Task.ID, TaskStatusCompleted)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("agent executed %d times, want 1", got)
	}
}

// TestAgentTaskApprovalApprove covers awaiting_approval -> approve -> completed.
func TestAgentTaskApprovalApprove(t *testing.T) {
	var calls int32
	approvalReq := guardrails.ApprovalRequest{
		ToolName:      "restart_service",
		Operation:     "execute",
		Params:        map[string]interface{}{"service": "payment-service"},
		Reason:        "restart may affect traffic",
		Scope:         guardrails.ScopeOneShot,
		ExpirySeconds: 300,
	}
	server := newTaskServer(t, "test-agent", fakeExec(func(ctx context.Context, task *agent.Task) (*agent.ExecutionResult, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return nil, &guardrails.ApprovalRequiredError{Request: approvalReq}
		}
		return successResult(), nil
	}))

	code, resp := submitTask(t, server, 1, 2, "test-agent", "restart service", "ct-approve")
	if code != http.StatusCreated {
		t.Fatalf("submit status = %d", code)
	}

	task := waitForTaskStatus(t, server, 1, 2, resp.Task.ID, TaskStatusAwaitingApproval)
	if task.ApprovalID == "" {
		t.Fatalf("expected approval id, got %+v", task)
	}

	p, err := server.pendingApprovals.Get(task.ApprovalID)
	if err != nil || p.ConversationID != task.ID || p.Status != "pending" {
		t.Fatalf("pending approval = %+v err=%v", p, err)
	}

	req := withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+task.ApprovalID+"/approve", nil), 1, 2)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("approve status = %d body %s", w.Code, w.Body.String())
	}

	done := waitForTaskStatus(t, server, 1, 2, resp.Task.ID, TaskStatusCompleted)
	if done.Result == nil || !done.Result.Success {
		t.Fatalf("completed result = %+v", done.Result)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("agent executed %d times, want 2", got)
	}
}

// TestAgentTaskApprovalReject covers awaiting_approval -> rejected.
func TestAgentTaskApprovalReject(t *testing.T) {
	approvalReq := guardrails.ApprovalRequest{
		ToolName:      "restart_service",
		Operation:     "execute",
		Params:        map[string]interface{}{"service": "payment-service"},
		Reason:        "restart may affect traffic",
		Scope:         guardrails.ScopeOneShot,
		ExpirySeconds: 300,
	}
	server := newTaskServer(t, "test-agent", fakeExec(func(ctx context.Context, task *agent.Task) (*agent.ExecutionResult, error) {
		return nil, &guardrails.ApprovalRequiredError{Request: approvalReq}
	}))

	code, resp := submitTask(t, server, 1, 2, "test-agent", "restart service", "ct-reject")
	if code != http.StatusCreated {
		t.Fatalf("submit status = %d", code)
	}

	task := waitForTaskStatus(t, server, 1, 2, resp.Task.ID, TaskStatusAwaitingApproval)

	req := withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+task.ApprovalID+"/reject", nil), 1, 2)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reject status = %d body %s", w.Code, w.Body.String())
	}

	rejected := waitForTaskStatus(t, server, 1, 2, resp.Task.ID, TaskStatusRejected)
	if rejected.Error == "" {
		t.Fatalf("expected rejection error text, got %+v", rejected)
	}
}

// TestAgentTaskCrossTenant verifies GET isolation and per-tenant idempotency.
func TestAgentTaskCrossTenant(t *testing.T) {
	server := newTaskServer(t, "test-agent", fakeExec(func(ctx context.Context, task *agent.Task) (*agent.ExecutionResult, error) {
		return successResult(), nil
	}))

	code, first := submitTask(t, server, 1, 2, "test-agent", "describe logs", "ct-tenant")
	if code != http.StatusCreated {
		t.Fatalf("submit tenant 1 = %d", code)
	}

	code, _ = getTask(t, server, 2, 3, first.Task.ID)
	if code != http.StatusNotFound {
		t.Fatalf("cross-tenant get status = %d, want 404", code)
	}

	// Cross-user within the same tenant: user 99 cannot read user 2's task.
	code, _ = getTask(t, server, 1, 99, first.Task.ID)
	if code != http.StatusNotFound {
		t.Fatalf("cross-user get status = %d, want 404", code)
	}

	code2, second := submitTask(t, server, 2, 3, "test-agent", "describe logs", "ct-tenant")
	if code2 != http.StatusCreated || second.Duplicated {
		t.Fatalf("tenant 2 submit should create its own task, got %d %+v", code2, second)
	}
	if second.Task.ID == first.Task.ID {
		t.Fatalf("tenant 2 reused tenant 1 task %s", second.Task.ID)
	}

	waitForTaskStatus(t, server, 1, 2, first.Task.ID, TaskStatusCompleted)
	waitForTaskStatus(t, server, 2, 3, second.Task.ID, TaskStatusCompleted)
}
