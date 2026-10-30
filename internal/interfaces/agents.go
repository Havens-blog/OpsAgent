// Package interfaces provides HTTP handlers for the OpsAgent API.
package interfaces

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Havens-blog/OpsAgent/internal/agent"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/observability"
	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

// executableAgent is the narrow interface the API layer needs: run a task.
// Both CoordinatorAgent and the specialist agents satisfy it via BaseAgent.
type executableAgent interface {
	Execute(ctx context.Context, task *agent.Task) (*agent.ExecutionResult, error)
}

// agentEntry pairs an executable agent with display metadata.
type agentEntry struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	Description  string          `json:"description"`
	Capabilities []string        `json:"capabilities"`
	exec         executableAgent `json:"-"`
}

// initAgents constructs the coordinator and specialist agents and registers
// them. Construction failures are non-fatal: the server still starts and the
// affected agent is simply absent from /agents (mirrors chatService handling).
func (s *APIServer) initAgents() {
	s.agents = make(map[string]executableAgent)
	s.agentMeta = make(map[string]*agentEntry)

	register := func(id, name, typ, desc string, caps []string, exec executableAgent, err error) executableAgent {
		if err != nil {
			s.logger.Warn("Agent not available",
				observability.String("agent", id),
				observability.Err(err),
			)
			return nil
		}
		s.agents[id] = exec
		s.agentMeta[id] = &agentEntry{
			ID: id, Name: name, Type: typ, Description: desc, Capabilities: caps, exec: exec,
		}
		return exec
	}

	// Specialist agents.
	logAnalyst, laErr := agent.NewLogAnalystAgent(s.settings)
	register("log_analyst", "LogAnalyst", "log_analyst",
		"日志模式识别与错误关联分析",
		[]string{"log_analysis", "pattern_detection", "error_correlation"},
		wrapLogAnalyst(logAnalyst), laErr)

	// Build RCA data sources with progressive enhancement: real integrations
	// where credentials are present (from env), mock for every other
	// dimension. The system is always functional and upgrades per-dimension as
	// credentials are supplied.
	dsCfg := agent.NewDataSourceConfigFromEnv()
	rcaSources, active := agent.CreateRCASourcesWithFallback(dsCfg)
	s.dataSourceStatus = active
	// Share the change source with /api/v1/changes. changeSourceReal gates the
	// handler: when the dimension fell back to the honest empty source (no
	// K8S_*/e-cam credentials), the endpoint returns an empty list with a hint
	// and NEVER queries the fallback — no mock record may reach the console.
	s.changeSource = rcaSources.ChangeSource
	s.changeSourceReal = active["changes"]
	s.logger.Info("RCA data sources configured",
		observability.Bool("metrics_real", active["metrics"]),
		observability.Bool("topology_real", active["topology"]),
		observability.Bool("changes_real", active["changes"]),
		observability.Bool("logs_real", active["logs"]),
	)

	monitor, mErr := agent.NewMonitorAgent(s.settings, rcaSources)
	register("monitor", "Monitor", "monitor",
		"APM 指标分析与根因分析",
		[]string{"rca", "apm", "metrics", "traces", "topology"},
		wrapMonitor(monitor), mErr)

	inspector, iErr := agent.NewInspectorAgent(s.settings)
	register("inspector", "Inspector", "inspector",
		"云资源巡检与合规检查",
		[]string{"inspection", "compliance_check", "resource_audit"},
		wrapInspector(inspector), iErr)

	// Coordinator orchestrates the specialists.
	coordinator, cErr := agent.NewCoordinatorAgent(s.settings)
	if cErr == nil {
		// Register specialists for delegation (best-effort).
		if logAnalyst != nil {
			_ = coordinator.RegisterAgent("log_analyst", logAnalyst)
		}
		if monitor != nil {
			_ = coordinator.RegisterAgent("monitor", monitor)
		}
		if inspector != nil {
			_ = coordinator.RegisterAgent("inspector", inspector)
		}
	}
	register("coordinator", "Coordinator", "coordinator",
		"任务调度、委派与结论汇总",
		[]string{"orchestration", "delegation", "aggregation"},
		coordinator, cErr)
}

// wrapLogAnalyst/Monitor/Inspector return nil (typed) when the agent is nil, so
// a failed construction stays nil through the executableAgent interface rather
// than becoming a non-nil interface wrapping a nil pointer.
func wrapLogAnalyst(a *agent.LogAnalystAgent) executableAgent {
	if a == nil {
		return nil
	}
	return a
}
func wrapMonitor(a *agent.MonitorAgent) executableAgent {
	if a == nil {
		return nil
	}
	return a
}
func wrapInspector(a *agent.InspectorAgent) executableAgent {
	if a == nil {
		return nil
	}
	return a
}

// agentTaskRequest is the body accepted by POST /api/v1/agents/{id} and
// POST /api/v1/agents/{id}/tasks. ClientTaskID is optional and only used by the
// async endpoint for idempotency.
type agentTaskRequest struct {
	Description  string                 `json:"description"`
	Params       map[string]interface{} `json:"params,omitempty"`
	ClientTaskID string                 `json:"client_task_id,omitempty"`
}

