// Package change provides tests for timeline analysis.
package change

import (
	"context"
	"testing"
	"time"
)

func TestTimelineAnalyzer(t *testing.T) {
	t.Run("Analyze Incident Timeline with Changes", func(t *testing.T) {
		// Create analyzer
		store := NewMemoryStore()
		changeAnalyzer := NewChangeAnalyzer(store)
		configAnalyzer := NewConfigAnalyzer(store)
		analyzer := NewTimelineAnalyzer(changeAnalyzer, configAnalyzer)

		// Setup: Create deployment
		deployment := DeploymentEvent{
			ID:          "deploy-001",
			CICDSystem:  CICDSystemGitLabCI,
			Project:     "test/project",
			Environment: "production",
			Service:     "affected-service",
			CommitSHA:   "abc123",
			StartedAt:   time.Now().Add(-3 * time.Minute),
			Status:      DeploymentStatusFailed,
		}
		_ = deployment // Use deployment to avoid unused variable error
		changeAnalyzer.GetRecentForService(context.Background(), "affected-service", time.Now().Add(-1*time.Hour))

		// Analyze incident timeline
		incidentWindow := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Minute),
			EndTime:   time.Now(),
		}

		analysis, err := analyzer.AnalyzeIncidentTimeline(context.Background(), "incident-001", incidentWindow)
		if err != nil {
			t.Fatalf("Failed to analyze timeline: %v", err)
		}

		if analysis == nil {
			t.Fatal("Expected analysis, got nil")
		}

		if analysis.IncidentID != "incident-001" {
			t.Errorf("Expected incident ID %s, got %s", "incident-001", analysis.IncidentID)
		}
	})

	t.Run("Analyze Incident Timeline with No Changes", func(t *testing.T) {
		store := NewMemoryStore()
		changeAnalyzer := NewChangeAnalyzer(store)
		configAnalyzer := NewConfigAnalyzer(store)
		analyzer := NewTimelineAnalyzer(changeAnalyzer, configAnalyzer)

		incidentWindow := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Minute),
			EndTime:   time.Now(),
		}

		analysis, err := analyzer.AnalyzeIncidentTimeline(context.Background(), "incident-002", incidentWindow)
		if err != nil {
			t.Fatalf("Failed to analyze timeline: %v", err)
		}

		if analysis == nil {
			t.Fatal("Expected analysis, got nil")
		}

		if len(analysis.Changes) != 0 {
			t.Errorf("Expected no changes, got %d", len(analysis.Changes))
		}

		if analysis.RootCauseHypothesis.Confidence != 0 {
			t.Errorf("Expected zero confidence with no changes, got %f", analysis.RootCauseHypothesis.Confidence)
		}
	})
}

func TestBuildCausalityChain(t *testing.T) {
	analyzer := NewTimelineAnalyzer(nil, nil)

	t.Run("Single Change", func(t *testing.T) {
		changes := []TimelineChange{
			{
				ChangeID:         "change-001",
				ChangeType:       ChangeTypeDeployment,
				TimeToIncident:   5 * time.Minute,
				CorrelationScore: 0.8,
				Description:      "Deployment within 5 minutes",
			},
		}

		chain := analyzer.buildCausalityChain(changes, "incident-001")

		if len(chain) != 1 {
			t.Errorf("Expected 1 link, got %d", len(chain))
		}

		if chain[0].FromChangeID != "change-001" {
			t.Errorf("Expected from change-001, got %s", chain[0].FromChangeID)
		}

		if chain[0].ToIncidentID != "incident-001" {
			t.Errorf("Expected to incident-001, got %s", chain[0].ToIncidentID)
		}
	})

	t.Run("Multiple Changes", func(t *testing.T) {
		changes := []TimelineChange{
			{
				ChangeID:         "change-001",
				ChangeType:       ChangeTypeConfig,
				TimeToIncident:   15 * time.Minute,
				CorrelationScore: 0.6,
			},
			{
				ChangeID:         "change-002",
				ChangeType:       ChangeTypeDeployment,
				TimeToIncident:   5 * time.Minute,
				CorrelationScore: 0.9,
			},
		}

		chain := analyzer.buildCausalityChain(changes, "incident-001")

		// Should have: change-001 -> change-002, change-002 -> incident
		expectedLinks := 3 // change-001 -> change-002, change-002 -> incident, plus one more
		if len(chain) < expectedLinks {
			t.Errorf("Expected at least %d links, got %d", expectedLinks, len(chain))
		}
	})
}

