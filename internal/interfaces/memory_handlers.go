package interfaces

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/memory"
	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

var validMemoryKinds = map[string]memory.FactKind{
	string(memory.FactRootCause): memory.FactRootCause,
	string(memory.FactFragility): memory.FactFragility,
}

type confirmMemoryFactRequest struct {
	Kind       string  `json:"kind"`
	Service    string  `json:"service"`
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence"`
	Version    string  `json:"version,omitempty"`
	ExpiresAt  string  `json:"expires_at,omitempty"`
}

// confirmMemoryFactHandler handles POST /api/v1/memory.
// 确认主体必须来自服务端认证上下文，客户端自报的 confirmed_by 一律忽略：
// 只有已认证的人类主体才能把事实确认为人工确认，机器/未认证调用被拒绝。
func (s *APIServer) confirmMemoryFactHandler(w http.ResponseWriter, r *http.Request) {
	var req confirmMemoryFactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
		return
	}
	req.Service = strings.TrimSpace(req.Service)
	req.Content = strings.TrimSpace(req.Content)
	req.Version = strings.TrimSpace(req.Version)

	kind, ok := validMemoryKinds[strings.TrimSpace(req.Kind)]
	if !ok {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_kind", "kind must be root_cause or fragility", "")
		return
	}
	if req.Service == "" || req.Content == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "service and content are required", "")
		return
	}
	if req.Confidence < 0 || req.Confidence > 1 {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_confidence", "confidence must be within [0,1]", "")
		return
	}

	// 确认主体从认证上下文获取，不接受客户端自报的 confirmed_by。
	// uid==0 表示未认证或机器凭证，不能制造 human-confirmed 记录。
	uid := tenant.UserIDFrom(r.Context())
	if uid == 0 {
		s.writeErrorResponse(w, http.StatusForbidden, "confirmation_denied",
			"人工确认需要已认证的人类主体，机器凭证或未认证调用被拒绝", "")
		return
	}
	username := EIAMUsername(r.Context())
	if strings.TrimSpace(username) == "" {
		username = fmt.Sprintf("uid-%d", uid)
	}

	var expires time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			s.writeErrorResponse(w, http.StatusBadRequest, "invalid_expires_at", "expires_at must be RFC3339", err.Error())
			return
		}
		expires = t
	}

	fact := memory.Fact{
		Kind:           kind,
		Scope:          memory.ScopeService,
		Service:        req.Service,
		Content:        req.Content,
		Confidence:     req.Confidence,
		TenantID:       tenant.IDFrom(r.Context()),
		Version:        req.Version,
		ConfirmedBy:    username,
		ConfirmedByUID: uid,
		ConfirmedAt:    time.Now(),
		ExpiresAt:      expires,
	}
	stored, err := s.faultMemory.Record(fact)
	if err != nil {
		// 作用域门槛拒绝（非服务级、置信度不足等）按请求错误返回，不静默丢弃
		s.writeErrorResponse(w, http.StatusBadRequest, "rejected_by_scope_gate", "Fact rejected by memory scope gate", err.Error())
		return
	}
	s.writeJSONResponse(w, http.StatusCreated, map[string]interface{}{
		"stored":    stored,
		"duplicate": !stored,
	})
}

// queryMemoryFactHandler handles GET /api/v1/memory?service=&version=.
// 只返回当前租户未过期的事实。
func (s *APIServer) queryMemoryHandler(w http.ResponseWriter, r *http.Request) {
	facts := s.faultMemory.Query(memory.Query{
		TenantID: tenant.IDFrom(r.Context()),
		Service:  strings.TrimSpace(r.URL.Query().Get("service")),
		Version:  strings.TrimSpace(r.URL.Query().Get("version")),
	})
	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"facts": facts,
		"count": len(facts),
	})
}
