// Package topology provides tests for propagation analysis.
package topology

import (
	"context"
	"testing"
	"time"
)

func TestPropagationAnalyzer(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)

	t.Run("Analyze Propagation", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		affectedServices := []string{"order-service", "postgres-order"}

		analysis, err := analyzer.AnalyzePropagation(
			context.Background(),
			"incident-001",
			affectedServices,
			window,
		)

		if err != nil {
			t.Fatalf("Failed to analyze propagation: %v", err)
		}

		if analysis == nil {
			t.Fatal("Expected analysis, got nil")
		}

		if analysis.IncidentID != "incident-001" {
			t.Errorf("Expected incident ID incident-001, got %s", analysis.IncidentID)
		}

		if len(analysis.AffectedServices) != 2 {
			t.Errorf("Expected 2 affected services, got %d", len(analysis.AffectedServices))
		}
	})

	t.Run("Analyze Propagation with No Services", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		analysis, err := analyzer.AnalyzePropagation(
			context.Background(),
			"incident-002",
			[]string{},
			window,
		)

		if err != nil {
			t.Fatalf("Failed to analyze propagation: %v", err)
		}

		if analysis == nil {
			t.Fatal("Expected analysis, got nil")
		}

		if analysis.PropagationTree == nil {
			// Expected with no services
			return
		}
	})
}

func TestBuildPropagationTree(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)

	t.Run("Build Tree with Affected Services", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		graph, err := client.GetServiceGraph(context.Background(), window)
		if err != nil {
			t.Fatalf("Failed to get graph: %v", err)
		}

		affectedServices := []string{"order-service", "postgres-order"}
		tree := analyzer.buildPropagationTree(graph, affectedServices)

		if tree == nil {
			t.Fatal("Expected tree, got nil")
		}

		if tree.Root == nil {
			t.Fatal("Expected root node, got nil")
		}

		if tree.Direction != "downstream" {
			t.Errorf("Expected direction downstream, got %s", tree.Direction)
		}
	})
}

func TestDetermineRootCandidate(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)

	t.Run("Find Root by Error Rate", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		graph, err := client.GetServiceGraph(context.Background(), window)
		if err != nil {
			t.Fatalf("Failed to get graph: %v", err)
		}

		affectedServices := []string{"order-service", "postgres-order", "api-gateway"}
		root := analyzer.determineRootCandidate(graph, affectedServices)

		if root == "" {
			t.Error("Expected root candidate, got empty string")
		}

		// order-service has highest error rate in mock data (0.025)
		if root != "order-service" {
			t.Errorf("Expected root order-service, got %s", root)
		}
	})
}

func TestCalculatePathConfidence(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)

	t.Run("Single Step Path", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		graph, err := client.GetServiceGraph(context.Background(), window)
		if err != nil {
			t.Fatalf("Failed to get graph: %v", err)
		}

		steps := []PropagationStep{
			{
				FromNode: "node-a",
				ToNode:   "node-b",
				Impact:   0.05,
			},
		}

		confidence := analyzer.calculatePathConfidence(steps, graph)

		if confidence <= 0 || confidence > 1 {
			t.Errorf("Expected confidence between 0 and 1, got %f", confidence)
		}
	})

	t.Run("Multi Step Path", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		graph, err := client.GetServiceGraph(context.Background(), window)
		if err != nil {
			t.Fatalf("Failed to get graph: %v", err)
		}

		steps := []PropagationStep{
			{FromNode: "node-a", ToNode: "node-b", Impact: 0.02},
			{FromNode: "node-b", ToNode: "node-c", Impact: 0.03},
			{FromNode: "node-c", ToNode: "node-d", Impact: 0.01},
		}

		confidence := analyzer.calculatePathConfidence(steps, graph)

		// Longer path should have lower confidence
		if confidence <= 0 || confidence > 1 {
			t.Errorf("Expected confidence between 0 and 1, got %f", confidence)
		}
	})
}

