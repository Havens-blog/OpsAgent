// Package memory stores long-term operational fault memory. Only stable,
// service-scoped facts pass the scope gate; conversation-local or one-off facts
// are rejected so transient context never pollutes the durable store.
package memory

import (
	"fmt"
	"strings"
	"sync"
)

// FactScope describes the lifetime/authority of a fact.
type FactScope string

const (
	ScopeService FactScope = "service" // stable, descriptive service-level fact
	ScopeUser    FactScope = "user"    // user-level preference
	ScopeSession FactScope = "session" // conversation-local, not durable
)

// FactKind distinguishes the two fault-memory shapes OpsAgent records.
type FactKind string

const (
	FactRootCause FactKind = "root_cause"
	FactFragility FactKind = "fragility"
)

// Fact is a candidate durable fact about a service.
type Fact struct {
	Kind       FactKind
	Scope      FactScope
	Service    string
	Content    string
	Confidence float64
}

// key deduplicates identical facts for the same service.
func (f Fact) key() string {
	return string(f.Kind) + "\x00" + f.Service + "\x00" + strings.TrimSpace(f.Content)
}

// scopeGate returns true only for stable, descriptive service-level facts with
// enough confidence. Everything else stays out of long-term memory.
func scopeGate(f Fact, minConfidence float64) bool {
	if f.Scope != ScopeService {
		return false
	}
	if strings.TrimSpace(f.Service) == "" || strings.TrimSpace(f.Content) == "" {
		return false
	}
	if f.Confidence < minConfidence {
		return false
	}
	return true
}

// Store is a thread-safe, deduplicated fault memory.
type Store struct {
	mu            sync.RWMutex
	facts         map[string]Fact
	minConfidence float64
}

// New creates an empty store requiring at least minConfidence to persist a fact.
func New(minConfidence float64) *Store {
	return &Store{facts: make(map[string]Fact), minConfidence: minConfidence}
}

// Record persists a fact if it passes the scope gate. It returns whether the
// fact was newly stored; duplicates and rejected facts are not stored.
func (s *Store) Record(f Fact) (bool, error) {
	if !scopeGate(f, s.minConfidence) {
		return false, fmt.Errorf("fact rejected by scope gate")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.facts[f.key()]; exists {
		return false, nil
	}
	s.facts[f.key()] = f
	return true, nil
}

// ForService returns all stored facts for one service.
func (s *Store) ForService(service string) []Fact {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Fact, 0)
	for _, f := range s.facts {
		if f.Service == service {
			out = append(out, f)
		}
	}
	return out
}

// Count returns the number of stored facts.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.facts)
}
