// Package change provides in-memory storage for change events.
package change

import (
	"context"
	"fmt"
	"sync"
)

// MemoryStore provides an in-memory implementation of ChangeStore.
// Useful for testing and development.
type MemoryStore struct {
	mu           sync.RWMutex
	deployments  map[string]DeploymentEvent
	correlations map[string][]ChangeCorrelation // incidentID -> correlations
}

// NewMemoryStore creates a new in-memory change store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		deployments:  make(map[string]DeploymentEvent),
		correlations: make(map[string][]ChangeCorrelation),
	}
}

// StoreDeployment stores a deployment event.
func (s *MemoryStore) StoreDeployment(ctx context.Context, deployment DeploymentEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if deployment.ID == "" {
		deployment.ID = generateDeploymentID(deployment)
	}

	s.deployments[deployment.ID] = deployment
	return nil
}

// GetDeployment retrieves a deployment event by ID.
func (s *MemoryStore) GetDeployment(ctx context.Context, id string) (DeploymentEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deployment, ok := s.deployments[id]
	if !ok {
		return DeploymentEvent{}, fmt.Errorf("deployment not found: %s", id)
	}

	return deployment, nil
}

// ListDeployments lists deployment events matching filters.
func (s *MemoryStore) ListDeployments(ctx context.Context, filter DeploymentFilter) ([]DeploymentEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []DeploymentEvent

	for _, deployment := range s.deployments {
		if s.matchesFilter(deployment, filter) {
			results = append(results, deployment)
		}
	}

	// Apply offset
	if filter.Offset > 0 && filter.Offset < len(results) {
		results = results[filter.Offset:]
	}

	// Apply limit
	if filter.Limit > 0 && filter.Limit < len(results) {
		results = results[:filter.Limit]
	}

	return results, nil
}

// StoreCorrelation stores a change-incident correlation.
func (s *MemoryStore) StoreCorrelation(ctx context.Context, correlation ChangeCorrelation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.correlations[correlation.IncidentID] = append(
		s.correlations[correlation.IncidentID],
		correlation,
	)

	return nil
}

// GetCorrelations retrieves correlations for an incident.
func (s *MemoryStore) GetCorrelations(ctx context.Context, incidentID string) ([]ChangeCorrelation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	correlations, ok := s.correlations[incidentID]
	if !ok {
		return []ChangeCorrelation{}, nil
	}

	// Return a copy to prevent concurrent modification
	result := make([]ChangeCorrelation, len(correlations))
	copy(result, correlations)

	return result, nil
}

// matchesFilter checks if a deployment matches the given filter.
func (s *MemoryStore) matchesFilter(deployment DeploymentEvent, filter DeploymentFilter) bool {
	// Project filter
	if filter.Project != "" && deployment.Project != filter.Project {
		return false
	}

	// Repository filter
	if filter.Repository != "" && deployment.Repository != filter.Repository {
		return false
	}

	// Service filter
	if filter.Service != "" && deployment.Service != filter.Service {
		return false
	}

	// Environment filter
	if filter.Environment != "" && deployment.Environment != filter.Environment {
		return false
	}

	// Status filter
	if filter.Status != "" && deployment.Status != filter.Status {
		return false
	}

	// Time window filter
	if filter.StartTime != nil && deployment.StartedAt.Before(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && deployment.StartedAt.After(*filter.EndTime) {
		return false
	}

	return true
}

// generateDeploymentID generates a unique deployment ID.
func generateDeploymentID(deployment DeploymentEvent) string {
	timestamp := deployment.StartedAt.Format("20060102150405")
	return fmt.Sprintf("deploy-%s-%s", timestamp, deployment.CommitSHA[:8])
}

// Clear clears all stored data. Useful for testing.
func (s *MemoryStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.deployments = make(map[string]DeploymentEvent)
	s.correlations = make(map[string][]ChangeCorrelation)
}

// Stats returns statistics about the store.
func (s *MemoryStore) Stats() StoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := StoreStats{
		TotalDeployments: len(s.deployments),
		TotalIncidents:   len(s.correlations),
	}

	for _, correlations := range s.correlations {
		stats.TotalCorrelations += len(correlations)
	}

	return stats
}

// StoreStats represents statistics about the change store.
type StoreStats struct {
	TotalDeployments  int `json:"total_deployments"`
	TotalIncidents    int `json:"total_incidents"`
	TotalCorrelations int `json:"total_correlations"`
}
