// Package agent provides the CoordinatorAgent for task orchestration.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/agent/comm"
	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/llm"
	"github.com/Havens-blog/OpsAgent/internal/observability"
)

// SpecialistAgent is a sub-agent registered with the Coordinator: it executes
// tasks and describes itself for LLM routing. Registered as an interface so
// delegation dispatches to the specialist's TYPED Execute override — Go
// dispatches statically on *BaseAgent, so the old *BaseAgent registry silently
// bypassed the real capability paths (RCA engine, log analysis) and reduced
// every delegation to a bare LLM chat.
type SpecialistAgent interface {
	Execute(ctx context.Context, task *Task) (*ExecutionResult, error)
	GetConfig() *AgentConfig
}

// CoordinatorAgent orchestrates multiple agents to complete complex tasks.
// It decomposes tasks, delegates to specialized agents, and aggregates results.
type CoordinatorAgent struct {
	*BaseAgent
	agents      map[string]SpecialistAgent
	mu          sync.RWMutex
	subTasks    map[string][]*TaskSubmission
	taskCounter int64
	evidence    *comm.EvidenceStore
}

// TaskSubmission represents a task submitted to a sub-agent.
type TaskSubmission struct {
	TaskID      string
	AgentName   string
	Status      string
	Result      *ExecutionResult
	SubmittedAt time.Time
	CompletedAt time.Time
	Error       string
}

// NewCoordinatorAgent creates a new coordinator agent.
func NewCoordinatorAgent(settings *config.Settings) (*CoordinatorAgent, error) {
	cfg := &AgentConfig{
		Name:        "coordinator-agent",
		Type:        AgentTypeCoordinator,
		Description: "Orchestrates multiple agents to complete complex tasks",
		Model:       "claude-sonnet-4-20250514",
		MaxTurns:    15,
		Timeout:     10 * time.Minute,
		SystemPrompt: `You are a CoordinatorAgent responsible for orchestrating other agents.

Your capabilities include:
- Decomposing complex tasks into sub-tasks
- Delegating sub-tasks to specialized agents (log_analyst, monitor, inspector)
- Aggregating results from multiple agents
- Coordinating parallel execution when possible
- Resolving dependencies between sub-tasks

Available agent types:
- log_analyst: Log analysis and pattern detection
- monitor: APM analysis and Root Cause Analysis
- inspector: Operations inspection and compliance checks

When delegating:
1. Identify the appropriate agent for each sub-task
2. Provide clear context and requirements
3. Specify dependencies between tasks
4. Set appropriate timeouts`,
		Capabilities: []string{"orchestration", "delegation", "aggregation", "planning"},
	}

	base, err := NewBaseAgent(cfg, settings)
	if err != nil {
		return nil, err
	}

	return &CoordinatorAgent{
		BaseAgent:   base,
		agents:      make(map[string]SpecialistAgent),
		subTasks:    make(map[string][]*TaskSubmission),
		taskCounter: 0,
		evidence:    comm.NewEvidenceStore(comm.MergeStrategyUnion),
	}, nil
}

// RegisterAgent registers a specialized agent for delegation.
func (c *CoordinatorAgent) RegisterAgent(name string, agent SpecialistAgent) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if agent == nil {
		return fmt.Errorf("cannot register nil agent")
	}

	c.agents[name] = agent
	c.logger.Info("Agent registered",
		observability.String("agent", name),
		observability.String("type", string(agent.GetConfig().Type)),
	)

	return nil
}

// UnregisterAgent removes an agent from the registry.
func (c *CoordinatorAgent) UnregisterAgent(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.agents, name)
	c.logger.Info("Agent unregistered",
		observability.String("agent", name),
	)
}

// GetRegisteredAgents returns the list of registered agent names.
func (c *CoordinatorAgent) GetRegisteredAgents() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	names := make([]string, 0, len(c.agents))
	for name := range c.agents {
		names = append(names, name)
	}
	return names
}

// succeeded reports whether the sub-task finished without error and the
// specialist itself reported success.
func (s *TaskSubmission) succeeded() bool {
	return s.Error == "" && s.Result != nil && s.Result.Success
}

