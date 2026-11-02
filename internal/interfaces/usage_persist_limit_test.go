// Package interfaces provides tests for REST API handlers.
package interfaces

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	m.Flush()

	reloaded, err := newFileBackedUsageMetrics(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	agg := reloaded.tenantAggregate(1)
	if agg.TotalTokens != 150 || agg.ToolCalls != 1 {
		t.Fatalf("reloaded aggregate = %+v, want TotalTokens=150 ToolCalls=1", agg)
	}
}

func TestUsageBatchPersistFlushesOnInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")

	m, err := newFileBackedUsageMetrics(path)
	if err != nil {
		t.Fatalf("newFileBackedUsageMetrics: %v", err)
	}

	// 首次 add 因 lastFlush 为零值而立即落盘。
	m.addLLM(1, 2, llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})

	// 前移 lastFlush，模拟已越过节流窗口。
	m.mu.Lock()
	m.lastFlush = time.Now().Add(-usageFlushInterval)
	m.mu.Unlock()

	// 到达间隔后的下一次 add 应自动 flush。
	m.addTool(1, 2)

	reloaded, err := newFileBackedUsageMetrics(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	agg := reloaded.tenantAggregate(1)
	if agg.TotalTokens != 15 || agg.ToolCalls != 1 {
		t.Fatalf("interval flush aggregate = %+v, want TotalTokens=15 ToolCalls=1", agg)
	}
}

func TestUsageBatchPersistThrottlesWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")

	m, err := newFileBackedUsageMetrics(path)
	if err != nil {
		t.Fatalf("newFileBackedUsageMetrics: %v", err)
	}

	// 首次 add 立即落盘。
	m.addLLM(1, 2, llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})

	// 紧接着的 addTool 仍在节流窗口内，不应落盘。
	m.addTool(1, 2)

	reloaded, err := newFileBackedUsageMetrics(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	agg := reloaded.tenantAggregate(1)
	if agg.TotalTokens != 15 {
		t.Fatalf("throttled aggregate TotalTokens = %d, want 15", agg.TotalTokens)
	}
	if agg.ToolCalls != 0 {
		t.Fatalf("addTool 应在节流窗口内保持未落盘, ToolCalls = %d, want 0", agg.ToolCalls)
	}

	// 退出前 Flush 把最后一段 dirty 计数落盘。
	m.Flush()

	reloaded2, err := newFileBackedUsageMetrics(path)
	if err != nil {
		t.Fatalf("reload after flush: %v", err)
	}
	agg2 := reloaded2.tenantAggregate(1)
	if agg2.TotalTokens != 15 || agg2.ToolCalls != 1 {
		t.Fatalf("post-flush aggregate = %+v, want TotalTokens=15 ToolCalls=1", agg2)
	}

	// 内存版（path 为空）Flush 应为无操作且不报错。
	mem := newUsageMetrics()
	mem.addLLM(1, 2, llm.Usage{TotalTokens: 5})
	mem.Flush()
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
