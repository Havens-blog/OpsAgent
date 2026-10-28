package chat

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

	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

// persistedState is the on-disk snapshot shape. Tenant and conversation ids
// are integer map keys, which encoding/json serializes as their decimal string
// forms and reads back unchanged.
type persistedState struct {
	Conversations map[int64]map[string]*Conversation `json:"conversations"`
	Messages      map[int64]map[string][]*Message    `json:"messages"`
}

// PersistentConversationStore is a tenant-scoped ConversationStore that keeps
// an in-memory copy of the state and writes a full snapshot to disk after every
// mutation. Reads never touch disk; the memory state is guarded by a RWMutex.
type PersistentConversationStore struct {
	mu            sync.RWMutex
	path          string
	conversations map[int64]map[string]*Conversation
	messages      map[int64]map[string][]*Message
}

// NewPersistentConversationStore loads the snapshot at path, or starts empty
// when it does not exist. A corrupt or unreadable file returns an error rather
// than silently discarding existing data.
func NewPersistentConversationStore(path string) (*PersistentConversationStore, error) {
	if path == "" {
		return nil, fmt.Errorf("persistent conversation store path is required")
	}

	s := &PersistentConversationStore{
		path:          path,
		conversations: make(map[int64]map[string]*Conversation),
		messages:      make(map[int64]map[string][]*Message),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads and decodes the snapshot. A missing file is the empty store.
func (s *PersistentConversationStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read conversation store %s: %w", s.path, err)
	}

	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse conversation store %s: %w", s.path, err)
	}
	if state.Conversations == nil {
		state.Conversations = make(map[int64]map[string]*Conversation)
	}
	if state.Messages == nil {
		state.Messages = make(map[int64]map[string][]*Message)
	}

	s.conversations = state.Conversations
	s.messages = state.Messages
	return nil
}

// persistLocked writes the current state to a temp file in the same directory
// and atomically renames it over the target. Callers must hold the write lock.
func (s *PersistentConversationStore) persistLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create conversation store dir: %w", err)
	}

	state := persistedState{Conversations: s.conversations, Messages: s.messages}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode conversation store: %w", err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create conversation store temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write conversation store temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close conversation store temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace conversation store: %w", err)
	}
	return nil
}

// Create stores a conversation under the ctx tenant and persists the snapshot.
func (s *PersistentConversationStore) Create(ctx context.Context, conv *Conversation) error {
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
	conv.TenantID = tid
	s.conversations[tid][conv.ID] = conv
	return s.persistLocked()
}

// Get retrieves a conversation within the ctx tenant, or not-found.
func (s *PersistentConversationStore) Get(ctx context.Context, id string) (*Conversation, error) {
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
func (s *PersistentConversationStore) List(ctx context.Context) ([]*Conversation, error) {
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

// Delete removes a conversation and its messages within the ctx tenant and
// persists the snapshot.
func (s *PersistentConversationStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tid := tenant.IDFrom(ctx)
	if _, ok := s.conversations[tid][id]; !ok {
		return fmt.Errorf("conversation not found: %s", id)
	}
	delete(s.conversations[tid], id)
	delete(s.messages[tid], id)
	return s.persistLocked()
}

// UpdateTitle renames a conversation within the ctx tenant and persists the
// snapshot, or reports not-found when it does not exist in this tenant.
func (s *PersistentConversationStore) UpdateTitle(ctx context.Context, id string, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tid := tenant.IDFrom(ctx)
	conv, ok := s.conversations[tid][id]
	if !ok {
		return fmt.Errorf("conversation not found: %s", id)
	}
	conv.Title = title
	conv.UpdatedAt = time.Now()
	return s.persistLocked()
}

// AddMessage appends a message to a conversation, touches its UpdatedAt, and
// persists the snapshot.
func (s *PersistentConversationStore) AddMessage(ctx context.Context, convID string, msg *Message) error {
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
	return s.persistLocked()
}

// ListMessages returns a conversation's messages in insertion order, or
// not-found when the conversation does not exist in the ctx tenant.
func (s *PersistentConversationStore) ListMessages(ctx context.Context, convID string) ([]*Message, error) {
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
