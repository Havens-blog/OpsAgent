// Package interfaces provides HTTP handlers for the OpsAgent API.
package interfaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
	// ClientMessageID is the client id of the user message that triggered
	// the approval, so a restored page can retry with the same id.
	ClientMessageID string `json:"client_message_id,omitempty"`
}

// PendingApprovalStore keeps pending approvals for the HTTP layer. When path
// is non-empty, mutations write a full snapshot to disk after each change.
type PendingApprovalStore struct {
	mu    sync.RWMutex
	path  string
	items map[string]*PendingApproval
}

// persistedApprovals is the on-disk snapshot shape. The loader also accepts a
// bare map[string]*PendingApproval for older snapshots.
type persistedApprovals struct {
	Items map[string]*PendingApproval `json:"items"`
}

// NewPendingApprovalStore creates an empty in-memory pending approval store.
func NewPendingApprovalStore() *PendingApprovalStore {
	return &PendingApprovalStore{items: make(map[string]*PendingApproval)}
}

// NewFileBackedPendingApprovalStore loads the snapshot at path, or starts
// empty when it does not exist. A corrupt or unreadable file returns an error.
func NewFileBackedPendingApprovalStore(path string) (*PendingApprovalStore, error) {
	if path == "" {
		return nil, fmt.Errorf("pending approval store path is required")
	}
	s := &PendingApprovalStore{path: path, items: make(map[string]*PendingApproval)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads and decodes the snapshot. A missing file is the empty store. Only
// pending, unexpired entries are restored so handled or stale approvals do not
// come back to life after a restart.
func (s *PendingApprovalStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read approval store %s: %w", s.path, err)
	}

	var withItems persistedApprovals
	if err := json.Unmarshal(data, &withItems); err != nil {
		return fmt.Errorf("parse approval store %s: %w", s.path, err)
	}
	items := withItems.Items
	if items == nil {
		items = make(map[string]*PendingApproval)
		if err := json.Unmarshal(data, &items); err != nil {
			return fmt.Errorf("parse approval store %s: %w", s.path, err)
		}
	}

	now := time.Now()
	for id, p := range items {
		if p != nil && p.Status == "pending" && now.Before(p.ExpiresAt) {
			s.items[id] = p
		}
	}
	return nil
}

// persistLocked writes the current state to a temp file in the same directory
// and atomically renames it over the target. Callers must hold the write lock.
func (s *PendingApprovalStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create approval store dir: %w", err)
	}

	data, err := json.MarshalIndent(persistedApprovals{Items: s.items}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode approval store: %w", err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create approval store temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write approval store temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close approval store temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace approval store: %w", err)
	}
	return nil
}

// Create stores a pending approval.
func (s *PendingApprovalStore) Create(p *PendingApproval) error {
	if p == nil || p.ApprovalID == "" {
		return fmt.Errorf("approval id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[p.ApprovalID] = p
	if err := s.persistLocked(); err != nil {
		delete(s.items, p.ApprovalID)
		return err
	}
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

// ListPending returns a user's pending, unexpired approvals newest first.
func (s *PendingApprovalStore) ListPending(tenantID, userID int64) []*PendingApproval {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	out := make([]*PendingApproval, 0)
	for _, p := range s.items {
		if p.TenantID != tenantID || p.UserID != userID || p.Status != "pending" || now.After(p.ExpiresAt) {
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

// ListPendingForConversation returns a user's pending, unexpired approvals for
// one conversation, oldest first. It never returns nil.
func (s *PendingApprovalStore) ListPendingForConversation(tenantID, userID int64, conversationID string) []*PendingApproval {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	out := make([]*PendingApproval, 0)
	for _, p := range s.items {
		if p.TenantID != tenantID || p.UserID != userID || p.ConversationID != conversationID ||
			p.Status != "pending" || now.After(p.ExpiresAt) {
			continue
		}
		// Copy so the response is serialized outside the lock without racing SetStatus.
		cp := *p
		out = append(out, &cp)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ApprovalID < out[j].ApprovalID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
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
	old := p.Status
	p.Status = status
	if err := s.persistLocked(); err != nil {
		p.Status = old
		return err
	}
	return nil
}

// approvalsHandler handles GET /api/v1/approvals.
func (s *APIServer) approvalsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
		return
	}

	items := s.pendingApprovals.ListPending(EIAMTenantID(r.Context()), EIAMUID(r.Context()))
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
	if err := s.pendingApprovals.SetStatus(id, "approved"); err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "store_error", "Failed to persist approval status", err.Error())
		return
	}

	// If this approval was created for an async agent task, resume execution.
	s.maybeReexecuteTask(r.Context(), p)

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
	if err := s.pendingApprovals.SetStatus(id, "rejected"); err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "store_error", "Failed to persist approval status", err.Error())
		return
	}

	// If this approval was created for an async agent task, mark it rejected.
	s.maybeRejectTask(r.Context(), p)

	s.writeJSONResponse(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// maybeReexecuteTask resumes a task whose pending approval was just granted.
// ConversationID doubles as the task id for agent-task approvals; chat
// approvals (ConversationID == conversation id) never match a task here.
func (s *APIServer) maybeReexecuteTask(ctx context.Context, p *PendingApproval) {
	if s.taskStore == nil {
		return
	}
	task, err := s.taskStore.Get(ctx, p.ConversationID)
	if err != nil || task.ApprovalID != p.ApprovalID {
		return
	}

	task.Status = TaskStatusRunning
	task.ApprovalID = ""
	task.Error = ""
	if err := s.taskStore.Update(ctx, task); err != nil {
		return
	}

	go s.runAgentTask(task.AgentID, task.ID, task.TenantID, task.UserID)
}

// maybeRejectTask marks a task rejected when its pending approval is denied.
func (s *APIServer) maybeRejectTask(ctx context.Context, p *PendingApproval) {
	if s.taskStore == nil {
		return
	}
	task, err := s.taskStore.Get(ctx, p.ConversationID)
	if err != nil || task.ApprovalID != p.ApprovalID {
		return
	}

	task.Status = TaskStatusRejected
	task.ApprovalID = ""
	task.Error = "approval rejected"
	_ = s.taskStore.Update(ctx, task)
}
