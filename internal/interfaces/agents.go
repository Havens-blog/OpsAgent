// Package interfaces provides HTTP handlers for the OpsAgent API.
package interfaces

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/agent"
	"github.com/Havens-blog/OpsAgent/internal/observability"
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
			_ = coordinator.RegisterAgent("log_analyst", logAnalyst.BaseAgent)
		}
		if monitor != nil {
			_ = coordinator.RegisterAgent("monitor", monitor.BaseAgent)
		}
		if inspector != nil {
			_ = coordinator.RegisterAgent("inspector", inspector.BaseAgent)
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

// agentTaskRequest is the body accepted by POST /api/v1/agents/{id}.
type agentTaskRequest struct {
	Description string                 `json:"description"`
	Params      map[string]interface{} `json:"params,omitempty"`
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