func TestGetUpstreamImpact(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)

	t.Run("Get Upstream Impact", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		impact, err := analyzer.GetUpstreamImpact(context.Background(), "order-service", window)
		if err != nil {
			t.Fatalf("Failed to get upstream impact: %v", err)
		}

		if impact == nil {
			t.Fatal("Expected impact, got nil")
		}

		if impact.ServiceID != "order-service" {
			t.Errorf("Expected service ID order-service, got %s", impact.ServiceID)
		}

		if len(impact.UpstreamServices) == 0 {
			t.Error("Expected at least one upstream service")
		}
	})
}

func TestGetDownstreamImpact(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)

	t.Run("Get Downstream Impact", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		impact, err := analyzer.GetDownstreamImpact(context.Background(), "api-gateway", window)
		if err != nil {
			t.Fatalf("Failed to get downstream impact: %v", err)
		}

		if impact == nil {
			t.Fatal("Expected impact, got nil")
		}

		if impact.ServiceID != "api-gateway" {
			t.Errorf("Expected service ID api-gateway, got %s", impact.ServiceID)
		}

		if len(impact.DownstreamServices) == 0 {
			t.Error("Expected at least one downstream service")
		}
	})
}

func TestGetBlastRadius(t *testing.T) {
	client := NewARMSClient()
	analyzer := NewPropagationAnalyzer(client)

	t.Run("Get Blast Radius", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		radius, err := analyzer.GetBlastRadius(context.Background(), "api-gateway", window)
		if err != nil {
			t.Fatalf("Failed to get blast radius: %v", err)
		}

		if radius == nil {
			t.Fatal("Expected radius, got nil")
		}

		if radius.ServiceID != "api-gateway" {
			t.Errorf("Expected service ID api-gateway, got %s", radius.ServiceID)
		}

		if radius.TotalServices == 0 {
			t.Error("Expected at least one affected service")
		}

		if radius.RiskLevel == "" {
			t.Error("Expected risk level to be set")
		}
	})
}

func TestCalculateRiskLevel(t *testing.T) {
	tests := []struct {
		name        string
		impact      float64
		expected    string
	}{
		{"Critical Impact", 4.0, "critical"},
		{"High Impact", 2.5, "high"},
		{"Medium Impact", 1.5, "medium"},
		{"Low Impact", 0.5, "low"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := calculateRiskLevel(tt.impact)
			if level != tt.expected {
				t.Errorf("Expected risk level %s, got %s", tt.expected, level)
			}
		})
	}
}

func TestCalculateServiceImpact(t *testing.T) {
	t.Run("Healthy Service", func(t *testing.T) {
		node := ServiceNode{
			ID:        "healthy-service",
			Name:      "Healthy Service",
			Health:    "healthy",
			ErrorRate: 0.001,
			Latency:   25.0,
		}

		impact := calculateServiceImpact(node, 0.001)
		if impact > 0.1 {
			t.Errorf("Healthy service should have low impact, got %f", impact)
		}
	})

	t.Run("Degraded Service", func(t *testing.T) {
		node := ServiceNode{
			ID:        "degraded-service",
			Name:      "Degraded Service",
			Health:    "degraded",
			ErrorRate: 0.025,
			Latency:   120.0,
		}

		impact := calculateServiceImpact(node, 0.025)
		if impact < 0.3 {
			t.Errorf("Degraded service should have medium impact, got %f", impact)
		}
	})

	t.Run("Unhealthy Service", func(t *testing.T) {
		node := ServiceNode{
			ID:        "unhealthy-service",
			Name:      "Unhealthy Service",
			Health:    "unhealthy",
			ErrorRate: 0.1,
			Latency:   500.0,
		}

		impact := calculateServiceImpact(node, 0.1)
		if impact < 0.5 {
			t.Errorf("Unhealthy service should have high impact, got %f", impact)
		}
	})
}

