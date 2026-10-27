// Package chat provides the conversational assistant service for OpsAgent.
package chat

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

// Conversation is a persisted chat conversation scoped to a tenant.
type Conversation struct {
	ID        string    `json:"id"`
	TenantID  int64     `json:"tenant_id"`
	UserID    int64     `json:"user_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ConversationStore persists conversations and their messages, tenant-scoped.
type ConversationStore interface {
	Create(ctx context.Context, conv *Conversation) error
	Get(ctx context.Context, id string) (*Conversation, error)
	List(ctx context.Context) ([]*Conversation, error)
	Delete(ctx context.Context, id string) error
	AddMessage(ctx context.Context, convID string, msg *Message) error
	ListMessages(ctx context.Context, convID string) ([]*Message, error)
}

// MemoryConversationStore is an in-memory, tenant-scoped ConversationStore.
// Data is bucketed by tenant id (see internal/tenant); cross-tenant reads and
// deletes report not-found so existence never leaks across tenants.
type MemoryConversationStore struct {
	mu            sync.RWMutex
	conversations map[int64]map[string]*Conversation
	messages      map[int64]map[string][]*Message
}

// NewMemoryConversationStore creates an empty in-memory conversation store.
func NewMemoryConversationStore() *MemoryConversationStore {
	return &MemoryConversationStore{
		conversations: make(map[int64]map[string]*Conversation),
		messages:      make(map[int64]map[string][]*Message),
	}
}

// Create stores a conversation under the ctx tenant.
func (s *MemoryConversationStore) Create(ctx context.Context, conv *Conversation) error {
	if conv == nil || conv.ID == "" {
		return fmt.Errorf("conversation id is required")
	}

	now := time.Now()
	if conv.CreatedAt.IsZero() {
		conv.CreatedAt = now
	}
	if conv.UpdatedAt.IsZero() {
		conv.UpdatedAt = conv.CreatedAt
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tid := tenant.IDFrom(ctx)
	if s.conversations[tid] == nil {
		s.conversations[tid] = make(map[string]*Conversation)
	}
	// Tenant is authoritative from ctx, never from the caller-supplied struct.
	conv.TenantID = tid
	s.conversations[tid][conv.ID] = conv
	return nil
}

// Get retrieves a conversation within the ctx tenant, or not-found.
func (s *MemoryConversationStore) Get(ctx context.Context, id string) (*Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tid := tenant.IDFrom(ctx)
	conv, ok := s.conversations[tid][id]
	if !ok {
		return nil, fmt.Errorf("conversation not found: %s", id)
	}
	return conv, nil
}

// List returns the ctx tenant's conversations ordered by UpdatedAt descending.
func (s *MemoryConversationStore) List(ctx context.Context) ([]*Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tid := tenant.IDFrom(ctx)
	bucket := s.conversations[tid]
	out := make([]*Conversation, 0, len(bucket))
	for _, conv := range bucket {
		out = append(out, conv)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

// Delete removes a conversation and its messages within the ctx tenant.
func (s *MemoryConversationStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tid := tenant.IDFrom(ctx)
	if _, ok := s.conversations[tid][id]; !ok {
		return fmt.Errorf("conversation not found: %s", id)
	}
	delete(s.conversations[tid], id)
	delete(s.messages[tid], id)
	return nil
}

// AddMessage appends a message to a conversation and touches its UpdatedAt.
func (s *MemoryConversationStore) AddMessage(ctx context.Context, convID string, msg *Message) error {
	if msg == nil || msg.ID == "" {
		return fmt.Errorf("message id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tid := tenant.IDFrom(ctx)
	conv, ok := s.conversations[tid][convID]
	if !ok {
		return fmt.Errorf("conversation not found: %s", convID)
	}

	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	msg.ConversationID = convID

	if s.messages[tid] == nil {
		s.messages[tid] = make(map[string][]*Message)
	}
	s.messages[tid][convID] = append(s.messages[tid][convID], msg)
	conv.UpdatedAt = msg.CreatedAt
	return nil
}

// ListMessages returns a conversation's messages in insertion order, or
// not-found when the conversation does not exist in the ctx tenant.
func (s *MemoryConversationStore) ListMessages(ctx context.Context, convID string) ([]*Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tid := tenant.IDFrom(ctx)
	if _, ok := s.conversations[tid][convID]; !ok {
		return nil, fmt.Errorf("conversation not found: %s", convID)
	}

	src := s.messages[tid][convID]
	out := make([]*Message, len(src))
	copy(out, src)
	return out, nil
}