// DelegateTask sends a task to a specific agent. All submission bookkeeping is
// done under c.mu so concurrent delegations get unique IDs and GetTaskStatus
// never observes a half-written submission.
func (c *CoordinatorAgent) DelegateTask(ctx context.Context, agentName string, task *Task) (*TaskSubmission, error) {
	c.mu.Lock()
	agent, exists := c.agents[agentName]
	if !exists {
		c.mu.Unlock()
		return nil, fmt.Errorf("agent not registered: %s", agentName)
	}
	submission := &TaskSubmission{
		TaskID:      fmt.Sprintf("%s-%d", task.ID, c.taskCounter),
		AgentName:   agentName,
		Status:      "pending",
		SubmittedAt: time.Now(),
	}
	c.taskCounter++
	c.subTasks[task.ID] = append(c.subTasks[task.ID], submission)
	c.mu.Unlock()

	c.logger.Info("Task delegated",
		observability.String("task_id", task.ID),
		observability.String("agent", agentName),
		observability.String("submission_id", submission.TaskID),
	)

	result, err := agent.Execute(ctx, task)

	c.mu.Lock()
	submission.Status = "completed"
	if err != nil {
		submission.Status = "failed"
	}
	submission.CompletedAt = time.Now()
	submission.Result = result
	submission.Error = errText(err)
	c.mu.Unlock()

	if err != nil {
		c.logger.Error("Task delegation failed",
			observability.String("submission_id", submission.TaskID),
			observability.Err(err),
		)
	} else {
		c.logger.Info("Task delegation completed",
			observability.String("submission_id", submission.TaskID),
			observability.Bool("success", result != nil && result.Success),
		)
	}

	return submission, nil
}

// delegateOrFail delegates a task and converts a delegation error (e.g. an
// unregistered agent) into a failed submission, so callers always get one
// submission per delegation to report on.
func (c *CoordinatorAgent) delegateOrFail(ctx context.Context, d TaskDelegation) *TaskSubmission {
	sub, err := c.DelegateTask(ctx, d.AgentName, d.Task)
	if err == nil {
		return sub
	}
	now := time.Now()
	return &TaskSubmission{
		TaskID:      d.Task.ID,
		AgentName:   d.AgentName,
		Status:      "failed",
		Error:       err.Error(),
		SubmittedAt: now,
		CompletedAt: now,
	}
}

// DelegateTasksAsync delegates multiple tasks in parallel. It returns exactly
// one submission per delegation, in delegation order, including failures.
func (c *CoordinatorAgent) DelegateTasksAsync(ctx context.Context, delegations []TaskDelegation) []*TaskSubmission {
	submissions := make([]*TaskSubmission, len(delegations))
	var wg sync.WaitGroup
	for i, del := range delegations {
		wg.Add(1)
		go func(i int, d TaskDelegation) {
			defer wg.Done()
			submissions[i] = c.delegateOrFail(ctx, d)
		}(i, del)
	}
	wg.Wait()
	return submissions
}

// TaskDelegation represents a task to be delegated to an agent.
type TaskDelegation struct {
	AgentName string
	Task      *Task
}

// Execute orchestrates the task execution process.
func (c *CoordinatorAgent) Execute(ctx context.Context, task *Task) (*ExecutionResult, error) {
	startTime := time.Now()

	// Add timeout if not set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && c.BaseAgent.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.BaseAgent.config.Timeout)
		defer cancel()
	}

	// Add user message
	c.BaseAgent.conversation.AddMessage(Message{
		Role:      "user",
		Content:   task.Description,
		Timestamp: time.Now(),
	})

	c.logger.Info("CoordinatorAgent execution started",
		observability.String("task_id", task.ID),
	)

	// Analyze task and determine execution strategy
	plan, err := c.createExecutionPlan(ctx, task)
	if err != nil {
		return &ExecutionResult{
			Success:  false,
			Error:    errText(err),
			Response: fmt.Sprintf("Failed to create execution plan: %v", err),
		}, nil
	}

	// Execute the plan
	result := c.executePlan(ctx, task, plan)

	duration := time.Since(startTime)
	result.Duration = duration

	c.logger.Info("CoordinatorAgent execution completed",
		observability.String("task_id", task.ID),
		observability.Bool("success", result.Success),
		observability.String("duration", duration.String()),
	)

	// Add assistant message
	if result.Response != "" {
		c.BaseAgent.conversation.AddMessage(Message{
			Role:      "assistant",
			Content:   result.Response,
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"success":  result.Success,
				"duration": duration.String(),
			},
		})
	}

	return result, nil
}

