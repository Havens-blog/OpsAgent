package interfaces

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestConfigAdminWriteGate verifies the is_admin write gate on PUT
// /api/v1/config/llm — a faithful replica of the legacy ops Agent, whose only
// RBAC control is "non-admins cannot mutate settings". The gate runs only when
// EIAM is active. Verified through the real eiamAuthMiddleware chain.
func TestConfigAdminWriteGate(t *testing.T) {
	const key = "haven-dev-session-shared-key-2026-10-10-A"
	srv := newEIAMServer(t, key, "ecmdb-token-key")

	// Route exactly as production does: auth middleware then the handler.
	handler := srv.eiamAuthMiddleware(srv.llmConfigHandler)

	adminTok := mintEIAMToken(t, key, 1, map[string]string{
		"tenant_id": "1", "username": "root", "is_admin": "true",
	}, time.Now().Add(time.Hour))
	userTok := mintEIAMToken(t, key, 2, map[string]string{
		"tenant_id": "1", "username": "alice", "is_admin": "false",
	}, time.Now().Add(time.Hour))

	t.Run("non-admin PUT -> 403 forbidden", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/v1/config/llm", strings.NewReader(`{"provider":"openai","model":"x"}`))
		req.Header.Set("Authorization", "Bearer "+userTok)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("non-admin PUT expected 403, got %d (body=%s)", w.Code, w.Body.String())
		}
	})

	t.Run("admin PUT passes the gate (invalid body -> 400, not 403)", func(t *testing.T) {
		// Empty body makes saveLLMConfig fail decoding with 400, proving the
		// request got PAST the admin gate without persisting anything.
		req := httptest.NewRequest("PUT", "/api/v1/config/llm", strings.NewReader(``))
		req.Header.Set("Authorization", "Bearer "+adminTok)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code == http.StatusForbidden {
			t.Fatalf("admin PUT must NOT be gated, got 403 (body=%s)", w.Body.String())
		}
	})

	t.Run("non-admin GET is NOT gated", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/config/llm", nil)
		req.Header.Set("Authorization", "Bearer "+userTok)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("non-admin GET expected 200, got %d (body=%s)", w.Code, w.Body.String())
		}
	})
}
