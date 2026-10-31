// Package interfaces provides HTTP handlers for the OpsAgent API.
package interfaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// MemoryAgentTaskStore is an in-memory, tenant-isolated AgentTaskStore. When
// path is non-empty, mutations also write a full snapshot to disk.
type MemoryAgentTaskStore struct {
	mu    sync.RWMutex
	path  string
	items map[string]*AgentTask
}

// persistedTasks is the on-disk snapshot shape.
type persistedTasks struct {
	Items map[string]*AgentTask `json:"items"`
}

// NewMemoryAgentTaskStore creates an empty in-memory task store.
func NewMemoryAgentTaskStore() *MemoryAgentTaskStore {
	return &MemoryAgentTaskStore{items: make(map[string]*AgentTask)}
}

// NewFileBackedAgentTaskStore loads the snapshot at path, or starts empty when
// it does not exist. A corrupt or unreadable file returns an error.
func NewFileBackedAgentTaskStore(path string) (*MemoryAgentTaskStore, error) {
	if path == "" {
		return nil, fmt.Errorf("task store path is required")
	}
	s := &MemoryAgentTaskStore{path: path, items: make(map[string]*AgentTask)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads and decodes the snapshot. A missing file is the empty store.
// Running and pending tasks are rewritten to failed because their execution
// goroutines died with the previous process.
func (s *MemoryAgentTaskStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read task store %s: %w", s.path, err)
	}

	var snapshot persistedTasks
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("parse task store %s: %w", s.path, err)
	}

	now := time.Now()
	for id, t := range snapshot.Items {
		if t == nil {
			continue
		}
		if t.Status == TaskStatusRunning || t.Status == TaskStatusPending {
			t.Status = TaskStatusFailed
			t.Error = "task interrupted by server restart"
			t.UpdatedAt = now
		}
		s.items[id] = t
	}
	return nil
}

// persistLocked writes the current state to a temp file in the same directory
// and atomically renames it over the target. Callers must hold the write lock.
func (s *MemoryAgentTaskStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create task store dir: %w", err)
	}

	data, err := json.MarshalIndent(persistedTasks{Items: s.items}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode task store: %w", err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create task store temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write task store temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close task store temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace task store: %w", err)
	}
	return nil
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
	return s.persistLocked()
}

// Get returns a copy of a task, enforcing tenant isolation (cross-tenant reads
// report not-found).
func (s *MemoryAgentTaskStore) Get(ctx context.Context, id string) (*AgentTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.items[id]
	if !ok || t.TenantID != tenant.IDFrom(ctx) || t.UserID != EIAMUID(ctx) {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	return s.clone(t), nil
}

// List returns the calling user's tasks, newest first.
func (s *MemoryAgentTaskStore) List(ctx context.Context) ([]*AgentTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tid := tenant.IDFrom(ctx)
	uid := EIAMUID(ctx)
	out := make([]*AgentTask, 0, len(s.items))
	for _, t := range s.items {
		if t.TenantID != tid || t.UserID != uid {
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
	if !ok || cur.TenantID != tenant.IDFrom(ctx) || cur.UserID != EIAMUID(ctx) {
		return fmt.Errorf("task not found: %s", t.ID)
	}

	t.TenantID = cur.TenantID
	t.UserID = cur.UserID
	t.UpdatedAt = time.Now()
	s.items[t.ID] = t
	return s.persistLocked()
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
	uid := EIAMUID(ctx)
	for _, t := range s.items {
		if t.AgentID == agentID && t.ClientTaskID == clientTaskID && t.TenantID == tid && t.UserID == uid {
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