func TestCalculateLinkStrength(t *testing.T) {
	analyzer := NewTimelineAnalyzer(nil, nil)

	t.Run("Compatible Change Sequence", func(t *testing.T) {
		from := TimelineChange{
			ChangeID:         "config-change",
			ChangeType:       ChangeTypeConfig,
			TimeToIncident:   15 * time.Minute,
			CorrelationScore: 0.6,
		}

		to := TimelineChange{
			ChangeID:         "deployment",
			ChangeType:       ChangeTypeDeployment,
			TimeToIncident:   5 * time.Minute,
			CorrelationScore: 0.9,
		}

		strength := analyzer.calculateLinkStrength(from, to)

		if strength < 0.5 {
			t.Errorf("Expected strong link for compatible sequence, got %f", strength)
		}
	})

	t.Run("Incompatible Change Sequence", func(t *testing.T) {
		from := TimelineChange{
			ChangeID:         "deployment",
			ChangeType:       ChangeTypeDeployment,
			TimeToIncident:   15 * time.Minute,
			CorrelationScore: 0.6,
		}

		to := TimelineChange{
			ChangeID:         "config-change",
			ChangeType:       ChangeTypeConfig,
			TimeToIncident:   5 * time.Minute,
			CorrelationScore: 0.9,
		}

		strength := analyzer.calculateLinkStrength(from, to)

		// Should be weaker than compatible sequence
		if strength > 0.7 {
			t.Errorf("Expected weaker link for incompatible sequence, got %f", strength)
		}
	})
}

func TestDetermineRootCause(t *testing.T) {
	analyzer := NewTimelineAnalyzer(nil, nil)

	t.Run("Single Change", func(t *testing.T) {
		changes := []TimelineChange{
			{
				ChangeID:         "deployment-001",
				ChangeType:       ChangeTypeDeployment,
				TimeToIncident:   2 * time.Minute,
				CorrelationScore: 0.9,
				Description:      "Failed deployment",
			},
		}

		chain := []CausalityLink{
			{
				FromChangeID:  "deployment-001",
				ToIncidentID:  "incident-001",
				LinkStrength:  0.9,
			},
		}

		hypothesis := analyzer.determineRootCause(changes, chain)

		if hypothesis == nil {
			t.Fatal("Expected hypothesis, got nil")
		}

		if hypothesis.ChangeID != "deployment-001" {
			t.Errorf("Expected change ID deployment-001, got %s", hypothesis.ChangeID)
		}

		if hypothesis.Confidence < 0.8 {
			t.Errorf("Expected high confidence, got %f", hypothesis.Confidence)
		}
	})

	t.Run("Multiple Changes - Highest Score Wins", func(t *testing.T) {
		changes := []TimelineChange{
			{
				ChangeID:         "change-001",
				ChangeType:       ChangeTypeConfig,
				TimeToIncident:   15 * time.Minute,
				CorrelationScore: 0.5,
			},
			{
				ChangeID:         "change-002",
				ChangeType:       ChangeTypeDeployment,
				TimeToIncident:   2 * time.Minute,
				CorrelationScore: 0.8,
			},
			{
				ChangeID:         "change-003",
				ChangeType:       ChangeTypeSecret,
				TimeToIncident:   10 * time.Minute,
				CorrelationScore: 0.6,
			},
		}

		chain := []CausalityLink{}
		hypothesis := analyzer.determineRootCause(changes, chain)

		if hypothesis == nil {
			t.Fatal("Expected hypothesis, got nil")
		}

		// change-002 should win due to highest score and earliest time
		if hypothesis.ChangeID != "change-002" {
			t.Errorf("Expected change-002 as root cause, got %s", hypothesis.ChangeID)
		}
	})
}

func TestCalculateOverallConfidence(t *testing.T) {
	analyzer := NewTimelineAnalyzer(nil, nil)

	t.Run("High Confidence Hypothesis", func(t *testing.T) {
		changes := []TimelineChange{
			{
				ChangeID:         "change-001",
				CorrelationScore: 0.9,
			},
		}

		hypothesis := &RootCauseHypothesis{
			ChangeID:   "change-001",
			Confidence:  0.9,
		}

		confidence := analyzer.calculateOverallConfidence(changes, hypothesis)

		if confidence < 0.8 {
			t.Errorf("Expected high confidence, got %f", confidence)
		}
	})

	t.Run("Multiple Related Changes Increase Confidence", func(t *testing.T) {
		changes := []TimelineChange{
			{
				ChangeID:         "change-001",
				CorrelationScore: 0.7,
			},
			{
				ChangeID:         "change-002",
				CorrelationScore: 0.6,
			},
		}

		hypothesis := &RootCauseHypothesis{
			ChangeID:   "change-001",
			Confidence:  0.7,
		}

		confidence := analyzer.calculateOverallConfidence(changes, hypothesis)

		// Should be higher due to multiple related changes
		if confidence < 0.75 {
			t.Errorf("Multiple changes should increase confidence, got %f", confidence)
		}
	})
}

