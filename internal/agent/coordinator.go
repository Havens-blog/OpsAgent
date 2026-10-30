// Package agent provides the CoordinatorAgent for task orchestration.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/llm"
	"github.com/Havens-blog/OpsAgent/internal/observability"
)

// agentExecutor is the package-internal contract a delegate must satisfy. It
// lets the coordinator dispatch to a concrete agent's Execute instead of always
// falling back to BaseAgent.Execute.
type agentExecutor interface {
	Execute(ctx context.Context, task *Task) (*ExecutionResult, error)
	GetConfig() *AgentConfig
}

// CoordinatorAgent orchestrates multiple agents to complete complex tasks.
// It decomposes tasks, delegates to specialized agents, and aggregates results.
type CoordinatorAgent struct {
	*BaseAgent
	agents      map[string]agentExecutor
	mu          sync.RWMutex
	subTasks    map[string][]*TaskSubmission
	taskCounter int64
}

// TaskSubmission represents a task submitted to a sub-agent.
type TaskSubmission struct {
	TaskID      string
	AgentName   string
	Status      string
	Result      *ExecutionResult
	SubmittedAt time.Time
	CompletedAt time.Time
	Error       error
}

// NewCoordinatorAgent creates a new coordinator agent.
func NewCoordinatorAgent(settings *config.Settings) (*CoordinatorAgent, error) {
	cfg := &AgentConfig{
		Name:         "coordinator-agent",
		Type:         AgentTypeCoordinator,
		Description:  "Orchestrates multiple agents to complete complex tasks",
		Model:        "claude-sonnet-4-20250514",
		MaxTurns:     15,
		Timeout:      10 * time.Minute,
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
		agents:      make(map[string]agentExecutor),
		subTasks:    make(map[string][]*TaskSubmission),
		taskCounter: 0,
	}, nil
}

// RegisterAgent registers a specialized agent for delegation.
func (c *CoordinatorAgent) RegisterAgent(name string, agent agentExecutor) error {
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

// DelegateTask sends a task to a specific agent.
func (c *CoordinatorAgent) DelegateTask(ctx context.Context, agentName string, task *Task) (*TaskSubmission, error) {
	c.mu.RLock()
	agent, exists := c.agents[agentName]
	c.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("agent not registered: %s", agentName)
	}

	submission := &TaskSubmission{
		TaskID:      fmt.Sprintf("%s-%d", task.ID, c.taskCounter),
		AgentName:   agentName,
		Status:      "pending",
		SubmittedAt: time.Now(),
	}

	// Store submission
	c.mu.Lock()
	c.subTasks[task.ID] = append(c.subTasks[task.ID], submission)
	c.taskCounter++
	c.mu.Unlock()

	c.logger.Info("Task delegated",
		observability.String("task_id", task.ID),
		observability.String("agent", agentName),
		observability.String("submission_id", submission.TaskID),
	)

	// Execute asynchronously (in real implementation, use goroutine pool)
	result, err := agent.Execute(ctx, task)

	submission.Status = "completed"
	submission.CompletedAt = time.Now()
	submission.Result = result
	submission.Error = err

	if err != nil {
		submission.Status = "failed"
		c.logger.Error("Task delegation failed",
			observability.String("submission_id", submission.TaskID),
			observability.Err(err),
		)
	} else {
		c.logger.Info("Task delegation completed",
			observability.String("submission_id", submission.TaskID),
			observability.Bool("success", result.Success),
		)
	}

	return submission, nil
}

// DelegateTasksAsync delegates multiple tasks in parallel.
func (c *CoordinatorAgent) DelegateTasksAsync(ctx context.Context, delegations []TaskDelegation) []*TaskSubmission {
	type result struct {
		submission *TaskSubmission
		err        error
	}

	results := make(chan result, len(delegations))

	for _, del := range delegations {
		go func(d TaskDelegation) {
			sub, err := c.DelegateTask(ctx, d.AgentName, d.Task)
			results <- result{submission: sub, err: err}
		}(del)
	}

	// Collect results
	submissions := make([]*TaskSubmission, 0, len(delegations))
	for i := 0; i < len(delegations); i++ {
		r := <-results
		if r.err == nil {
			submissions = append(submissions, r.submission)
		}
	}

	return submissions
}

// TaskDelegation represents a task to be delegated to an agent.
type TaskDelegation struct {
	AgentName string
	Task      *Task
	DependsOn []string // IDs of tasks this depends on
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
			Success: false,
			Error:   err,
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
	Strategy     string // "sequential", "parallel", "hybrid"
	Delegations  []TaskDelegation
	Dependencies map[string][]string // task ID -> dependent task IDs
}