// getAgentInfo returns metadata about a registered agent.
func (s *APIServer) getAgentInfo(w http.ResponseWriter, agentID string) {
	meta, ok := s.agentMeta[agentID]
	if !ok {
		s.writeErrorResponse(w, http.StatusNotFound, "agent_not_found",
			"Agent not found: "+agentID, "")
		return
	}
	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"agent":  meta,
		"status": "ready",
	})
}

// executeAgentTask runs a task on the named agent using the real LLM.
func (s *APIServer) executeAgentTask(w http.ResponseWriter, r *http.Request, agentID string) {
	exec, ok := s.agents[agentID]
	if !ok {
		s.writeErrorResponse(w, http.StatusNotFound, "agent_not_found",
			"Agent not found or not available: "+agentID, "")
		return
	}

	var req agentTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request",
			"Failed to decode request", err.Error())
		return
	}
	if req.Description == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request",
			"Task description is required", "")
		return
	}

	task := &agent.Task{
		ID:          "api-" + time.Now().Format("20060102150405"),
		Description: req.Description,
		Params:      req.Params,
		CreatedAt:   time.Now(),
		Status:      "pending",
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	s.logger.Info("Agent task execution requested",
		observability.String("agent", agentID),
		observability.String("task_id", task.ID),
	)

	result, err := exec.Execute(ctx, task)
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "execution_failed",
			"Agent execution failed", err.Error())
		return
	}

	resp := map[string]interface{}{
		"agent_id":    agentID,
		"task_id":     task.ID,
		"success":     result.Success,
		"response":    result.Response,
		"data":        result.Data,
		"tools_used":  result.ToolsUsed,
		"duration_ms": result.Duration.Milliseconds(),
	}
	if result.Error != nil {
		resp["error"] = result.Error.Error()
	}
	s.writeJSONResponse(w, http.StatusOK, resp)
}

// submitAgentTaskHandler handles POST /api/v1/agents/{id}/tasks.
func (s *APIServer) submitAgentTaskHandler(w http.ResponseWriter, r *http.Request) {
	s.submitAgentTask(w, r, r.PathValue("id"))
}

// getAgentTaskHandler handles GET /api/v1/tasks/{id}.
func (s *APIServer) getAgentTaskHandler(w http.ResponseWriter, r *http.Request) {
	s.getAgentTask(w, r, r.PathValue("id"))
}

// submitAgentTask creates a task record and returns immediately; the actual
// execution runs on a background goroutine. A non-empty client_task_id makes
// the submission idempotent within (agent, tenant).
func (s *APIServer) submitAgentTask(w http.ResponseWriter, r *http.Request, agentID string) {
	if _, ok := s.agents[agentID]; !ok {
		s.writeErrorResponse(w, http.StatusNotFound, "agent_not_found",
			"Agent not found or not available: "+agentID, "")
		return
	}

	var req agentTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request",
			"Failed to decode request", err.Error())
		return
	}
	if req.Description == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request",
			"Task description is required", "")
		return
	}

	ctx := r.Context()
	if req.ClientTaskID != "" {
		if existing, ok := s.taskStore.FindByClientTaskID(ctx, agentID, req.ClientTaskID); ok {
			s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
				"task":       toAgentTaskDTO(existing),
				"duplicated": true,
			})
			return
		}
	}

	now := time.Now()
	task := &AgentTask{
		ID:           uuid.New().String(),
		AgentID:      agentID,
		Description:  req.Description,
		Params:       req.Params,
		ClientTaskID: req.ClientTaskID,
		Status:       TaskStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.taskStore.Create(ctx, task); err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "store_error",
			"Failed to create task", err.Error())
		return
	}

	// Encode the response before the goroutine mutates the task via Update.
	s.writeJSONResponse(w, http.StatusCreated, map[string]interface{}{
		"task":       toAgentTaskDTO(task),
		"duplicated": false,
	})

	go s.runAgentTask(agentID, task.ID, task.TenantID)
}

// getAgentTask returns the current task state (cross-tenant reads are 404).
func (s *APIServer) getAgentTask(w http.ResponseWriter, r *http.Request, id string) {
	task, err := s.taskStore.Get(r.Context(), id)
	if err != nil {
		s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Task not found", "")
		return
	}
	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"task": toAgentTaskDTO(task),
	})
}