// ExecutionPlan represents the execution strategy for a task.
type ExecutionPlan struct {
	Strategy    string // "sequential", "parallel", "direct"
	Delegations []TaskDelegation
}

// knownTypeAgent maps an explicit task type to its specialist agent. These are
// the fast-path routes: when a caller already labels the task, there is no
// reason to spend an LLM call deciding where it goes.
var knownTypeAgent = map[string]string{
	"rca":          "monitor",
	"log_analysis": "log_analyst",
	"inspection":   "inspector",
	"topology":     "resource_topology",
	"cmdb":         "resource_topology",
}

// createExecutionPlan analyzes the task and creates an execution plan.
//
// Routing precedence (most specific first):
//  1. Explicit task.Params["type"] matching a known specialist — zero-cost
//     deterministic route (back-compatible with the original behavior).
//  2. LLM intent routing — when no explicit type is given, ask the model which
//     registered specialists should handle the free-text request and whether
//     they can run in parallel.
//  3. "direct" fallback — when the LLM is unavailable, returns nothing usable,
//     or no specialists are registered, the coordinator answers directly. This
//     mirrors the progressive-enhancement used across the data sources: real
//     intelligence when available, a safe deterministic path otherwise.
func (c *CoordinatorAgent) createExecutionPlan(ctx context.Context, task *Task) (*ExecutionPlan, error) {
	plan := &ExecutionPlan{
		Strategy:    "sequential",
		Delegations: []TaskDelegation{},
	}

	// (1) Explicit type fast-path.
	if taskType, ok := task.Params["type"].(string); ok {
		if agentName, known := knownTypeAgent[taskType]; known {
			plan.Delegations = append(plan.Delegations, c.newDelegation(agentName, taskType, task, nil))
			return plan, nil
		}
		// Unknown explicit type: fall through to LLM / direct rather than
		// silently misrouting.
	}

	// (2) LLM intent routing.
	if llmPlan, err := c.routeWithLLM(ctx, task); err == nil && len(llmPlan.Delegations) > 0 {
		return llmPlan, nil
	} else if err != nil {
		c.logger.Info("LLM routing unavailable, handling directly",
			observability.String("task_id", task.ID),
			observability.String("reason", err.Error()),
		)
	}

	// (3) Direct fallback.
	plan.Strategy = "direct"
	return plan, nil
}

// agentTaskType maps a specialist to its typed-task route, so delegated tasks
// deterministically hit the specialist's real capability path even when the
// caller/LLM omitted the type.
var agentTaskType = map[string]string{
	"monitor":           "rca",
	"log_analyst":       "log_analysis",
	"inspector":         "inspection",
	"resource_topology": "topology",
}

// newDelegation builds a sub-task delegation for the named agent. Params are
// CLONED (never shared with the original task), overlaid with LLM-extracted
// params, and stamped with the agent's task type.
func (c *CoordinatorAgent) newDelegation(agentName, suffix string, task *Task, extra map[string]interface{}) TaskDelegation {
	params := make(map[string]interface{}, len(task.Params)+len(extra)+1)
	for k, v := range task.Params {
		params[k] = v
	}
	for k, v := range extra {
		params[k] = v
	}
	if _, exists := params["type"]; !exists {
		if t, ok := agentTaskType[agentName]; ok {
			params["type"] = t
		}
	}
	return TaskDelegation{
		AgentName: agentName,
		Task: &Task{
			ID:          fmt.Sprintf("%s-%s", task.ID, suffix),
			Description: task.Description,
			Params:      params,
			CreatedAt:   time.Now(),
			Status:      "pending",
		},
	}
}