// knownTypeAgent maps an explicit task type to its specialist agent. These are
// the fast-path routes: when a caller already labels the task, there is no
// reason to spend an LLM call deciding where it goes.
var knownTypeAgent = map[string]string{
	"rca":          "monitor",
	"log_analysis": "log_analyst",
	"inspection":   "inspector",
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
		Strategy:     "sequential",
		Delegations:  []TaskDelegation{},
		Dependencies: make(map[string][]string),
	}

	// (1) Explicit type fast-path.
	if taskType, ok := task.Params["type"].(string); ok {
		if agentName, known := knownTypeAgent[taskType]; known {
			plan.Delegations = append(plan.Delegations, c.newDelegation(agentName, taskType, task))
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

// newDelegation builds a sub-task delegation for the named agent.
func (c *CoordinatorAgent) newDelegation(agentName, suffix string, task *Task) TaskDelegation {
	return TaskDelegation{
		AgentName: agentName,
		Task: &Task{
			ID:          fmt.Sprintf("%s-%s", task.ID, suffix),
			Description: task.Description,
			Params:      task.Params,
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

Available agents:
%s
User request:
%q

Choose the agent(s) best suited to handle this request. Respond with ONLY a
JSON object, no prose, in this exact shape:
{"strategy":"sequential|parallel","agents":["<agent-name>", ...]}

Rules:
- "agents" must be a non-empty subset of the available agent names above.
- Use "parallel" only when the chosen agents can work independently.
- If a single agent suffices, return just that one.`, catalog.String(), task.Description)

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
		Strategy string   `json:"strategy"`
		Agents   []string `json:"agents"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return nil, fmt.Errorf("malformed routing JSON: %w", err)
	}

	strategy := "sequential"
	if parsed.Strategy == "parallel" {
		strategy = "parallel"
	}

	plan := &ExecutionPlan{
		Strategy:     strategy,
		Delegations:  []TaskDelegation{},
		Dependencies: make(map[string][]string),
	}
	seen := make(map[string]bool)
	for _, name := range parsed.Agents {
		name = strings.TrimSpace(name)
		if !valid[name] || seen[name] {
			continue // drop unknown/duplicate agents
		}
		seen[name] = true
		plan.Delegations = append(plan.Delegations, c.newDelegation(name, name, task))
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

	// Execute delegations based on strategy
	var submissions []*TaskSubmission

	switch plan.Strategy {
	case "sequential":
		for _, del := range plan.Delegations {
			sub, err := c.DelegateTask(ctx, del.AgentName, del.Task)
			if err != nil {
				return &ExecutionResult{
					Success: false,
					Error:   err,
					Response: fmt.Sprintf("Delegation to %s failed: %v", del.AgentName, err),
				}
			}
			submissions = append(submissions, sub)

			// Stop if task failed
			if sub.Error != nil || (sub.Result != nil && !sub.Result.Success) {
				return &ExecutionResult{
					Success: false,
					Error:   sub.Error,
					Response: fmt.Sprintf("Task failed at %s: %v", del.AgentName, sub.Error),
				}
			}
		}

	case "parallel":
		submissions = c.DelegateTasksAsync(ctx, plan.Delegations)

		// Check if all succeeded
		for _, sub := range submissions {
			if sub.Error != nil || (sub.Result != nil && !sub.Result.Success) {
				return &ExecutionResult{
					Success: false,
					Response: fmt.Sprintf("One or more parallel tasks failed"),
				}
			}
		}
	}

	// Aggregate results
	return c.aggregateResults(originalTask, submissions)
}

// aggregateResults combines results from multiple sub-tasks.
func (c *CoordinatorAgent) aggregateResults(originalTask *Task, submissions []*TaskSubmission) *ExecutionResult {
	if len(submissions) == 0 {
		return &ExecutionResult{
			Success:  true,
			Response: "Task completed with no sub-tasks",
		}
	}

	// Combine all responses
	var combinedResponse string
	toolsUsed := []string{}
	allSuccess := true

	for i, sub := range submissions {
		if i > 0 {
			combinedResponse += "\n\n"
		}
		combinedResponse += fmt.Sprintf("[%s]: %s", sub.AgentName, sub.Result.Response)

		if !sub.Result.Success {
			allSuccess = false
		}

		toolsUsed = append(toolsUsed, sub.Result.ToolsUsed...)
	}

	return &ExecutionResult{
		Success:   allSuccess,
		Response:  combinedResponse,
		ToolsUsed: toolsUsed,
		Data: map[string]interface{}{
			"submissions": submissions,
			"agent_count": len(submissions),
		},
	}
}

// executeTask implements the default task execution for coordinator.
func (c *CoordinatorAgent) executeTask(ctx context.Context, task *Task) *ExecutionResult {
	// Default coordinator behavior
	availableAgents := c.GetRegisteredAgents()

	return &ExecutionResult{
		Success: true,
		Response: fmt.Sprintf("CoordinatorAgent ready. Available agents: %v\n\nSpecify a task type to delegate:\n- type='rca' for Root Cause Analysis\n- type='log_analysis' for log analysis\n- type='inspection' for operations inspection", availableAgents),
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
