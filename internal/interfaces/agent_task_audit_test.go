// Package interfaces provides tests for REST API handlers.
package interfaces

import (
	"strings"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/agent"
)

// TestRecordAgentTaskExecution verifies a successful async agent task lands in
// the in-memory ring with the expected operation, result, detail and user.
func TestRecordAgentTaskExecution(t *testing.T) {
	server := newAuditServer(t, true)
	task := &AgentTask{AgentID: "monitor", TenantID: 3, UserID: 11}
	result := &agent.ExecutionResult{Success: true, ToolsUsed: []string{"query_metrics"}}

	server.recordAgentTaskExecution(task, result, nil)

	entries := server.auditLog.recent(10)
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1; entries = %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Operation != "agent_task:monitor" {
		t.Fatalf("operation = %q, want agent_task:monitor", e.Operation)
	}
	if e.Result != "success" {
		t.Fatalf("result = %q, want success", e.Result)
	}
	if e.Detail != "tools=query_metrics" {
		t.Fatalf("detail = %q, want tools=query_metrics", e.Detail)
	}
	if e.User != "11" {
		t.Fatalf("user = %q, want 11", e.User)
	}
}

// TestRecordAgentTaskExecutionFailed verifies a failed result surfaces as
// "failed" with the result error in the detail.
func TestRecordAgentTaskExecutionFailed(t *testing.T) {
	server := newAuditServer(t, true)
	task := &AgentTask{AgentID: "monitor", TenantID: 3, UserID: 11}
	result := &agent.ExecutionResult{Success: false, Error: "boom"}

	server.recordAgentTaskExecution(task, result, nil)

	entries := server.auditLog.recent(10)
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1; entries = %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Result != "failed" {
		t.Fatalf("result = %q, want failed", e.Result)
	}
	if !strings.Contains(e.Detail, "error=boom") {
		t.Fatalf("detail = %q, want error=boom included", e.Detail)
	}
}

// TestRecordAgentTaskExecutionDisabled verifies the audit ring stays empty when
// security.enable_audit_log is false.
func TestRecordAgentTaskExecutionDisabled(t *testing.T) {
	server := newAuditServer(t, false)
	task := &AgentTask{AgentID: "monitor", TenantID: 3, UserID: 11}

	server.recordAgentTaskExecution(task, &agent.ExecutionResult{Success: true}, nil)

	if got := len(server.auditLog.recent(10)); got != 0 {
		t.Fatalf("audit entries = %d, want 0", got)
	}
}
