// Package topology provides tests for topology path scoring.
package topology

import (
	"context"
	"testing"
	"time"
)

func TestPathScorer(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)
	scorer := NewPathScorer(client, analyzer)

	t.Run("Score Hypotheses", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		// api-gateway has downstream edges to order-service and payment-service,
		// so paths exist; order-service has no edge to payment-service (nil path).
		hypotheses := []RootCauseHypothesis{
			{
				ChangeID:   "api-gateway",
				ChangeType: "deployment",
				Confidence: 0.8,
			},
			{
				ChangeID:   "order-service",
				ChangeType: "config",
				Confidence: 0.6,
			},
		}

		affectedServices := []string{"order-service", "payment-service"}

		scores, err := scorer.ScoreHypotheses(context.Background(), hypotheses, affectedServices, window)
		if err != nil {
			t.Fatalf("Failed to score hypotheses: %v", err)
		}

		if len(scores) == 0 {
			t.Error("Expected at least one score")
		}
	})

	t.Run("Score Topology Paths", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		affectedServices := []string{"order-service", "postgres-order"}

		scores, err := scorer.ScoreTopologyPaths(context.Background(), affectedServices, window)
		if err != nil {
			t.Fatalf("Failed to score topology paths: %v", err)
		}

		if scores == nil {
			t.Fatal("Expected scores, got nil")
		}

		if len(scores.AffectedServices) != 2 {
			t.Errorf("Expected 2 affected services, got %d", len(scores.AffectedServices))
		}
	})

	t.Run("Get Critical Paths", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		paths, err := scorer.GetCriticalPaths(context.Background(), window, 5)
		if err != nil {
			t.Fatalf("Failed to get critical paths: %v", err)
		}

		if len(paths) == 0 {
			t.Error("Expected at least one critical path")
		}

		// Check that paths are sorted by score
		for i := 1; i < len(paths); i++ {
			if paths[i-1].Score < paths[i].Score {
				t.Error("Paths should be sorted by score in descending order")
			}
		}
	})

	t.Run("Compare Paths", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		path1 := []string{"api-gateway", "order-service"}
		path2 := []string{"api-gateway", "payment-service"}

		comparison, err := scorer.ComparePaths(context.Background(), path1, path2, window)
		if err != nil {
			t.Fatalf("Failed to compare paths: %v", err)
		}

		if comparison == nil {
			t.Fatal("Expected comparison, got nil")
		}

		if len(comparison.Path1) != 2 || len(comparison.Path2) != 2 {
			t.Error("Both paths should have 2 nodes")
		}

		if comparison.Recommendation == "" {
			t.Error("Expected recommendation to be set")
		}
	})

	t.Run("Rank Root Causes", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		potentialCauses := []string{"api-gateway", "order-service", "postgres-order"}
		affectedServices := []string{"order-service", "postgres-order"}

		ranks, err := scorer.RankRootCauses(context.Background(), potentialCauses, affectedServices, window)
		if err != nil {
			t.Fatalf("Failed to rank root causes: %v", err)
		}

		if len(ranks) == 0 {
			t.Error("Expected at least one rank")
		}

		// Check that ranks are sequential starting from 1
		for i, rank := range ranks {
			if rank.Rank != i+1 {
				t.Errorf("Expected rank %d, got %d", i+1, rank.Rank)
			}
		}

		// Check that higher scores come first
		for i := 1; i < len(ranks); i++ {
			if ranks[i-1].Score < ranks[i].Score {
				t.Error("Ranks should be sorted by score in descending order")
			}
		}
	})
}

