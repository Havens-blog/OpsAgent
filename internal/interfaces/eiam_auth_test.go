package interfaces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Havens-blog/OpsAgent/internal/config"
)

// mintEIAMToken forges a session token exactly the way EIAM/ginx does:
// jwt.NewWithClaims(HS256, RegisteredClaims[session.Claims]).SignedString(key).
// This lets the tests exercise the full verify + tenant-injection contract
// without running EIAM or Redis.
func mintEIAMToken(t *testing.T, key string, uid int64, data map[string]string, exp time.Time) string {
	t.Helper()
	claims := eiamSessionClaims{
		Data: eiamInnerClaims{
			Uid:        uid,
			SSID:       "test-ssid",
			Data:       data,
			Expiration: exp.UnixMilli(),
		},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(key))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return signed
}

func newEIAMServer(t *testing.T, key, cookieName string) *APIServer {
	t.Helper()
	settings := &config.Settings{
		Environment: config.EnvDevelopment,
		Server:      config.ServerConfig{Host: "localhost", Port: 8080},
		LLM:         config.LLMConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "test-key"},
	}
	srv, err := NewAPIServer(settings)
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}
	srv.eiamSessionKey = key
	srv.eiamCookieName = cookieName
	return srv
}

func TestEIAMAuth(t *testing.T) {
	const key = "haven-dev-session-shared-key-2026-10-10-A"
	srv := newEIAMServer(t, key, "ecmdb-token-key")

	// A terminal handler that echoes the injected identity so tests can assert
	// the context was populated.
	final := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"uid":      EIAMUID(r.Context()),
			"tenant":   EIAMTenantID(r.Context()),
			"username": EIAMUsername(r.Context()),
			"is_admin": EIAMIsAdmin(r.Context()),
		})
	}
	handler := srv.eiamAuthMiddleware(final)

	t.Run("valid token -> 200 + identity injected", func(t *testing.T) {
		tok := mintEIAMToken(t, key, 7, map[string]string{
			"tenant_id": "42", "username": "alice", "is_admin": "true",
		}, time.Now().Add(time.Hour))

		req := httptest.NewRequest("GET", "/api/v1/rca", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		if body["tenant"].(float64) != 42 {
			t.Errorf("tenant = %v, want 42", body["tenant"])
		}
		if body["uid"].(float64) != 7 {
			t.Errorf("uid = %v, want 7", body["uid"])
		}
		if body["username"] != "alice" {
			t.Errorf("username = %v, want alice", body["username"])
		}
		if body["is_admin"] != true {
			t.Errorf("is_admin = %v, want true", body["is_admin"])
		}
	})

	t.Run("missing token -> 401 ERR_TENANT_DENIED", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/rca", nil)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if e, _ := body["error"].(map[string]any); e == nil || e["code"] != codeTenantDenied {
			t.Errorf("expected code %s, got %v", codeTenantDenied, body["error"])
		}
	})

	t.Run("wrong signing key -> 401", func(t *testing.T) {
		tok := mintEIAMToken(t, "a-different-wrong-key", 7, map[string]string{"tenant_id": "42"}, time.Now().Add(time.Hour))
		req := httptest.NewRequest("GET", "/api/v1/rca", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for wrong key, got %d", w.Code)
		}
	})

	t.Run("expired token -> 401", func(t *testing.T) {
		tok := mintEIAMToken(t, key, 7, map[string]string{"tenant_id": "42"}, time.Now().Add(-time.Hour))
		req := httptest.NewRequest("GET", "/api/v1/rca", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for expired token, got %d", w.Code)
		}
	})

	t.Run("tenant_id=0 (unselected) -> 403 ERR_TENANT_REQUIRED", func(t *testing.T) {
		tok := mintEIAMToken(t, key, 7, map[string]string{"tenant_id": "0", "username": "bob"}, time.Now().Add(time.Hour))
		req := httptest.NewRequest("GET", "/api/v1/rca", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if e, _ := body["error"].(map[string]any); e == nil || e["code"] != codeTenantRequired {
			t.Errorf("expected code %s, got %v", codeTenantRequired, body["error"])
		}
	})

	t.Run("missing tenant_id claim -> 403", func(t *testing.T) {
		tok := mintEIAMToken(t, key, 7, map[string]string{"username": "bob"}, time.Now().Add(time.Hour))
		req := httptest.NewRequest("GET", "/api/v1/rca", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for missing tenant, got %d", w.Code)
		}
	})

	t.Run("cookie fallback carries the token", func(t *testing.T) {
		tok := mintEIAMToken(t, key, 9, map[string]string{"tenant_id": "5", "username": "carol"}, time.Now().Add(time.Hour))
		req := httptest.NewRequest("GET", "/api/v1/rca", nil)
		req.AddCookie(&http.Cookie{Name: "ecmdb-token-key", Value: tok})
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 via cookie, got %d (body=%s)", w.Code, w.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body["tenant"].(float64) != 5 {
			t.Errorf("tenant = %v, want 5", body["tenant"])
		}
	})

	t.Run("OPTIONS preflight bypasses auth", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/api/v1/rca", nil)
		w := httptest.NewRecorder()
		called := false
		h := srv.eiamAuthMiddleware(func(w http.ResponseWriter, r *http.Request) { called = true })
		h(w, req)
		if !called {
			t.Error("expected OPTIONS to pass through to next handler")
		}
	})

	t.Run("algorithm confusion (alg=none) rejected", func(t *testing.T) {
		// Forge a token with alg=none; must be rejected by the HMAC pin.
		claims := eiamSessionClaims{
			Data:             eiamInnerClaims{Uid: 1, Data: map[string]string{"tenant_id": "42"}},
			RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
		signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatalf("mint none-token: %v", err)
		}
		req := httptest.NewRequest("GET", "/api/v1/rca", nil)
		req.Header.Set("Authorization", "Bearer "+signed)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for alg=none, got %d", w.Code)
		}
	})
}

// ensure context helpers are read in at least one place at compile time
var _ = context.Background
