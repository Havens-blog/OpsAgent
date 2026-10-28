// Package change provides tests for CI/CD integration.
package change

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStore(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	t.Run("Store and Get Deployment", func(t *testing.T) {
		deployment := DeploymentEvent{
			ID:          "test-deploy-001",
			CICDSystem:  CICDSystemGitLabCI,
			Project:     "test/project",
			Environment: "production",
			Service:     "test-service",
			CommitSHA:   "abc123",
			StartedAt:   time.Now(),
			Status:      DeploymentStatusSuccess,
		}

		err := store.StoreDeployment(ctx, deployment)
		if err != nil {
			t.Fatalf("Failed to store deployment: %v", err)
		}

		retrieved, err := store.GetDeployment(ctx, "test-deploy-001")
		if err != nil {
			t.Fatalf("Failed to get deployment: %v", err)
		}

		if retrieved.ID != deployment.ID {
			t.Errorf("Expected ID %s, got %s", deployment.ID, retrieved.ID)
		}

		if retrieved.Service != deployment.Service {
			t.Errorf("Expected service %s, got %s", deployment.Service, retrieved.Service)
		}
	})

	t.Run("List Deployments with Filters", func(t *testing.T) {
		// Store multiple deployments
		deployments := []DeploymentEvent{
			{
				ID:          "deploy-001",
				Project:     "project-a",
				Service:     "service-x",
				Environment: "production",
				StartedAt:   time.Now().Add(-1 * time.Hour),
				Status:      DeploymentStatusSuccess,
			},
			{
				ID:          "deploy-002",
				Project:     "project-a",
				Service:     "service-y",
				Environment: "production",
				StartedAt:   time.Now().Add(-30 * time.Minute),
				Status:      DeploymentStatusFailed,
			},
			{
				ID:          "deploy-003",
				Project:     "project-b",
				Service:     "service-x",
				Environment: "staging",
				StartedAt:   time.Now().Add(-15 * time.Minute),
				Status:      DeploymentStatusSuccess,
			},
		}

		for _, d := range deployments {
			if err := store.StoreDeployment(ctx, d); err != nil {
				t.Fatalf("Failed to store deployment: %v", err)
			}
		}

		// Filter by project
		filter := DeploymentFilter{
			Project: "project-a",
		}

		results, err := store.ListDeployments(ctx, filter)
		if err != nil {
			t.Fatalf("Failed to list deployments: %v", err)
		}

		if len(results) != 2 {
			t.Errorf("Expected 2 deployments for project-a, got %d", len(results))
		}

		// Filter by status
		filter = DeploymentFilter{
			Status: DeploymentStatusFailed,
		}

		results, err = store.ListDeployments(ctx, filter)
		if err != nil {
			t.Fatalf("Failed to list deployments: %v", err)
		}

		if len(results) != 1 {
			t.Errorf("Expected 1 failed deployment, got %d", len(results))
		}
	})

	t.Run("Store and Get Correlations", func(t *testing.T) {
		correlation := ChangeCorrelation{
			ChangeID:         "deploy-001",
			ChangeType:       ChangeTypeDeployment,
			IncidentID:       "incident-001",
			CorrelationScore: 0.8,
			Reason:           "Deployment within 5 minutes",
			TimeDelta:        3 * time.Minute,
		}

		err := store.StoreCorrelation(ctx, correlation)
		if err != nil {
			t.Fatalf("Failed to store correlation: %v", err)
		}

		correlations, err := store.GetCorrelations(ctx, "incident-001")
		if err != nil {
			t.Fatalf("Failed to get correlations: %v", err)
		}

		if len(correlations) != 1 {
			t.Errorf("Expected 1 correlation, got %d", len(correlations))
		}

		if correlations[0].ChangeID != correlation.ChangeID {
			t.Errorf("Expected change ID %s, got %s", correlation.ChangeID, correlations[0].ChangeID)
		}
	})

	t.Run("Stats", func(t *testing.T) {
		stats := store.Stats()

		if stats.TotalDeployments != 4 { // 1 from first test + 3 from second test
			t.Errorf("Expected 4 total deployments, got %d", stats.TotalDeployments)
		}

		if stats.TotalIncidents != 1 {
			t.Errorf("Expected 1 total incident, got %d", stats.TotalIncidents)
		}

		if stats.TotalCorrelations != 1 {
			t.Errorf("Expected 1 total correlation, got %d", stats.TotalCorrelations)
		}
	})
}