// runAgentTask drives one execution attempt to a terminal or approval-awaiting
// state. It runs on a fresh background context so the HTTP request can return.
func (s *APIServer) runAgentTask(agentID, taskID string, tenantID int64) {
	ctx := tenant.WithID(context.Background(), tenantID)
	task, err := s.taskStore.Get(ctx, taskID)
	if err != nil {
		return
	}

	exec, ok := s.agents[agentID]
	if !ok {
		task.Status = TaskStatusFailed
		task.Error = "agent not found or not available: " + agentID
		_ = s.taskStore.Update(ctx, task)
		return
	}

	task.Status = TaskStatusRunning
	task.Error = ""
	task.ApprovalID = ""
	if err := s.taskStore.Update(ctx, task); err != nil {
		return
	}

	agentTask := &agent.Task{
		ID:          taskID,
		Description: task.Description,
		Params:      task.Params,
		CreatedAt:   time.Now(),
		Status:      TaskStatusRunning,
	}

	execCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	result, execErr := exec.Execute(execCtx, agentTask)
	if execErr != nil {
		if ar, ok := execErr.(*guardrails.ApprovalRequiredError); ok {
			s.handleApprovalRequired(ctx, task, ar.Request)
			return
		}
		task.Status = TaskStatusFailed
		task.Error = execErr.Error()
		task.Result = nil
		_ = s.taskStore.Update(ctx, task)
		return
	}

	// A successful call that returned a failed result must be surfaced as a
	// failed task, not a completed one.
	if result == nil || !result.Success {
		task.Status = TaskStatusFailed
		task.Result = result
		task.Error = "task execution failed"
		if result != nil && result.Error != nil {
			task.Error = result.Error.Error()
		}
		_ = s.taskStore.Update(ctx, task)
		return
	}

	task.Status = TaskStatusCompleted
	task.Result = result
	task.Error = ""
	_ = s.taskStore.Update(ctx, task)
}

// handleApprovalRequired parks a task in awaiting_approval and creates the
// PendingApproval the user acts on through /api/v1/approvals.
func (s *APIServer) handleApprovalRequired(ctx context.Context, task *AgentTask, req guardrails.ApprovalRequest) {
	expiry := req.ExpirySeconds
	if expiry <= 0 {
		expiry = 300
	}
	scope := req.Scope
	if scope == "" {
		scope = guardrails.ScopeOneShot
	}
	now := time.Now()

	pending := &PendingApproval{
		ApprovalID:     uuid.New().String(),
		ToolName:       req.ToolName,
		Params:         req.Params,
		Reason:         req.Reason,
		ConversationID: task.ID,
		TenantID:       task.TenantID,
		UserID:         task.UserID,
		ParamHash:      guardrails.ParamHash(req.Params),
		Scope:          scope,
		ExpirySeconds:  expiry,
		ExpiresAt:      now.Add(time.Duration(expiry) * time.Second),
		Status:         "pending",
		CreatedAt:      now,
	}
	if err := s.pendingApprovals.Create(pending); err != nil {
		task.Status = TaskStatusFailed
		task.Error = "failed to create approval: " + err.Error()
		_ = s.taskStore.Update(ctx, task)
		return
	}

	task.Status = TaskStatusAwaitingApproval
	task.ApprovalID = pending.ApprovalID
	task.Error = ""
	_ = s.taskStore.Update(ctx, task)
}

// agentTaskDTO is the transport shape of an AgentTask. The result is flattened
// so duration_ms and error are first-class fields the console already consumes.
type agentTaskDTO struct {
	ID           string                 `json:"id"`
	AgentID      string                 `json:"agent_id"`
	Description  string                 `json:"description"`
	Params       map[string]interface{} `json:"params,omitempty"`
	ClientTaskID string                 `json:"client_task_id,omitempty"`
	Status       string                 `json:"status"`
	Result       *agentResultDTO        `json:"result,omitempty"`
	Error        string                 `json:"error,omitempty"`
	ApprovalID   string                 `json:"approval_id,omitempty"`
	TenantID     int64                  `json:"tenant_id"`
	UserID       int64                  `json:"user_id"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type agentResultDTO struct {
	Success    bool                   `json:"success"`
	Response   string                 `json:"response"`
	Data       map[string]interface{} `json:"data,omitempty"`
	ToolsUsed  []string               `json:"tools_used,omitempty"`
	DurationMS int64                  `json:"duration_ms"`
	Error      string                 `json:"error,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

func toAgentTaskDTO(t *AgentTask) *agentTaskDTO {
	if t == nil {
		return nil
	}
	dto := &agentTaskDTO{
		ID:           t.ID,
		AgentID:      t.AgentID,
		Description:  t.Description,
		Params:       t.Params,
		ClientTaskID: t.ClientTaskID,
		Status:       t.Status,
		Error:        t.Error,
		ApprovalID:   t.ApprovalID,
		TenantID:     t.TenantID,
		UserID:       t.UserID,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
	if t.Result != nil {
		dto.Result = &agentResultDTO{
			Success:    t.Result.Success,
			Response:   t.Result.Response,
			Data:       t.Result.Data,
			ToolsUsed:  t.Result.ToolsUsed,
			DurationMS: t.Result.Duration.Milliseconds(),
			Metadata:   t.Result.Metadata,
		}
		if t.Result.Error != nil {
			dto.Result.Error = t.Result.Error.Error()
		}
	}
	return dto
}