// routeWithLLM asks the LLM to choose which registered specialists should
// handle a free-text task and whether they can run in parallel. It returns an
// error (triggering the direct fallback) when no specialists are registered,
// the LLM client is unavailable, or the response cannot be parsed into at
// least one valid, registered agent.
func (c *CoordinatorAgent) routeWithLLM(ctx context.Context, task *Task) (*ExecutionPlan, error) {
	if c.llmClient == nil {
		return nil, fmt.Errorf("no LLM client configured")
	}

	// Snapshot registered agents and their advertised capabilities.
	c.mu.RLock()
	type agentInfo struct {
		name string
		desc string
		caps []string
	}
	infos := make([]agentInfo, 0, len(c.agents))
	for name, ag := range c.agents {
		info := agentInfo{name: name}
		if ag != nil {
			if cfg := ag.GetConfig(); cfg != nil {
				info.desc = cfg.Description
				info.caps = cfg.Capabilities
			}
		}
		infos = append(infos, info)
	}
	c.mu.RUnlock()

	if len(infos) == 0 {
		return nil, fmt.Errorf("no specialist agents registered")
	}

	// Build the capability catalog for the prompt.
	var catalog strings.Builder
	valid := make(map[string]bool, len(infos))
	for _, in := range infos {
		valid[in.name] = true
		fmt.Fprintf(&catalog, "- %s: %s", in.name, in.desc)
		if len(in.caps) > 0 {
			fmt.Fprintf(&catalog, " (capabilities: %s)", strings.Join(in.caps, ", "))
		}
		catalog.WriteString("\n")
	}

	prompt := fmt.Sprintf(`You are routing an operations request to specialist agents.

Current time: %s
Available agents:
%s
User request:
%q

Choose the agent(s) best suited to handle this request, and extract the
parameters each needs from the request (omit fields it does not imply).
Respond with ONLY a JSON object, no prose, in this exact shape:
{"strategy":"sequential|parallel","delegations":[{"agent":"<name>","params":{...}}, ...]}

Params per agent:
- log_analyst: {"service":"<svc>","start_time":"<RFC3339>","end_time":"<RFC3339>"}
- monitor: {"incident":{"id":"<short-slug>","start_time":"<RFC3339>","end_time":"<RFC3339>","severity":"P0|P1|P2|P3|P4","affected_services":["<svc>"],"description":"<what happened>"}}
- inspector: {"inspection_type":"cloud_resources|elasticsearch|compliance"}

Rules:
- every "agent" must be one of the available agent names above.
- Use "parallel" only when the chosen agents can work independently.
- If a single agent suffices, return just that one.`, time.Now().Format(time.RFC3339), catalog.String(), task.Description)

	messages := []llm.ClaudeMessage{{Role: "user", Content: prompt}}
	raw, err := c.llmClient.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM routing call failed: %w", err)
	}

	return c.parsePlanJSON(raw, valid, task)
}

