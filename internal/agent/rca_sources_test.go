// Package agent provides tests for RCA data source integration.
package agent

import (
	"context"
	"testing"
	"time"
)

// TestCreateMockRCASources tests the mock data source factory.
func TestCreateMockRCASources(t *testing.T) {
	sources := CreateMockRCASources()

	if sources == nil {
		t.Fatal("Expected non-nil sources")
	}

	if sources.MetricSource == nil {
		t.Error("Expected MetricSource to be set")
	}
	if sources.TopologySource == nil {
		t.Error("Expected TopologySource to be set")
	}
	if sources.ChangeSource == nil {
		t.Error("Expected ChangeSource to be set")
	}
}

// TestMockMetricSource tests the mock metric source implementation.
func TestMockMetricSource(t *testing.T) {
	ctx := context.Background()
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()
	mock := &MockMetricSource{}

	t.Run("QueryAnomalies", func(t *testing.T) {
		series, err := mock.QueryAnomalies(ctx, start, end)
		if err != nil {
			t.Fatalf("QueryAnomalies failed: %v", err)
		}

		if len(series) == 0 {
			t.Error("Expected at least one anomaly series")
		}

		s := series[0]
		if s.Name != "http_requests_total" {
			t.Errorf("Expected name 'http_requests_total', got '%s'", s.Name)
		}
		if s.Unit != "requests" {
			t.Errorf("Expected unit 'requests', got '%s'", s.Unit)
		}
		if len(s.DataPoints) == 0 {
			t.Error("Expected data points")
		}
	})

	t.Run("QueryMetric", func(t *testing.T) {
		series, err := mock.QueryMetric(ctx, "cpu_usage", start, end, map[string]string{"service": "api"})
		if err != nil {
			t.Fatalf("QueryMetric failed: %v", err)
		}

		if series == nil {
			t.Fatal("Expected non-nil series")
		}
		if series.Name != "cpu_usage" {
			t.Errorf("Expected name 'cpu_usage', got '%s'", series.Name)
		}
	})

	t.Run("QueryMetricsForService", func(t *testing.T) {
		series, err := mock.QueryMetricsForService(ctx, "order-service", start, end)
		if err != nil {
			t.Fatalf("QueryMetricsForService failed: %v", err)
		}

		if len(series) == 0 {
			t.Error("Expected at least one metric series")
		}
	})

	t.Run("GetAlerts", func(t *testing.T) {
		alerts, err := mock.GetAlerts(ctx)
		if err != nil {
			t.Fatalf("GetAlerts failed: %v", err)
		}

		if len(alerts) == 0 {
			t.Error("Expected at least one alert")
		}

		alert := alerts[0]
		if alert.State != "firing" {
			t.Errorf("Expected state 'firing', got '%s'", alert.State)
		}
	})
}

// TestMockTopologySource tests the mock topology source implementation.
func TestMockTopologySource(t *testing.T) {
	ctx := context.Background()
	mock := &MockTopologySource{}

	t.Run("GetTopology", func(t *testing.T) {
		topology, err := mock.GetTopology(ctx)
		if err != nil {
			t.Fatalf("GetTopology failed: %v", err)
		}

		if topology == nil {
			t.Fatal("Expected non-nil topology")
		}
		if len(topology.Nodes) == 0 {
			t.Error("Expected at least one node")
		}
		if len(topology.Edges) == 0 {
			t.Error("Expected at least one edge")
		}

		// Verify order-service exists
		found := false
		for _, node := range topology.Nodes {
			if node.Name == "order-service" {
				found = true
				if node.Type != "application" {
					t.Errorf("Expected type 'application', got '%s'", node.Type)
				}
				break
			}
		}
		if !found {
			t.Error("Expected to find 'order-service' node")
		}
	})

	t.Run("GetServiceNeighbors", func(t *testing.T) {
		// Test order-service neighbors
		dependents, dependencies, err := mock.GetServiceNeighbors(ctx, "order-service")
		if err != nil {
			t.Fatalf("GetServiceNeighbors failed: %v", err)
		}

		// order-service has payment-service as a dependency
		if len(dependencies) == 0 {
			t.Error("Expected order-service to have dependencies")
		}
		if dependencies[0] != "payment-service" {
			t.Errorf("Expected dependency 'payment-service', got '%s'", dependencies[0])
		}
		// order-service has no dependents in this mock
		if len(dependents) != 0 {
			t.Error("Expected order-service to have no dependents")
		}
	})

	t.Run("GetServiceHealth", func(t *testing.T) {
		health, err := mock.GetServiceHealth(ctx)
		if err != nil {
			t.Fatalf("GetServiceHealth failed: %v", err)
		}

		if len(health) == 0 {
			t.Error("Expected health status for at least one service")
		}

		// Check order-service health
		if status, ok := health["order-service"]; !ok {
			t.Error("Expected health status for 'order-service'")
		} else if status != "healthy" {
			t.Errorf("Expected status 'healthy', got '%s'", status)
		}
	})
}

