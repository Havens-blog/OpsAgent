// Package guardrails provides security guardrails for agent operations.
package guardrails

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	Granted   bool
	GrantedAt time.Time
	Scope     ApprovalScope
	ExpiresAt time.Time
	SessionID string
	ParamHash string
}

// IsExpired checks if the approval state has expired.
func (s *ApprovalState) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// ApprovalStore defines the interface for storing and retrieving approvals.
// The paramHash binds an approval to the exact parameter version that was
// approved, so reusing an approval with different arguments is impossible.
type ApprovalStore interface {
	Grant(toolName string, scope ApprovalScope, sessionID string, paramHash string, expirySeconds int) error
	Revoke(toolName string, sessionID string, paramHash string) error
	IsValid(toolName string, sessionID string, paramHash string) (bool, *ApprovalState, error)
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

// Grant grants approval for a tool and parameter version.
func (s *MemoryApprovalStore) Grant(toolName string, scope ApprovalScope, sessionID string, paramHash string, expirySeconds int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.makeKey(toolName, sessionID, paramHash)
	s.approvals[key] = &ApprovalState{
		Granted:   true,
		GrantedAt: time.Now(),
		Scope:     scope,
		ExpiresAt: time.Now().Add(time.Duration(expirySeconds) * time.Second),
		SessionID: sessionID,
		ParamHash: paramHash,
	}
	return nil
}

// Revoke revokes approval for a tool and parameter version.
func (s *MemoryApprovalStore) Revoke(toolName string, sessionID string, paramHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.makeKey(toolName, sessionID, paramHash)
	delete(s.approvals, key)
	return nil
}

// IsValid checks if approval is valid for a tool and parameter version.
func (s *MemoryApprovalStore) IsValid(toolName string, sessionID string, paramHash string) (bool, *ApprovalState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.makeKey(toolName, sessionID, paramHash)
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

// makeKey creates a unique key for the approval, bound to the parameter hash.
func (s *MemoryApprovalStore) makeKey(toolName, sessionID, paramHash string) string {
	return fmt.Sprintf("%s:%s:%s", toolName, sessionID, paramHash)
}

// paramHash computes a stable hash over the tool's parameters. json.Marshal
// emits maps with sorted keys and no insignificant whitespace, which keeps the
// hash independent of map iteration order.
func paramHash(params map[string]interface{}) string {
	if params == nil {
		params = map[string]interface{}{}
	}
	b, err := json.Marshal(params)
	if err != nil {
		b = []byte(fmt.Sprintf("%v", params))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ParamHash exposes the canonical parameter hash to other packages.
func ParamHash(params map[string]interface{}) string {
	return paramHash(params)
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

	ph := paramHash(req.Params)

	// Check if approval already exists for this exact parameter version.
	isValid, state, err := m.store.IsValid(req.ToolName, req.SessionID, ph)
	if err != nil {
		return fmt.Errorf("failed to check approval status: %w", err)
	}

	if isValid && state != nil {
		// For one-shot scope, consume the approval
		if state.Scope == ScopeOneShot {
			return m.store.Revoke(req.ToolName, req.SessionID, ph)
		}
		return nil
	}

	// Request approval (to be implemented by the UI layer)
	return &ApprovalRequiredError{
		Request: req,
	}
}

// GrantApproval grants approval for a tool without a parameter binding. Kept
// for backward compatibility with callers that do not track parameters; it is
// bound to an empty parameter hash and will never satisfy RequireApproval.
func (m *ApprovalManager) GrantApproval(toolName string, scope ApprovalScope, sessionID string, expirySeconds int) error {
	return m.store.Grant(toolName, scope, sessionID, "", expirySeconds)
}

// RevokeApproval revokes approval for a tool without a parameter binding.
func (m *ApprovalManager) RevokeApproval(toolName string, sessionID string) error {
	return m.store.Revoke(toolName, sessionID, "")
}

// GrantApprovalWithHash grants approval bound to a specific parameter hash.
func (m *ApprovalManager) GrantApprovalWithHash(toolName string, scope ApprovalScope, sessionID string, paramHash string, expirySeconds int) error {
	return m.store.Grant(toolName, scope, sessionID, paramHash, expirySeconds)
}

// RevokeApprovalWithHash revokes approval bound to a specific parameter hash.
func (m *ApprovalManager) RevokeApprovalWithHash(toolName string, sessionID string, paramHash string) error {
	return m.store.Revoke(toolName, sessionID, paramHash)
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