func TestChangeAnalyzer(t *testing.T) {
	t.Run("Correlate Deployment with Incident", func(t *testing.T) {
		store := NewMemoryStore()
		analyzer := NewChangeAnalyzer(store)

		// Create a deployment 2 minutes before incident
		deployment := DeploymentEvent{
			ID:          "deploy-before-incident",
			CICDSystem:  CICDSystemGitLabCI,
			Project:     "test/project",
			Environment: "production",
			Service:     "affected-service",
			CommitSHA:   "abc123",
			StartedAt:   time.Now().Add(-2 * time.Minute),
			Status:      DeploymentStatusSuccess,
		}

		if err := store.StoreDeployment(context.Background(), deployment); err != nil {
			t.Fatalf("Failed to store deployment: %v", err)
		}

		// Define incident window
		incidentWindow := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Minute),
			EndTime:   time.Now(),
		}

		// Correlate
		correlations, err := analyzer.CorrelateWithIncident(
			context.Background(),
			"incident-001",
			incidentWindow,
		)

		if err != nil {
			t.Fatalf("Failed to correlate: %v", err)
		}

		if len(correlations) != 1 {
			t.Errorf("Expected 1 correlation, got %d", len(correlations))
		}

		// Check correlation score
		if correlations[0].CorrelationScore < 0.5 {
			t.Errorf("Expected correlation score >= 0.5, got %f", correlations[0].CorrelationScore)
		}

		if correlations[0].ChangeID != deployment.ID {
			t.Errorf("Expected change ID %s, got %s", deployment.ID, correlations[0].ChangeID)
		}
	})

	t.Run("Failed Deployment Higher Correlation", func(t *testing.T) {
		store := NewMemoryStore()
		analyzer := NewChangeAnalyzer(store)

		// Failed deployment
		deployment := DeploymentEvent{
			ID:          "failed-deploy",
			CICDSystem:  CICDSystemGitLabCI,
			Project:     "test/project",
			Environment: "production",
			Service:     "affected-service",
			CommitSHA:   "badcommit",
			StartedAt:   time.Now().Add(-2 * time.Minute),
			Status:      DeploymentStatusFailed,
		}

		if err := store.StoreDeployment(context.Background(), deployment); err != nil {
			t.Fatalf("Failed to store deployment: %v", err)
		}

		incidentWindow := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Minute),
			EndTime:   time.Now(),
		}

		correlations, err := analyzer.CorrelateWithIncident(
			context.Background(),
			"incident-002",
			incidentWindow,
		)

		if err != nil {
			t.Fatalf("Failed to correlate: %v", err)
		}

		if len(correlations) != 1 {
			t.Fatalf("Expected 1 correlation, got %d", len(correlations))
		}

		// Failed deployment should have higher correlation score
		if correlations[0].CorrelationScore < 0.7 {
			t.Errorf("Failed deployment should have correlation score >= 0.7, got %f", correlations[0].CorrelationScore)
		}
	})

	t.Run("Rolled Back Deployment Highest Correlation", func(t *testing.T) {
		store := NewMemoryStore()
		analyzer := NewChangeAnalyzer(store)

		// Rolled back deployment
		deployment := DeploymentEvent{
			ID:          "rolled-back-deploy",
			CICDSystem:  CICDSystemGitLabCI,
			Project:     "test/project",
			Environment: "production",
			Service:     "affected-service",
			CommitSHA:   "badcommit",
			StartedAt:   time.Now().Add(-2 * time.Minute),
			Status:      DeploymentStatusRolledBack,
		}

		if err := store.StoreDeployment(context.Background(), deployment); err != nil {
			t.Fatalf("Failed to store deployment: %v", err)
		}

		incidentWindow := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Minute),
			EndTime:   time.Now(),
		}

		correlations, err := analyzer.CorrelateWithIncident(
			context.Background(),
			"incident-003",
			incidentWindow,
		)

		if err != nil {
			t.Fatalf("Failed to correlate: %v", err)
		}

		if len(correlations) != 1 {
			t.Fatalf("Expected 1 correlation, got %d", len(correlations))
		}

		// Rolled back deployment should have highest correlation score
		if correlations[0].CorrelationScore < 0.8 {
			t.Errorf("Rolled back deployment should have correlation score >= 0.8, got %f", correlations[0].CorrelationScore)
		}
	})

	t.Run("Deployment Too Long Ago No Correlation", func(t *testing.T) {
		store := NewMemoryStore()
		analyzer := NewChangeAnalyzer(store)

		// Deployment 1 hour ago
		deployment := DeploymentEvent{
			ID:          "old-deploy",
			CICDSystem:  CICDSystemGitLabCI,
			Project:     "test/project",
			Environment: "production",
			Service:     "affected-service",
			CommitSHA:   "oldcommit",
			StartedAt:   time.Now().Add(-1 * time.Hour),
			Status:      DeploymentStatusSuccess,
		}

		if err := store.StoreDeployment(context.Background(), deployment); err != nil {
			t.Fatalf("Failed to store deployment: %v", err)
		}

		incidentWindow := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Minute),
			EndTime:   time.Now(),
		}

		correlations, err := analyzer.CorrelateWithIncident(
			context.Background(),
			"incident-004",
			incidentWindow,
		)

		if err != nil {
			t.Fatalf("Failed to correlate: %v", err)
		}

		// Old deployment should not correlate
		if len(correlations) != 0 {
			t.Errorf("Expected no correlation for deployment 1 hour ago, got %d", len(correlations))
		}
	})
}