// TestMockChangeSource tests the mock change source implementation.
func TestMockChangeSource(t *testing.T) {
	ctx := context.Background()
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()
	mock := &MockChangeSource{}

	t.Run("QueryChanges", func(t *testing.T) {
		changes, err := mock.QueryChanges(ctx, start, end)
		if err != nil {
			t.Fatalf("QueryChanges failed: %v", err)
		}

		if len(changes) == 0 {
			t.Error("Expected at least one change")
		}

		change := changes[0]
		if change.ID != "deploy-001" {
			t.Errorf("Expected ID 'deploy-001', got '%s'", change.ID)
		}
		if change.Service != "order-service" {
			t.Errorf("Expected service 'order-service', got '%s'", change.Service)
		}
		if change.Type != "deployment" {
			t.Errorf("Expected type 'deployment', got '%s'", change.Type)
		}
	})

	t.Run("QueryChangesByService", func(t *testing.T) {
		changes, err := mock.QueryChangesByService(ctx, "api-service", start, end)
		if err != nil {
			t.Fatalf("QueryChangesByService failed: %v", err)
		}

		if len(changes) == 0 {
			t.Error("Expected at least one change")
		}

		change := changes[0]
		if change.Service != "api-service" {
			t.Errorf("Expected service 'api-service', got '%s'", change.Service)
		}
	})

	t.Run("GetLastChange", func(t *testing.T) {
		change, err := mock.GetLastChange(ctx, "order-service")
		if err != nil {
			t.Fatalf("GetLastChange failed: %v", err)
		}

		if change == nil {
			t.Fatal("Expected non-nil change")
		}
		if change.Service != "order-service" {
			t.Errorf("Expected service 'order-service', got '%s'", change.Service)
		}
	})
}

// TestCreateRCASources tests the real data source factory with empty config.
func TestCreateRCASources(t *testing.T) {
	t.Run("NilConfig", func(t *testing.T) {
		_, err := CreateRCASources(nil)
		if err == nil {
			t.Error("Expected error for nil config")
		}
	})

	t.Run("EmptyConfig", func(t *testing.T) {
		cfg := &DataSourceConfig{}
		sources, err := CreateRCASources(cfg)

		if err != nil {
			t.Fatalf("CreateRCASources failed: %v", err)
		}

		if sources.MetricSource != nil {
			t.Error("Expected nil MetricSource when no endpoint configured")
		}
		if sources.TopologySource != nil {
			t.Error("Expected nil TopologySource when no endpoint configured")
		}
		if sources.ChangeSource != nil {
			t.Error("Expected nil ChangeSource when no endpoint configured")
		}
	})
}

// TestRCASourcesIntegration tests the full RCA sources integration with MonitorAgent.
func TestRCASourcesIntegration(t *testing.T) {
	sources := CreateMockRCASources()

	// Verify all sources are mock implementations
	if sources.MetricSource == nil {
		t.Fatal("Expected MetricSource to be set")
	}

	ctx := context.Background()
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()

	// Test MetricSource
	_, err := sources.MetricSource.QueryAnomalies(ctx, start, end)
	if err != nil {
		t.Errorf("MetricSource.QueryAnomalies failed: %v", err)
	}

	// Test TopologySource
	_, err = sources.TopologySource.GetTopology(ctx)
	if err != nil {
		t.Errorf("TopologySource.GetTopology failed: %v", err)
	}

	health, err := sources.TopologySource.GetServiceHealth(ctx)
	if err != nil {
		t.Errorf("TopologySource.GetServiceHealth failed: %v", err)
	}
	if len(health) == 0 {
		t.Error("Expected health status for at least one service")
	}

	// Test ChangeSource
	changes, err := sources.ChangeSource.QueryChanges(ctx, start, end)
	if err != nil {
		t.Errorf("ChangeSource.QueryChanges failed: %v", err)
	}
	if len(changes) == 0 {
		t.Error("Expected at least one change")
	}
}
