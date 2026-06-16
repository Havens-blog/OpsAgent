// Package guardrails provides security guardrails for agent operations.
package guardrails

import (
	"fmt"
	"sync"
	"time"
)

// ApprovalScope defines the scope of an approval.
type ApprovalScope string

const (
	// ScopeOneShot means the approval is valid for a single tool call.
	ScopeOneShot ApprovalScope = "one_shot"

	// ScopeSession means the approval is valid for the entire session.
	ScopeSession ApprovalScope = "session"
)

// ApprovalRequest represents a request for user approval.
type ApprovalRequest struct {
	ToolName       string
	Operation      string
	Params         map[string]interface{}
	Reason         string
	ExpirySeconds  int           // Approval expiry time (default 5 minutes)
	Scope          ApprovalScope
	RequestTime    time.Time
	SessionID      string
}

// IsValid checks if the approval request is valid.
func (r *ApprovalRequest) IsValid() error {
	if r.ToolName == "" {
		return fmt.Errorf("tool name is required")
	}
	if r.Operation == "" {
		return fmt.Errorf("operation is required")
	}
	if r.Scope == "" {
		r.Scope = ScopeOneShot
	}
	if r.ExpirySeconds <= 0 {
		r.ExpirySeconds = 300 // Default 5 minutes
	}
	if r.RequestTime.IsZero() {
		r.RequestTime = time.Now()
	}
	return nil
}

// IsExpired checks if the approval has expired.
func (r *ApprovalRequest) IsExpired() bool {
	expiryTime := r.RequestTime.Add(time.Duration(r.ExpirySeconds) * time.Second)
	return time.Now().After(expiryTime)
}

// ApprovalState represents the state of an approval.
type ApprovalState struct {
	Granted      bool
	GrantedAt    time.Time
	Scope        ApprovalScope
	ExpiresAt    time.Time
	SessionID    string
}

// IsExpired checks if the approval state has expired.
func (s *ApprovalState) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// ApprovalStore defines the interface for storing and retrieving approvals.
type ApprovalStore interface {
	Grant(toolName string, scope ApprovalScope, sessionID string, expirySeconds int) error
	Revoke(toolName string, sessionID string) error
	IsValid(toolName string, sessionID string) (bool, *ApprovalState, error)
	CleanExpired() error
}

// MemoryApprovalStore provides an in-memory implementation of ApprovalStore.
type MemoryApprovalStore struct {
	mu       sync.RWMutex
	approvals map[string]*ApprovalState
}

// NewMemoryApprovalStore creates a new in-memory approval store.
func NewMemoryApprovalStore() *MemoryApprovalStore {
	return &MemoryApprovalStore{
		approvals: make(map[string]*ApprovalState),
	}
}

// Grant grants approval for a tool.
func (s *MemoryApprovalStore) Grant(toolName string, scope ApprovalScope, sessionID string, expirySeconds int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.makeKey(toolName, sessionID)
	s.approvals[key] = &ApprovalState{
		Granted:   true,
		GrantedAt: time.Now(),
		Scope:     scope,
		ExpiresAt: time.Now().Add(time.Duration(expirySeconds) * time.Second),
		SessionID: sessionID,
	}
	return nil
}

// Revoke revokes approval for a tool.
func (s *MemoryApprovalStore) Revoke(toolName string, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.makeKey(toolName, sessionID)
	delete(s.approvals, key)
	return nil
}

// IsValid checks if approval is valid for a tool.
func (s *MemoryApprovalStore) IsValid(toolName string, sessionID string) (bool, *ApprovalState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.makeKey(toolName, sessionID)
	state, exists := s.approvals[key]
	if !exists {
		return false, nil, nil
	}

	if state.IsExpired() {
		delete(s.approvals, key)
		return false, nil, nil
	}

	return true, state, nil
}

// CleanExpired removes all expired approvals.
func (s *MemoryApprovalStore) CleanExpired() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for key, state := range s.approvals {
		if now.After(state.ExpiresAt) {
			delete(s.approvals, key)
		}
	}
	return nil
}

// makeKey creates a unique key for the approval.
func (s *MemoryApprovalStore) makeKey(toolName, sessionID string) string {
	return fmt.Sprintf("%s:%s", toolName, sessionID)
}

// ApprovalManager manages approval requests and states.
type ApprovalManager struct {
	store ApprovalStore
}

// NewApprovalManager creates a new approval manager.
func NewApprovalManager(store ApprovalStore) *ApprovalManager {
	if store == nil {
		store = NewMemoryApprovalStore()
	}
	return &ApprovalManager{
		store: store,
	}
}

// RequireApproval checks if approval is required and requests it if needed.
func (m *ApprovalManager) RequireApproval(req ApprovalRequest) error {
	if err := req.IsValid(); err != nil {
		return fmt.Errorf("invalid approval request: %w", err)
	}

	// Check if approval already exists
	isValid, state, err := m.store.IsValid(req.ToolName, req.SessionID)
	if err != nil {
		return fmt.Errorf("failed to check approval status: %w", err)
	}

	if isValid && state != nil {
		// For one-shot scope, consume the approval
		if state.Scope == ScopeOneShot {
			return m.store.Revoke(req.ToolName, req.SessionID)
		}
		return nil
	}

	// Request approval (to be implemented by the UI layer)
	return &ApprovalRequiredError{
		Request: req,
	}
}

// GrantApproval grants approval for a tool.
func (m *ApprovalManager) GrantApproval(toolName string, scope ApprovalScope, sessionID string, expirySeconds int) error {
	return m.store.Grant(toolName, scope, sessionID, expirySeconds)
}

// RevokeApproval revokes approval for a tool.
func (m *ApprovalManager) RevokeApproval(toolName string, sessionID string) error {
	return m.store.Revoke(toolName, sessionID)
}

// ApprovalRequiredError is returned when approval is required.
type ApprovalRequiredError struct {
	Request ApprovalRequest
}

func (e *ApprovalRequiredError) Error() string {
	return fmt.Sprintf("approval required for tool %s: %s", e.Request.ToolName, e.Request.Reason)
}

// ApprovalRequired returns true if the error is an approval required error.
func ApprovalRequired(err error) bool {
	_, ok := err.(*ApprovalRequiredError)
	return ok
}
