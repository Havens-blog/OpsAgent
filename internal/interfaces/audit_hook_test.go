// Package interfaces provides tests for REST API handlers.
package interfaces

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/chat"
	"github.com/Havens-blog/OpsAgent/internal/config"
)

func newAuditServer(t *testing.T, enable bool) *APIServer {
	t.Helper()
	server, err := NewAPIServer(&config.Settings{
		Environment: config.EnvDevelopment,
		Server:      config.ServerConfig{Host: "localhost", Port: 8080},
		Security:    config.SecurityConfig{EnableAuditLog: enable},
	})
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}
	t.Cleanup(func() { server.Shutdown(context.Background()) })
	return server
}

// TestRecordToolExecution verifies both success and failed tool executions are
// recorded into the in-memory ring with the expected operation and result.
func TestRecordToolExecution(t *testing.T) {
	server := newAuditServer(t, true)
	req := httptest.NewRequest("POST", "/api/v1/conversations/c1/messages", nil)

	server.recordToolExecution(req, chat.ToolEvidence{
		EvidenceID: "e1",
		ToolName:   "query_waf_logs",
		Status:     "success",
		Source:     "waf",
	})
	server.recordToolExecution(req, chat.ToolEvidence{
		EvidenceID: "e2",
		ToolName:   "query_logs",
		Status:     "failed",
		Source:     "logs",
		Error:      "boom",
	})

	entries := server.auditLog.recent(10)
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d, want 2; entries = %+v", len(entries), entries)
	}

	byOperation := map[string]string{}
	for _, e := range entries {
		byOperation[e.Operation] = e.Result
	}
	if byOperation["tool:query_waf_logs"] != "success" {
		t.Fatalf("query_waf_logs result = %q, want success; entries = %+v", byOperation["tool:query_waf_logs"], entries)
	}
	if byOperation["tool:query_logs"] != "failed" {
		t.Fatalf("query_logs result = %q, want failed; entries = %+v", byOperation["tool:query_logs"], entries)
	}
}

// TestRecordToolExecutionDisabled verifies the audit ring stays empty when
// security.enable_audit_log is false.
func TestRecordToolExecutionDisabled(t *testing.T) {
	server := newAuditServer(t, false)
	req := httptest.NewRequest("POST", "/api/v1/conversations/c1/messages", nil)

	server.recordToolExecution(req, chat.ToolEvidence{
		EvidenceID: "e1",
		ToolName:   "query_waf_logs",
		Status:     "success",
		Source:     "waf",
	})

	if got := len(server.auditLog.recent(10)); got != 0 {
		t.Fatalf("audit entries = %d, want 0", got)
	}
}