// parsePlanJSON tolerantly parses the LLM routing response into an execution
// plan. It extracts the first JSON object (the model may wrap it in prose or
// code fences), keeps only agents that are actually registered, and errors if
// nothing valid remains so the caller can fall back to direct handling.
func (c *CoordinatorAgent) parsePlanJSON(raw string, valid map[string]bool, task *Task) (*ExecutionPlan, error) {
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object in LLM response")
	}

	var parsed struct {
		Strategy    string   `json:"strategy"`
		Agents      []string `json:"agents"` // legacy shape: bare names
		Delegations []struct {
			Agent  string                 `json:"agent"`
			Params map[string]interface{} `json:"params"`
		} `json:"delegations"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return nil, fmt.Errorf("malformed routing JSON: %w", err)
	}

	strategy := "sequential"
	if parsed.Strategy == "parallel" {
		strategy = "parallel"
	}

	plan := &ExecutionPlan{
		Strategy:    strategy,
		Delegations: []TaskDelegation{},
	}
	seen := make(map[string]bool)
	add := func(name string, params map[string]interface{}) {
		name = strings.TrimSpace(name)
		if !valid[name] || seen[name] {
			return // drop unknown/duplicate agents
		}
		seen[name] = true
		plan.Delegations = append(plan.Delegations, c.newDelegation(name, name, task, params))
	}
	for _, d := range parsed.Delegations {
		add(d.Agent, d.Params)
	}
	for _, name := range parsed.Agents {
		add(name, nil)
	}

	if len(plan.Delegations) == 0 {
		return nil, fmt.Errorf("LLM selected no valid registered agents")
	}
	return plan, nil
}

// executePlan executes the given execution plan.
func (c *CoordinatorAgent) executePlan(ctx context.Context, originalTask *Task, plan *ExecutionPlan) *ExecutionResult {
	if plan.Strategy == "direct" {
		// Handle directly without delegation
		return c.executeTask(ctx, originalTask)
	}

	var submissions []*TaskSubmission
	if plan.Strategy == "parallel" {
		submissions = c.DelegateTasksAsync(ctx, plan.Delegations)
	} else {
		// 顺序执行遇到失败即停止，但保留已完成专家的结果
		for _, del := range plan.Delegations {
			sub := c.delegateOrFail(ctx, del)
			submissions = append(submissions, sub)
			if !sub.succeeded() {
				break
			}
		}
	}

	return c.aggregateResults(ctx, originalTask, submissions)
}

// aggregateResults combines results from multiple sub-tasks. Failed sub-tasks
// never discard the evidence other specialists already produced: successful
// responses are kept, failures are listed, and Data["partial"] marks a mixed
// outcome.
func (c *CoordinatorAgent) aggregateResults(ctx context.Context, originalTask *Task, submissions []*TaskSubmission) *ExecutionResult {
	if len(submissions) == 0 {
		return &ExecutionResult{
			Success:  true,
			Response: "Task completed with no sub-tasks",
		}
	}

	var parts []string
	toolsUsed := []string{}
	failed := []string{}
	evidenceIDs := []string{}

	for _, sub := range submissions {
		if sub.Result != nil {
			toolsUsed = append(toolsUsed, sub.Result.ToolsUsed...)
		}
		if sub.succeeded() {
			parts = append(parts, fmt.Sprintf("[%s]: %s", sub.AgentName, sub.Result.Response))
			evidenceIDs = append(evidenceIDs, c.recordEvidence(originalTask, sub))
			continue
		}
		failed = append(failed, sub.AgentName)
		reason := sub.Error
		if reason == "" && sub.Result != nil {
			reason = sub.Result.Error
			if reason == "" {
				reason = sub.Result.Response
			}
		}
		parts = append(parts, fmt.Sprintf("[%s] 执行失败: %s", sub.AgentName, reason))
	}

	response, synthesized := c.synthesize(ctx, originalTask, submissions, parts)

	result := &ExecutionResult{
		Success:   len(failed) == 0,
		Response:  response,
		ToolsUsed: toolsUsed,
		Data: map[string]interface{}{
			"submissions":    submissions,
			"agent_count":    len(submissions),
			"failed_agents":  failed,
			"partial":        len(failed) > 0 && len(failed) < len(submissions),
			"evidence_ids":   evidenceIDs,
			"evidence_count": len(c.evidence.GetByIncident(originalTask.ID)),
			"synthesized":    synthesized,
		},
	}
	if len(failed) > 0 {
		result.Error = fmt.Sprintf("%d/%d 个专家执行失败: %s", len(failed), len(submissions), strings.Join(failed, ", "))
	}
	return result
}

// recordEvidence persists a successful specialist conclusion as shared evidence.
func (c *CoordinatorAgent) recordEvidence(task *Task, sub *TaskSubmission) string {
	e := &comm.SharedEvidence{
		SourceAgent: sub.AgentName,
		Type:        comm.EvidenceTypeHypothesis,
		Title:       fmt.Sprintf("%s 结论", sub.AgentName),
		Description: findingConclusion(sub.Result),
		IncidentID:  task.ID,
		Confidence:  findingConfidence(sub.Result),
	}
	c.evidence.Add(e)
	return e.DedupKey
}

// findingConclusion prefers the specialist's structured conclusion and falls
// back to its full response when no finding was produced.
func findingConclusion(r *ExecutionResult) string {
	if r != nil && r.Finding != nil && strings.TrimSpace(r.Finding.Conclusion) != "" {
		return r.Finding.Conclusion
	}
	if r != nil {
		return r.Response
	}
	return ""
}

// findingConfidence prefers the structured finding's confidence and falls back
// to the result data when no finding was produced.
func findingConfidence(r *ExecutionResult) float64 {
	if r != nil && r.Finding != nil {
		return r.Finding.Confidence
	}
	return resultConfidence(r)
}

// resultConfidence extracts the specialist's self-reported confidence from its
// result data, defaulting to 0 when absent or not a number.
func resultConfidence(r *ExecutionResult) float64 {
	if r == nil || r.Data == nil {
		return 0
	}
	switch v := r.Data["confidence"].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return 0
	}
}

// synthesize asks the coordinator's LLM to fuse multiple specialists' findings
// into one conclusion. With fewer than two successful experts, no LLM, or a
// cancelled parent context, it falls back to the deterministic concatenated
// response and never starts a new background-only call.
func (c *CoordinatorAgent) synthesize(ctx context.Context, task *Task, submissions []*TaskSubmission, parts []string) (string, bool) {
	successful := make([]*TaskSubmission, 0, len(submissions))
	for _, sub := range submissions {
		if sub.succeeded() {
			successful = append(successful, sub)
		}
	}
	if len(successful) < 2 || c.llmClient == nil {
		return strings.Join(parts, "\n\n"), false
	}
	if ctx != nil && ctx.Err() != nil {
		return strings.Join(parts, "\n\n"), false
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	messages := []llm.ClaudeMessage{{
		Role:    "user",
		Content: buildSynthesisPrompt(task, successful),
	}}
	out, err := c.llmClient.Chat(ctx, messages)
	if err != nil || strings.TrimSpace(out) == "" {
		c.logger.Info("LLM synthesis unavailable, using deterministic aggregation",
			observability.String("task_id", task.ID),
			observability.Err(err),
		)
		return strings.Join(parts, "\n\n"), false
	}
	return out, true
}

// buildSynthesisPrompt renders the specialists' structured findings for the
// coordinator's final fusion pass.
func buildSynthesisPrompt(task *Task, submissions []*TaskSubmission) string {
	var b strings.Builder
	b.WriteString("你是运维协调者，请把多位专家的排查结论融合成一份简洁、专业的最终结论，")
	b.WriteString("用中文输出，说明最可能的根因、支持它的证据，以及不同结论的冲突点。\n\n")
	b.WriteString("原始诉求：\n")
	b.WriteString(task.Description)
	b.WriteString("\n\n专家结论：\n")
	for _, sub := range submissions {
		fmt.Fprintf(&b, "--- %s ---\n%s\n", sub.AgentName, findingConclusion(sub.Result))
	}
	return b.String()
}

// executeTask is the coordinator's fallback for a task it could not route to a
// specialist (no explicit type and LLM routing unavailable/empty). It reports
// failure rather than claiming success without doing the requested work.
func (c *CoordinatorAgent) executeTask(ctx context.Context, task *Task) *ExecutionResult {
	availableAgents := c.GetRegisteredAgents()

	return &ExecutionResult{
		Success:  false,
		Error:    "unable to route task to a specialist agent",
		Response: fmt.Sprintf("无法将任务路由到可用专家（可用：%v）。请提供明确的 type 参数（rca / log_analysis / inspection）或稍后重试。", availableAgents),
		Data: map[string]interface{}{
			"available_agents": availableAgents,
			"registered_count": len(availableAgents),
		},
	}
}

// GetTaskStatus returns the status of all sub-tasks for a given task.
func (c *CoordinatorAgent) GetTaskStatus(taskID string) []*TaskSubmission {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.subTasks[taskID]
}
