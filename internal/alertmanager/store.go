// Package alertmanager provides in-memory incident storage.
package alertmanager

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryIncidentStore provides an in-memory implementation of IncidentStore.
type MemoryIncidentStore struct {
	mu        sync.RWMutex
	incidents map[string]Incident
}

// NewMemoryIncidentStore creates a new in-memory incident store.
func NewMemoryIncidentStore() *MemoryIncidentStore {
	return &MemoryIncidentStore{
		incidents: make(map[string]Incident),
	}
}

// Store stores an incident.
func (s *MemoryIncidentStore) Store(ctx context.Context, incident Incident) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	incident.CreatedAt = time.Now()
	incident.UpdatedAt = time.Now()

	s.incidents[incident.ID] = incident
	return nil
}

// Get retrieves an incident by ID.
func (s *MemoryIncidentStore) Get(ctx context.Context, id string) (Incident, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	incident, exists := s.incidents[id]
	if !exists {
		return Incident{}, fmt.Errorf("incident not found: %s", id)
	}

	return incident, nil
}

// Update updates an existing incident.
func (s *MemoryIncidentStore) Update(ctx context.Context, incident Incident) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.incidents[incident.ID]; !exists {
		return fmt.Errorf("incident not found: %s", incident.ID)
	}

	incident.UpdatedAt = time.Now()
	s.incidents[incident.ID] = incident
	return nil
}

// List lists incidents based on filters.
func (s *MemoryIncidentStore) List(ctx context.Context, filter IncidentFilter) ([]Incident, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []Incident

	for _, incident := range s.incidents {
		if s.matchesFilter(incident, filter) {
			results = append(results, incident)
		}
	}

	return results, nil
}

// matchesFilter checks if an incident matches the given filters.
func (s *MemoryIncidentStore) matchesFilter(incident Incident, filter IncidentFilter) bool {
	// Filter by severity
	if filter.Severity != "" && incident.Severity != filter.Severity {
		return false
	}

	// Filter by status
	if filter.Status != "" && incident.Status != filter.Status {
		return false
	}

	// Filter by service
	if filter.Service != "" {
		found := false
		for _, service := range incident.AffectedServices {
			if service == filter.Service {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filter by time range
	if !filter.StartTime.IsZero() && incident.StartTime.Before(filter.StartTime) {
		return false
	}
	if !filter.EndTime.IsZero() && incident.EndTime.After(filter.EndTime) {
		return false
	}

	return true
}

// Cleanup removes resolved incidents older than the specified duration.
func (s *MemoryIncidentStore) Cleanup(ctx context.Context, olderThan time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	for id, incident := range s.incidents {
		if incident.Status == "resolved" && incident.EndTime.Before(cutoff) {
			delete(s.incidents, id)
		}
	}

	return nil
}
