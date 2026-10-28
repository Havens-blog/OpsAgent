// Package interfaces provides HTTP handlers for the OpsAgent API.
package interfaces

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/agent"
	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

// Agent task statuses.
const (
	TaskStatusPending          = "pending"
	TaskStatusRunning          = "running"
	TaskStatusAwaitingApproval = "awaiting_approval"
	TaskStatusCompleted        = "completed"
	TaskStatusFailed           = "failed"
	TaskStatusRejected         = "rejected"
)

// AgentTask is the async, tenant-scoped task record surfaced by
// POST /api/v1/agents/{id}/tasks and GET /api/v1/tasks/{id}.
type AgentTask struct {
	ID           string                 `json:"id"`
	AgentID      string                 `json:"agent_id"`
	Description  string                 `json:"description"`
	Params       map[string]interface{} `json:"params,omitempty"`
	ClientTaskID string                 `json:"client_task_id,omitempty"`
	Status       string                 `json:"status"`
	Result       *agent.ExecutionResult `json:"result,omitempty"`
	Error        string                 `json:"error,omitempty"`
	ApprovalID   string                 `json:"approval_id,omitempty"`
	TenantID     int64                  `json:"tenant_id"`
	UserID       int64                  `json:"user_id"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// AgentTaskStore persists async agent tasks.
type AgentTaskStore interface {
	Create(ctx context.Context, t *AgentTask) error
	Get(ctx context.Context, id string) (*AgentTask, error)
	List(ctx context.Context) ([]*AgentTask, error)
	Update(ctx context.Context, t *AgentTask) error
	FindByClientTaskID(ctx context.Context, agentID, clientTaskID string) (*AgentTask, bool)
}

// MemoryAgentTaskStore is an in-memory, tenant-isolated AgentTaskStore.
type MemoryAgentTaskStore struct {
	mu    sync.RWMutex
	items map[string]*AgentTask
}

// NewMemoryAgentTaskStore creates an empty in-memory task store.
func NewMemoryAgentTaskStore() *MemoryAgentTaskStore {
	return &MemoryAgentTaskStore{items: make(map[string]*AgentTask)}
}

// Create stores a task, stamping tenant/user from the request context so the
// tenant can never be spoofed by a caller-supplied value.
func (s *MemoryAgentTaskStore) Create(ctx context.Context, t *AgentTask) error {
	if t == nil || t.ID == "" {
		return fmt.Errorf("task id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t.TenantID = tenant.IDFrom(ctx)
	t.UserID = EIAMUID(ctx)
	now := time.Now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	s.items[t.ID] = t
	return nil
}

// Get returns a copy of a task, enforcing tenant isolation (cross-tenant reads
// report not-found).
func (s *MemoryAgentTaskStore) Get(ctx context.Context, id string) (*AgentTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.items[id]
	if !ok || t.TenantID != tenant.IDFrom(ctx) {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	return s.clone(t), nil
}

// List returns the calling tenant's tasks, newest first.
func (s *MemoryAgentTaskStore) List(ctx context.Context) ([]*AgentTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tid := tenant.IDFrom(ctx)
	out := make([]*AgentTask, 0, len(s.items))
	for _, t := range s.items {
		if t.TenantID != tid {
			continue
		}
		out = append(out, s.clone(t))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// Update replaces the stored task with the supplied copy, preserving tenant and
// user identity from the existing record.
func (s *MemoryAgentTaskStore) Update(ctx context.Context, t *AgentTask) error {
	if t == nil || t.ID == "" {
		return fmt.Errorf("task id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cur, ok := s.items[t.ID]
	if !ok || cur.TenantID != tenant.IDFrom(ctx) {
		return fmt.Errorf("task not found: %s", t.ID)
	}

	t.TenantID = cur.TenantID
	t.UserID = cur.UserID
	t.UpdatedAt = time.Now()
	s.items[t.ID] = t
	return nil
}

// FindByClientTaskID returns a task matching agent + client_task_id within the
// calling tenant. Used for idempotent submission.
func (s *MemoryAgentTaskStore) FindByClientTaskID(ctx context.Context, agentID, clientTaskID string) (*AgentTask, bool) {
	if clientTaskID == "" {
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	tid := tenant.IDFrom(ctx)
	for _, t := range s.items {
		if t.AgentID == agentID && t.ClientTaskID == clientTaskID && t.TenantID == tid {
			return s.clone(t), true
		}
	}
	return nil, false
}

func (s *MemoryAgentTaskStore) clone(t *AgentTask) *AgentTask {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}