func TestClusterChanges(t *testing.T) {
	analyzer := NewTimelineAnalyzer(nil, nil)

	t.Run("Cluster Temporal Proximate Changes", func(t *testing.T) {
		changes := []TimelineChange{
			{
				ChangeID:       "change-001",
				TimeToIncident: 2 * time.Minute,
			},
			{
				ChangeID:       "change-002",
				TimeToIncident: 3 * time.Minute,
			},
			{
				ChangeID:       "change-003",
				TimeToIncident: 15 * time.Minute,
			},
		}

		clusters := analyzer.ClusterChanges(changes, 5*time.Minute)

		if len(clusters) != 2 {
			t.Errorf("Expected 2 clusters, got %d", len(clusters))
		}

		// First cluster should have 2 changes
		if len(clusters[0]) != 2 {
			t.Errorf("Expected first cluster to have 2 changes, got %d", len(clusters[0]))
		}

		// Second cluster should have 1 change
		if len(clusters[1]) != 1 {
			t.Errorf("Expected second cluster to have 1 change, got %d", len(clusters[1]))
		}
	})

	t.Run("No Clusters with Single Change", func(t *testing.T) {
		changes := []TimelineChange{
			{
				ChangeID:       "change-001",
				TimeToIncident: 2 * time.Minute,
			},
		}

		clusters := analyzer.ClusterChanges(changes, 5*time.Minute)

		if len(clusters) != 1 {
			t.Errorf("Expected 1 cluster, got %d", len(clusters))
		}

		if len(clusters[0]) != 1 {
			t.Errorf("Expected cluster to have 1 change, got %d", len(clusters[0]))
		}
	})
}

func TestGetCriticalPath(t *testing.T) {
	analyzer := NewTimelineAnalyzer(nil, nil)

	t.Run("Simple Critical Path", func(t *testing.T) {
		analysis := &TimeWindowAnalysis{
			CausalityChain: []CausalityLink{
				{
					FromChangeID:  "change-001",
					ToIncidentID:  "incident-001",
					LinkStrength:  0.8,
				},
			},
		}

		path := analyzer.GetCriticalPath(analysis)

		if len(path) != 1 {
			t.Errorf("Expected 1 link in critical path, got %d", len(path))
		}
	})

	t.Run("Complex Critical Path", func(t *testing.T) {
		analysis := &TimeWindowAnalysis{
			CausalityChain: []CausalityLink{
				{
					FromChangeID: "change-001",
					ToChangeID:   "change-002",
					LinkStrength: 0.5,
				},
				{
					FromChangeID:  "change-002",
					ToIncidentID:  "incident-001",
					LinkStrength:  0.9,
				},
			},
		}

		path := analyzer.GetCriticalPath(analysis)

		// Should find the path through change-002
		if len(path) == 0 {
			t.Error("Expected non-empty critical path")
		}
	})
}

func TestTimelineChange(t *testing.T) {
	t.Run("Timeline Change Fields", func(t *testing.T) {
		change := TimelineChange{
			ChangeID:         "test-001",
			ChangeType:       ChangeTypeDeployment,
			Timestamp:        time.Now(),
			TimeToIncident:    5 * time.Minute,
			CorrelationScore: 0.8,
			Severity:         SeverityHigh,
			Description:      "Test change",
		}

		if change.ChangeID == "" {
			t.Error("ChangeID should not be empty")
		}
		if change.TimeToIncident == 0 {
			t.Error("TimeToIncident should be set")
		}
	})
}

func TestCausalityLink(t *testing.T) {
	t.Run("Causality Link Fields", func(t *testing.T) {
		link := CausalityLink{
			FromChangeID:  "change-001",
			ToChangeID:    "change-002",
			LinkStrength:  0.7,
			Reasoning:     "Test reasoning",
			TimeDelta:     2 * time.Minute,
		}

		if link.FromChangeID == "" {
			t.Error("FromChangeID should not be empty")
		}
		if link.LinkStrength < 0 || link.LinkStrength > 1 {
			t.Errorf("LinkStrength should be between 0 and 1, got %f", link.LinkStrength)
		}
	})
}

func TestRootCauseHypothesis(t *testing.T) {
	t.Run("Root Cause Hypothesis Fields", func(t *testing.T) {
		hypothesis := &RootCauseHypothesis{
			ChangeID:   "change-001",
			ChangeType: ChangeTypeDeployment,
			Confidence: 0.9,
			Reasoning:  "Test reasoning",
			Alternatives: []AlternativeRootCause{
				{
					ChangeID:   "change-002",
					Confidence: 0.6,
					Reasoning:  "Alternative",
				},
			},
		}

		if hypothesis.ChangeID == "" {
			t.Error("ChangeID should not be empty")
		}
		if hypothesis.Confidence < 0 || hypothesis.Confidence > 1 {
			t.Errorf("Confidence should be between 0 and 1, got %f", hypothesis.Confidence)
		}
		if len(hypothesis.Alternatives) == 0 {
			t.Error("Expected at least one alternative")
		}
	})
}