func TestCalculatePathScore(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)
	scorer := NewPathScorer(client, analyzer)

	t.Run("Empty Path", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		graph, err := client.GetServiceGraph(context.Background(), window)
		if err != nil {
			t.Fatalf("Failed to get graph: %v", err)
		}

		score, metrics := scorer.calculatePathScore(graph, []string{}, []string{})

		if score != 0 {
			t.Errorf("Expected zero score for empty path, got %f", score)
		}

		if metrics.PathLength != 0 {
			t.Errorf("Expected path length 0 for empty path, got %d", metrics.PathLength)
		}
	})

	t.Run("Single Node Path", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		graph, err := client.GetServiceGraph(context.Background(), window)
		if err != nil {
			t.Fatalf("Failed to get graph: %v", err)
		}

		path := []string{"order-service"}
		score, metrics := scorer.calculatePathScore(graph, path, []string{})

		if score < 0 || score > 1 {
			t.Errorf("Expected score between 0 and 1, got %f", score)
		}

		if metrics.PathLength != 1 {
			t.Errorf("Expected path length 1, got %d", metrics.PathLength)
		}
	})

	t.Run("Multi Node Path", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		graph, err := client.GetServiceGraph(context.Background(), window)
		if err != nil {
			t.Fatalf("Failed to get graph: %v", err)
		}

		path := []string{"api-gateway", "order-service", "postgres-order"}
		score, metrics := scorer.calculatePathScore(graph, path, []string{"order-service"})

		if score < 0 || score > 1 {
			t.Errorf("Expected score between 0 and 1, got %f", score)
		}

		if metrics.PathLength != 3 {
			t.Errorf("Expected path length 3, got %d", metrics.PathLength)
		}
	})
}

func TestCalculateOverallScore(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)
	scorer := NewPathScorer(client, analyzer)

	t.Run("High Error Rate Metrics", func(t *testing.T) {
		metrics := PathMetrics{
			TotalErrorRate: 0.1,
			AvgLatency:     150.0,
			PathLength:     3,
			FailedEdges:    2,
			HealthScore:    0.3,
			TrafficImpact:  0.8,
		}

		score := scorer.calculateOverallScore(metrics)

		// Path length penalty (÷3) keeps score moderate for multi-hop paths
		if score < 0.35 {
			t.Errorf("Expected elevated score for high error rate, got %f", score)
		}
	})

	t.Run("Low Error Rate Metrics", func(t *testing.T) {
		metrics := PathMetrics{
			TotalErrorRate: 0.001,
			AvgLatency:     25.0,
			PathLength:     2,
			FailedEdges:    0,
			HealthScore:    1.0,
			TrafficImpact:  0.2,
		}

		score := scorer.calculateOverallScore(metrics)

		if score > 0.3 {
			t.Errorf("Expected low score for low error rate, got %f", score)
		}
	})
}

func TestCalculateConfidence(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)
	scorer := NewPathScorer(client, analyzer)

	t.Run("High Confidence Metrics", func(t *testing.T) {
		metrics := PathMetrics{
			TotalErrorRate: 0.15,
			FailedEdges:    3,
			HealthScore:    0.2,
		}

		confidence := scorer.calculateConfidence(metrics)

		if confidence < 0.8 {
			t.Errorf("Expected high confidence, got %f", confidence)
		}
	})

	t.Run("Low Confidence Metrics", func(t *testing.T) {
		metrics := PathMetrics{
			TotalErrorRate: 0.01,
			FailedEdges:    0,
			HealthScore:    0.9,
		}

		confidence := scorer.calculateConfidence(metrics)

		if confidence > 0.7 {
			t.Errorf("Expected lower confidence, got %f", confidence)
		}
	})
}

func TestGenerateReasoning(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)
	scorer := NewPathScorer(client, analyzer)

	t.Run("Generate Reasoning", func(t *testing.T) {
		metrics := PathMetrics{
			TotalErrorRate: 0.08,
			PathLength:    3,
			FailedEdges:   1,
			HealthScore:   0.4,
		}

		path := []string{"api-gateway", "order-service", "postgres-order"}
		reasoning := scorer.generateReasoning(metrics, path)

		if reasoning == "" {
			t.Error("Expected reasoning to be generated")
		}

		// Check that reasoning contains expected elements
		if !containsSubstring(reasoning, "Path of") {
			t.Error("Reasoning should mention path length")
		}
	})
}

