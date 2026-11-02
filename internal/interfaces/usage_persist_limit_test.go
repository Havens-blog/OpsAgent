// Package interfaces provides tests for REST API handlers.
package interfaces

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/llm"
)

func TestFileBackedUsageMetricsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")

	m, err := newFileBackedUsageMetrics(path)
	if err != nil {
		t.Fatalf("newFileBackedUsageMetrics: %v", err)
	}
	m.addLLM(1, 2, llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	m.addTool(1, 2)

	reloaded, err := newFileBackedUsageMetrics(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	agg := reloaded.tenantAggregate(1)
	if agg.TotalTokens != 150 || agg.ToolCalls != 1 {
		t.Fatalf("reloaded aggregate = %+v, want TotalTokens=150 ToolCalls=1", agg)
	}
}

func TestFileBackedUsageMetricsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	if _, err := newFileBackedUsageMetrics(path); err == nil {
		t.Fatalf("expected error for corrupt file")
	}
}

func TestFileBackedUsageMetricsEmptyPath(t *testing.T) {
	if _, err := newFileBackedUsageMetrics(""); err == nil {
		t.Fatalf("expected error for empty path")
	}
}

func TestTenantTokenLimit(t *testing.T) {
	server := &APIServer{
		usage:            newUsageMetrics(),
		tenantTokenLimit: 150,
	}
	server.usage.addLLM(1, 2, llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})

	req := withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/conversations/x/messages", nil), 1, 2)
	w := httptest.NewRecorder()
	if !server.enforceTenantTokenLimit(w, req) {
		t.Fatalf("expected enforceTenantTokenLimit to block")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body = %s", w.Code, w.Body.String())
	}

	// limit=0 (unlimited) must not block.
	unlimited := &APIServer{usage: newUsageMetrics(), tenantTokenLimit: 0}
	unlimited.usage.addLLM(1, 2, llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	w2 := httptest.NewRecorder()
	if unlimited.enforceTenantTokenLimit(w2, req) {
		t.Fatalf("expected no block when limit=0")
	}
	if w2.Code != http.StatusOK {
		t.Fatalf("unlimited status = %d, want 200", w2.Code)
	}
}
