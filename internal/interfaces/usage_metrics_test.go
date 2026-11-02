// Package interfaces provides tests for REST API handlers.
package interfaces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/llm"
)

func TestUsageMetricsAggregate(t *testing.T) {
	m := newUsageMetrics()
	m.addLLM(1, 2, llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	m.addLLM(1, 2, llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	m.addLLM(1, 3, llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
	m.addTool(1, 2)
	m.addTool(1, 3)

	tenant := m.tenantAggregate(1)
	if tenant.PromptTokens != 210 || tenant.CompletionTokens != 105 || tenant.TotalTokens != 315 ||
		tenant.LLMCalls != 3 || tenant.ToolCalls != 2 {
		t.Fatalf("tenant aggregate = %+v, want {210 105 315 3 2}", tenant)
	}

	user := m.userAggregate(1, 2)
	if user.PromptTokens != 200 || user.CompletionTokens != 100 || user.TotalTokens != 300 ||
		user.LLMCalls != 2 || user.ToolCalls != 1 {
		t.Fatalf("user aggregate = %+v, want {200 100 300 2 1}", user)
	}
}

func TestUsageEndpoint(t *testing.T) {
	server, err := NewAPIServer(&config.Settings{
		Environment: config.EnvDevelopment,
		Server:      config.ServerConfig{Host: "localhost", Port: 8080},
	})
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}
	t.Cleanup(func() { server.Shutdown(context.Background()) })

	server.usage.addLLM(1, 2, llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})
	server.usage.addTool(1, 2)

	req := withIdentity(httptest.NewRequest(http.MethodGet, "/api/v1/audit/usage", nil), 1, 2)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Tenant struct {
			TenantID     int64 `json:"tenant_id"`
			PromptTokens int64 `json:"prompt_tokens"`
			ToolCalls    int64 `json:"tool_calls"`
		} `json:"tenant"`
		User struct {
			TenantID     int64 `json:"tenant_id"`
			UserID       int64 `json:"user_id"`
			PromptTokens int64 `json:"prompt_tokens"`
			ToolCalls    int64 `json:"tool_calls"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, w.Body.String())
	}

	if resp.Tenant.TenantID != 1 || resp.User.TenantID != 1 || resp.User.UserID != 2 {
		t.Fatalf("identity fields = tenant_id %d, user tenant_id %d, user_id %d; body = %s",
			resp.Tenant.TenantID, resp.User.TenantID, resp.User.UserID, w.Body.String())
	}
	if resp.Tenant.PromptTokens != 100 || resp.Tenant.ToolCalls != 1 {
		t.Fatalf("tenant counters = prompt %d, tool_calls %d; body = %s",
			resp.Tenant.PromptTokens, resp.Tenant.ToolCalls, w.Body.String())
	}
	if resp.User.PromptTokens != 100 || resp.User.ToolCalls != 1 {
		t.Fatalf("user counters = prompt %d, tool_calls %d; body = %s",
			resp.User.PromptTokens, resp.User.ToolCalls, w.Body.String())
	}
}
