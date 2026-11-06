package interfaces

import (
	"encoding/json"
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
	Kind        string  `json:"kind"`
	Service     string  `json:"service"`
	Content     string  `json:"content"`
	Confidence  float64 `json:"confidence"`
	Version     string  `json:"version,omitempty"`
	ExpiresAt   string  `json:"expires_at,omitempty"`
	ConfirmedBy string  `json:"confirmed_by"`
}

// confirmMemoryFactHandler handles POST /api/v1/memory.
// 只有在这里显式提交确认人与确认动作的事实才会进入长期记忆。
func (s *APIServer) confirmMemoryFactHandler(w http.ResponseWriter, r *http.Request) {
	var req confirmMemoryFactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_request", "Failed to decode request", err.Error())
		return
	}
	req.Service = strings.TrimSpace(req.Service)
	req.Content = strings.TrimSpace(req.Content)
	req.ConfirmedBy = strings.TrimSpace(req.ConfirmedBy)
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
	if req.ConfirmedBy == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "confirmation_required", "confirmed_by is required: only human-confirmed facts enter memory", "")
		return
	}
	if req.Confidence < 0 || req.Confidence > 1 {
		s.writeErrorResponse(w, http.StatusBadRequest, "invalid_confidence", "confidence must be within [0,1]", "")
		return
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
		Kind:        kind,
		Scope:       memory.ScopeService,
		Service:     req.Service,
		Content:     req.Content,
		Confidence:  req.Confidence,
		TenantID:    tenant.IDFrom(r.Context()),
		Version:     req.Version,
		ConfirmedBy: req.ConfirmedBy,
		ConfirmedAt: time.Now(),
		ExpiresAt:   expires,
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