func TestPropagationStep(t *testing.T) {
	t.Run("Propagation Step Fields", func(t *testing.T) {
		step := PropagationStep{
			FromNode:  "service-a",
			ToNode:    "service-b",
			EdgeType:  "http",
			Impact:    0.05,
			Reasoning: "Test reasoning",
			Timestamp: time.Now(),
		}

		if step.FromNode == "" || step.ToNode == "" {
			t.Error("FromNode and ToNode should not be empty")
		}
		if step.Impact < 0 || step.Impact > 1 {
			t.Errorf("Impact should be between 0 and 1, got %f", step.Impact)
		}
	})
}

func TestPropagationNode(t *testing.T) {
	t.Run("Propagation Node Fields", func(t *testing.T) {
		node := &PropagationNode{
			ServiceID:   "service-1",
			ServiceName: "Service 1",
			Children:     []*PropagationNode{
				{ServiceID: "service-2", ServiceName: "Service 2"},
			},
			Impact:    0.8,
			Health:    "degraded",
			ErrorRate: 0.025,
			Latency:   120.0,
		}

		if node.ServiceID == "" {
			t.Error("ServiceID should not be empty")
		}
		if len(node.Children) != 1 {
			t.Errorf("Expected 1 child, got %d", len(node.Children))
		}
	})
}

func TestUpstreamImpact(t *testing.T) {
	t.Run("Upstream Impact Structure", func(t *testing.T) {
		impact := &UpstreamImpact{
			ServiceID: "test-service",
			UpstreamServices: []ServiceImpact{
				{ServiceID: "upstream-1", ImpactScore: 0.5},
				{ServiceID: "upstream-2", ImpactScore: 0.3},
			},
			TotalImpact: 0.8,
		}

		if impact.ServiceID == "" {
			t.Error("ServiceID should not be empty")
		}
		if len(impact.UpstreamServices) != 2 {
			t.Errorf("Expected 2 upstream services, got %d", len(impact.UpstreamServices))
		}
	})
}

func TestDownstreamImpact(t *testing.T) {
	t.Run("Downstream Impact Structure", func(t *testing.T) {
		impact := &DownstreamImpact{
			ServiceID: "test-service",
			DownstreamServices: []ServiceImpact{
				{ServiceID: "downstream-1", ImpactScore: 0.6},
				{ServiceID: "downstream-2", ImpactScore: 0.4},
			},
			TotalImpact: 1.0,
		}

		if impact.ServiceID == "" {
			t.Error("ServiceID should not be empty")
		}
		if len(impact.DownstreamServices) != 2 {
			t.Errorf("Expected 2 downstream services, got %d", len(impact.DownstreamServices))
		}
	})
}

func TestBlastRadius(t *testing.T) {
	t.Run("Blast Radius Structure", func(t *testing.T) {
		upstream := &UpstreamImpact{
			ServiceID: "test-service",
			UpstreamServices: []ServiceImpact{
				{ServiceID: "upstream-1", ImpactScore: 0.3},
			},
			TotalImpact: 0.3,
		}

		downstream := &DownstreamImpact{
			ServiceID: "test-service",
			DownstreamServices: []ServiceImpact{
				{ServiceID: "downstream-1", ImpactScore: 0.5},
				{ServiceID: "downstream-2", ImpactScore: 0.2},
			},
			TotalImpact: 0.7,
		}

		radius := &BlastRadius{
			ServiceID:        "test-service",
			DownstreamImpact: downstream,
			UpstreamImpact:  upstream,
			TotalServices:   3,
			RiskLevel:        "medium",
		}

		if radius.ServiceID == "" {
			t.Error("ServiceID should not be empty")
		}
		if radius.TotalServices != 3 {
			t.Errorf("Expected 3 total services, got %d", radius.TotalServices)
		}
		if radius.RiskLevel == "" {
			t.Error("RiskLevel should not be empty")
		}
	})
}
