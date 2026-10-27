// Package interfaces provides HTTP handlers for the OpsAgent API.
package interfaces

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/guardrails"
)

// PendingApproval is a tenant/user-scoped approval request awaiting a decision.
type PendingApproval struct {
	ApprovalID     string                   `json:"approval_id"`
	ToolName       string                   `json:"tool_name"`
	Params         map[string]interface{}   `json:"params"`
	Reason         string                   `json:"reason"`
	ConversationID string                   `json:"conversation_id"`
	TenantID       int64                    `json:"tenant_id"`
	UserID         int64                    `json:"user_id"`
	ParamHash      string                   `json:"param_hash"`
	Scope          guardrails.ApprovalScope `json:"scope"`
	ExpirySeconds  int                      `json:"expiry_seconds"`
	ExpiresAt      time.Time                `json:"expires_at"`
	Status         string                   `json:"status"`
	CreatedAt      time.Time                `json:"created_at"`
}

// PendingApprovalStore keeps in-memory pending approvals for the HTTP layer.
type PendingApprovalStore struct {
	mu    sync.RWMutex
	items map[string]*PendingApproval
}

// NewPendingApprovalStore creates an empty pending approval store.
func NewPendingApprovalStore() *PendingApprovalStore {
	return &PendingApprovalStore{items: make(map[string]*PendingApproval)}
}

// Create stores a pending approval.
func (s *PendingApprovalStore) Create(p *PendingApproval) error {
	if p == nil || p.ApprovalID == "" {
		return fmt.Errorf("approval id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[p.ApprovalID] = p
	return nil
}

// Get returns a pending approval by id.
func (s *PendingApprovalStore) Get(id string) (*PendingApproval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.items[id]
	if !ok {
		return nil, fmt.Errorf("approval not found: %s", id)
	}
	return p, nil
}

// ListPending returns a tenant's pending, unexpired approvals newest first.
func (s *PendingApprovalStore) ListPending(tenantID int64) []*PendingApproval {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	out := make([]*PendingApproval, 0)
	for _, p := range s.items {
		if p.TenantID != tenantID || p.Status != "pending" || now.After(p.ExpiresAt) {
			continue
		}
		out = append(out, p)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ApprovalID < out[j].ApprovalID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// SetStatus updates the status of a pending approval.
func (s *PendingApprovalStore) SetStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.items[id]
	if !ok {
		return fmt.Errorf("approval not found: %s", id)
	}
	p.Status = status
	return nil
}

// approvalsHandler handles GET /api/v1/approvals.
func (s *APIServer) approvalsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
		return
	}

	items := s.pendingApprovals.ListPending(EIAMTenantID(r.Context()))
	s.writeJSONResponse(w, http.StatusOK, map[string]interface{}{
		"approvals": items,
		"count":     len(items),
	})
}

// approvalHandler dispatches POST /api/v1/approvals/{id}/{action}.
func (s *APIServer) approvalHandler(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/approvals/"), "/")
	parts := strings.Split(rest, "/")

	if len(parts) == 2 && parts[0] != "" {
		switch parts[1] {
		case "approve":
			s.approveApprovalHandler(w, r, parts[0])
		case "reject":
			s.rejectApprovalHandler(w, r, parts[0])
		default:
			s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Approval endpoint not found", r.URL.Path)
		}
		return
	}

	s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Approval endpoint not found", r.URL.Path)
}

// resolvePendingApproval validates ownership, status, and expiry.
func (s *APIServer) resolvePendingApproval(w http.ResponseWriter, r *http.Request, id string) (*PendingApproval, bool) {
	p, err := s.pendingApprovals.Get(id)
	if err != nil {
		s.writeErrorResponse(w, http.StatusNotFound, "not_found", "Approval not found", "")
		return nil, false
	}
	if p.TenantID != EIAMTenantID(r.Context()) || p.UserID != EIAMUID(r.Context()) {
		s.writeErrorResponse(w, http.StatusForbidden, "forbidden", "Approval does not belong to the current tenant/user", "")
		return nil, false
	}
	if p.Status != "pending" {
		s.writeErrorResponse(w, http.StatusConflict, "invalid_state", "Approval is not pending", "")
		return nil, false
	}
	if time.Now().After(p.ExpiresAt) {
		s.writeErrorResponse(w, http.StatusGone, "expired", "Approval has expired", "")
		return nil, false
	}
	return p, true
}

// approveApprovalHandler handles POST /api/v1/approvals/{id}/approve.
func (s *APIServer) approveApprovalHandler(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
		return
	}
	p, ok := s.resolvePendingApproval(w, r, id)
	if !ok {
		return
	}

	if err := s.approvalMgr.GrantApprovalWithHash(p.ToolName, p.Scope, p.ConversationID, p.ParamHash, p.ExpirySeconds); err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "store_error", "Failed to grant approval", err.Error())
		return
	}
	_ = s.pendingApprovals.SetStatus(id, "approved")

	s.writeJSONResponse(w, http.StatusOK, map[string]string{"status": "approved"})
}

// rejectApprovalHandler handles POST /api/v1/approvals/{id}/reject.
func (s *APIServer) rejectApprovalHandler(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
		return
	}
	p, ok := s.resolvePendingApproval(w, r, id)
	if !ok {
		return
	}

	_ = s.approvalMgr.RevokeApprovalWithHash(p.ToolName, p.ConversationID, p.ParamHash)
	_ = s.pendingApprovals.SetStatus(id, "rejected")

	s.writeJSONResponse(w, http.StatusOK, map[string]string{"status": "rejected"})
}