func TestCalculateTrafficImpact(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)
	scorer := NewPathScorer(client, analyzer)

	t.Run("Calculate Traffic Impact", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		graph, err := client.GetServiceGraph(context.Background(), window)
		if err != nil {
			t.Fatalf("Failed to get graph: %v", err)
		}

		path := []string{"api-gateway", "order-service"}
		impact := scorer.calculateTrafficImpact(graph, path)

		if impact < 0 || impact > 1 {
			t.Errorf("Expected impact between 0 and 1, got %f", impact)
		}
	})
}

func TestTopologyScores(t *testing.T) {
	t.Run("Topology Scores Structure", func(t *testing.T) {
		scores := &TopologyScores{
			AffectedServices: []string{"service-1", "service-2"},
			AnomalousNodes:   []string{"service-1"},
			PathScores: []PathScore{
				{Score: 0.8},
				{Score: 0.6},
			},
		}

		scores.calculateStatistics()

		if scores.BestPath == nil {
			t.Error("Expected best path to be set")
		}

		if scores.BestPath.Score != 0.8 {
			t.Errorf("Expected best score 0.8, got %f", scores.BestPath.Score)
		}

		if scores.AverageScore != 0.7 {
			t.Errorf("Expected average score 0.7, got %f", scores.AverageScore)
		}
	})
}

func TestPathScore(t *testing.T) {
	t.Run("Path Score Fields", func(t *testing.T) {
		score := PathScore{
			Path:       []string{"node-1", "node-2"},
			Score:      0.8,
			Confidence: 0.7,
			Reasoning:  "Test reasoning",
			Metrics: PathMetrics{
				TotalErrorRate: 0.05,
				PathLength:     2,
			},
		}

		if len(score.Path) != 2 {
			t.Errorf("Expected 2 nodes in path, got %d", len(score.Path))
		}

		if score.Score < 0 || score.Score > 1 {
			t.Errorf("Expected score between 0 and 1, got %f", score.Score)
		}
	})
}

func TestPathMetrics(t *testing.T) {
	t.Run("Path Metrics Fields", func(t *testing.T) {
		metrics := PathMetrics{
			TotalErrorRate: 0.025,
			AvgLatency:     100.0,
			PathLength:     3,
			FailedEdges:    1,
			HealthScore:    0.6,
			TrafficImpact:  0.5,
		}

		if metrics.PathLength != 3 {
			t.Errorf("Expected path length 3, got %d", metrics.PathLength)
		}

		if metrics.TotalErrorRate < 0 || metrics.TotalErrorRate > 1 {
			t.Errorf("Expected error rate between 0 and 1, got %f", metrics.TotalErrorRate)
		}
	})
}

func TestPathComparison(t *testing.T) {
	t.Run("Path Comparison Fields", func(t *testing.T) {
		comparison := &PathComparison{
			Path1:          []string{"node-1", "node-2"},
			Path2:          []string{"node-1", "node-3"},
			Score1:         0.8,
			Score2:         0.6,
			BetterPath:     []string{"node-1", "node-2"},
			Recommendation: "Path 1 is better",
		}

		if len(comparison.Path1) != 2 || len(comparison.Path2) != 2 {
			t.Error("Both paths should have 2 nodes")
		}

		if comparison.Recommendation == "" {
			t.Error("Expected recommendation to be set")
		}
	})
}

func TestRootCauseRank(t *testing.T) {
	t.Run("Root Cause Rank Fields", func(t *testing.T) {
		rank := RootCauseRank{
			CauseID: "service-1",
			Rank:    1,
			Score:   0.9,
			Reasoning: "Most likely cause",
		}

		if rank.CauseID == "" {
			t.Error("CauseID should not be empty")
		}

		if rank.Rank < 1 {
			t.Errorf("Rank should be >= 1, got %d", rank.Rank)
		}

		if rank.Score < 0 || rank.Score > 1 {
			t.Errorf("Score should be between 0 and 1, got %f", rank.Score)
		}
	})
}

// Helper function

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > len(substr) &&
		(s[:len(substr)] == substr ||
		 s[len(s)-len(substr):] == substr ||
		 indexOfString(s, substr) >= 0))
}

func indexOfString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
