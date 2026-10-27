package interfaces

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

// EIAM session integration.
//
// OpsAgent does NOT run an OIDC flow against EIAM. The self-hosted EIAM
// platform issues a session token that is a standard HS256-signed JWT
// (github.com/ecodeclub/ginx internal jwt manager, golang-jwt/v5). We verify
// that token locally with the shared secret — the read path is pure JWT and
// never touches Redis, matching how the legacy ops Agent integrates.
//
// Byte-for-byte compatibility notes (from ginx source):
//   - signing method is HS256 (ginx NewOptions default, not overridden);
//   - the JWT payload is RegisteredClaims[session.Claims]:
//     {"data":{"Uid":..,"SSID":..,"Data":{..},"Expiration":..}, exp, iat, ...};
//   - session.Claims has NO json tags, so Go serializes its fields with their
//     capitalized names (Uid/SSID/Data/Expiration) — we mirror that exactly;
//   - identity + tenant live in Data: tenant_id, username, is_admin (strings).

// Error codes mirror the legacy ops Agent so platform clients see the same
// contract: 401 for unauthenticated, 403 for authenticated-but-no-tenant.
const (
	codeTenantDenied   = "ERR_TENANT_DENIED"
	codeTenantRequired = "ERR_TENANT_REQUIRED"
)

// eiamContextKey is a private type for request-context keys so EIAM identity
// values can never collide with other packages' context keys.
type eiamContextKey string

const (
	eiamUIDKey      eiamContextKey = "eiam_uid"
	eiamUsernameKey eiamContextKey = "eiam_username"
	eiamIsAdminKey  eiamContextKey = "eiam_is_admin"
)

// eiamInnerClaims mirrors ginx session.Claims. The source struct has no json
// tags, so the JSON keys are the capitalized Go field names.
type eiamInnerClaims struct {
	Uid        int64             `json:"Uid"`
	SSID       string            `json:"SSID"`
	Data       map[string]string `json:"Data"`
	Expiration int64             `json:"Expiration"`
}

// eiamSessionClaims mirrors ginx internal/jwt RegisteredClaims[session.Claims]:
// the inner payload under "data" plus the standard registered claims (exp/iat).
type eiamSessionClaims struct {
	Data eiamInnerClaims `json:"data"`
	jwt.RegisteredClaims
}

// extractEIAMToken pulls the session token from the request: Authorization
// Bearer header first (used by both the frontend proxy and service-to-service
// callers), then the EIAM session cookie as a browser fallback.
func (s *APIServer) extractEIAMToken(r *http.Request) string {
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		const bearer = "Bearer "
		if strings.HasPrefix(auth, bearer) {
			return strings.TrimSpace(auth[len(bearer):])
		}
		return auth
	}
	if s.eiamCookieName != "" {
		if c, err := r.Cookie(s.eiamCookieName); err == nil {
			return strings.TrimSpace(c.Value)
		}
	}
	return ""
}

// verifyEIAMToken validates the HS256 JWT with the shared secret. The signing
// method is pinned to HMAC to prevent algorithm-confusion attacks; golang-jwt
// validates the standard exp claim during parsing.
func (s *APIServer) verifyEIAMToken(token string) (*eiamSessionClaims, error) {
	claims := &eiamSessionClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(s.eiamSessionKey), nil
	})
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

// eiamAuthMiddleware enforces EIAM session authentication and tenant selection.
//
// Contract (identical to the legacy ops Agent):
//   - missing/invalid token           -> 401 ERR_TENANT_DENIED
//   - valid session but tenant_id == 0 -> 403 ERR_TENANT_REQUIRED (0 means the
//     user has not yet selected a tenant; it is NOT a wildcard)
//   - otherwise inject uid/tenant/username/is_admin into the request context.
//
// The tenant is taken ONLY from the session claims; any client-supplied tenant
// (X-Tenant-ID header, request body) is ignored.
func (s *APIServer) eiamAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next(w, r)
			return
		}

		token := s.extractEIAMToken(r)
		if token == "" {
			s.writeErrorResponse(w, http.StatusUnauthorized, codeTenantDenied,
				"认证失败：未登录或令牌无效", "")
			return
		}

		claims, err := s.verifyEIAMToken(token)
		if err != nil {
			s.writeErrorResponse(w, http.StatusUnauthorized, codeTenantDenied,
				"认证失败：未登录或令牌无效", "")
			return
		}

		// Tenant is required and must be non-zero. tenant_id == 0 is EIAM's
		// "awaiting tenant selection" temporary credential, never a wildcard.
		var tid int64
		if raw, ok := claims.Data.Data["tenant_id"]; ok && raw != "" {
			tid, _ = strconv.ParseInt(raw, 10, 64)
		}
		if tid == 0 {
			s.writeErrorResponse(w, http.StatusForbidden, codeTenantRequired,
				"未选定租户：请先选择租户后重试", "")
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, eiamUIDKey, claims.Data.Uid)
		// Tenant goes into the SHARED tenant context key so storage layers
		// (internal/rca, ...) can read it to enforce per-tenant isolation.
		ctx = tenant.WithID(ctx, tid)
		if u, ok := claims.Data.Data["username"]; ok {
			ctx = context.WithValue(ctx, eiamUsernameKey, u)
		}
		ctx = context.WithValue(ctx, eiamIsAdminKey, claims.Data.Data["is_admin"] == "true")

		next(w, r.WithContext(ctx))
	}
}

// EIAMTenantID returns the authenticated tenant id from the request context
// (0 when EIAM auth did not run). Handlers use this to scope data per tenant.
// It reads the shared tenant context key written by the auth middleware.
func EIAMTenantID(ctx context.Context) int64 {
	return tenant.IDFrom(ctx)
}

// EIAMUID returns the authenticated user id from the request context.
func EIAMUID(ctx context.Context) int64 {
	v, _ := ctx.Value(eiamUIDKey).(int64)
	return v
}

// EIAMUsername returns the authenticated username from the request context.
func EIAMUsername(ctx context.Context) string {
	v, _ := ctx.Value(eiamUsernameKey).(string)
	return v
}

// EIAMIsAdmin reports whether the authenticated user is an EIAM admin.
func EIAMIsAdmin(ctx context.Context) bool {
	v, _ := ctx.Value(eiamIsAdminKey).(bool)
	return v
}
