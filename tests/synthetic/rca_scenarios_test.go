// Package synthetic provides tests for synthetic RCA scenarios.
package synthetic

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// TestScenarios tests that all scenarios are properly defined.
func TestScenarios(t *testing.T) {
	scenarios := Scenarios()

	if len(scenarios) == 0 {
		t.Fatal("Expected at least one scenario")
	}

	t.Run("scenarios_are_well_formed", func(t *testing.T) {
		for _, scenario := range scenarios {
			if scenario.Name == "" {
				t.Errorf("Scenario has empty name")
			}
			if scenario.Incident == nil {
				t.Errorf("Scenario %s has nil incident", scenario.Name)
			}
			if scenario.Expected == nil {
				t.Errorf("Scenario %s has nil expected result", scenario.Name)
			}
			if err := scenario.Incident.Validate(); err != nil {
				t.Errorf("Scenario %s has invalid incident: %v", scenario.Name, err)
			}
		}
	})

	t.Run("all_scenarios_unique", func(t *testing.T) {
		names := make(map[string]bool)
		for _, scenario := range scenarios {
			if names[scenario.Name] {
				t.Errorf("Duplicate scenario name: %s", scenario.Name)
			}
			names[scenario.Name] = true
		}
	})
}

// TestDatabaseConnectionPoolExhaustion tests the DB pool exhaustion scenario.
func TestDatabaseConnectionPoolExhaustion(t *testing.T) {
	scenario := DatabaseConnectionPoolExhaustion()

	t.Run("scenario_is_valid", func(t *testing.T) {
		if scenario.Name != "database_connection_pool_exhaustion" {
			t.Errorf("Expected name 'database_connection_pool_exhaustion', got '%s'", scenario.Name)
		}
		if scenario.Incident.Severity != "P1" {
			t.Errorf("Expected severity P1, got %s", scenario.Incident.Severity)
		}
	})

	t.Run("data_sources_provide_data", func(t *testing.T) {
		ctx := context.Background()
		start := scenario.Incident.StartTime
		end := scenario.Incident.EndTime

		if scenario.DataSources.Logs == nil {
			t.Error("Expected logs data source")
		} else {
			logs, err := scenario.DataSources.Logs(ctx, start, end)
			if err != nil {
				t.Fatalf("Logs error: %v", err)
			}
			if len(logs) == 0 {
				t.Error("Expected logs from scenario")
			}
		}

		if scenario.DataSources.Metrics == nil {
			t.Error("Expected metrics data source")
		} else {
			metrics, err := scenario.DataSources.Metrics(ctx, start, end)
			if err != nil {
				t.Fatalf("Metrics error: %v", err)
			}
			if len(metrics) == 0 {
				t.Error("Expected metrics from scenario")
			}
		}
	})
}

// TestHighErrorRateAfterDeployment tests the post-deployment scenario.
func TestHighErrorRateAfterDeployment(t *testing.T) {
	scenario := HighErrorRateAfterDeployment()

	t.Run("scenario_is_valid", func(t *testing.T) {
		if scenario.Name != "high_error_rate_after_deployment" {
			t.Errorf("Expected name 'high_error_rate_after_deployment', got '%s'", scenario.Name)
		}
		if scenario.Incident.Severity != "P0" {
			t.Errorf("Expected severity P0, got %s", scenario.Incident.Severity)
		}
	})

	t.Run("has_deployment_change", func(t *testing.T) {
		ctx := context.Background()
		changes, err := scenario.DataSources.Changes(ctx, scenario.Incident.StartTime, scenario.Incident.EndTime)
		if err != nil {
			t.Fatalf("Changes error: %v", err)
		}
		if len(changes) == 0 {
			t.Error("Expected deployment change in scenario")
		}
		foundDeployment := false
		for _, change := range changes {
			if change.Type == "deployment" {
				foundDeployment = true
				break
			}
		}
		if !foundDeployment {
			t.Error("Expected to find deployment change")
		}
	})
}

// TestUpstreamDependencyFailure tests the upstream failure scenario.
func TestUpstreamDependencyFailure(t *testing.T) {
	scenario := UpstreamDependencyFailure()

	t.Run("scenario_is_valid", func(t *testing.T) {
		if scenario.Name != "upstream_dependency_failure" {
			t.Errorf("Expected name 'upstream_dependency_failure', got '%s'", scenario.Name)
		}
	})

	t.Run("has_topology_data", func(t *testing.T) {
		if scenario.DataSources.Topology == nil {
			t.Fatal("Expected topology data source")
		}
		ctx := context.Background()
		topo, err := scenario.DataSources.Topology(ctx)
		if err != nil {
			t.Fatalf("Topology error: %v", err)
		}
		if len(topo.Nodes) == 0 {
			t.Error("Expected topology nodes")
		}
		if len(topo.Edges) == 0 {
			t.Error("Expected topology edges")
		}
	})
}

// TestMemoryLeakProgressiveDegradation tests the memory leak scenario.
func TestMemoryLeakProgressiveDegradation(t *testing.T) {
	scenario := MemoryLeakProgressiveDegradation()

	t.Run("scenario_is_valid", func(t *testing.T) {
		if scenario.Name != "memory_leak_progressive_degradation" {
			t.Errorf("Expected name 'memory_leak_progressive_degradation', got '%s'", scenario.Name)
		}
	})

	t.Run("has_metric_trend", func(t *testing.T) {
		ctx := context.Background()
		metrics, err := scenario.DataSources.Metrics(ctx, scenario.Incident.StartTime, scenario.Incident.EndTime)
		if err != nil {
			t.Fatalf("Metrics error: %v", err)
		}
		if len(metrics) == 0 {
			t.Error("Expected metrics from scenario")
		}
		// Check that memory increases over time
		for _, metric := range metrics {
			if metric.Name == "jvm_memory_used_bytes" {
				if len(metric.DataPoints) < 2 {
					t.Error("Expected multiple data points for memory trend")
				}
				// Verify increasing trend
				increasing := true
				for i := 1; i < len(metric.DataPoints); i++ {
					if metric.DataPoints[i].Value <= metric.DataPoints[i-1].Value {
						increasing = false
						break
					}
				}
				if !increasing {
					t.Error("Expected memory to increase over time")
				}
			}
		}
	})
}

// TestRunScenario tests running individual scenarios.
func TestRunScenario(t *testing.T) {
	t.Run("database_pool_scenario", func(t *testing.T) {
		scenario := DatabaseConnectionPoolExhaustion()
		ctx := context.Background()

		result, err := RunScenario(ctx, scenario)
		if err != nil {
			t.Fatalf("RunScenario failed: %v", err)
		}

		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		if result.Report == nil {
			t.Error("Expected non-nil report")
		}

		if result.Report.Incident.ID != scenario.Incident.ID {
			t.Errorf("Expected incident ID %s, got %s", scenario.Incident.ID, result.Report.Incident.ID)
		}
	})
}

// TestSyntheticDataSources tests synthetic data source implementations.
func TestSyntheticDataSources(t *testing.T) {
	t.Run("SyntheticMetricSource", func(t *testing.T) {
		ctx := context.Background()
		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()

		dataFunc := func(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
			return []models.MetricSeries{
				{Name: "test_metric"},
			}, nil
		}

		source := &SyntheticMetricSource{dataFunc: dataFunc}

		series, err := source.QueryAnomalies(ctx, start, end)
		if err != nil {
			t.Fatalf("QueryAnomalies failed: %v", err)
		}
		if len(series) != 1 {
			t.Errorf("Expected 1 series, got %d", len(series))
		}
	})

	t.Run("SyntheticLogSource", func(t *testing.T) {
		ctx := context.Background()
		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()

		dataFunc := func(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
			return []models.LogEntry{
				{Service: "test-service", Level: "error"},
			}, nil
		}

		source := &SyntheticLogSource{dataFunc: dataFunc}

		logs, err := source.QueryErrors(ctx, start, end)
		if err != nil {
			t.Fatalf("QueryErrors failed: %v", err)
		}
		if len(logs) != 1 {
			t.Errorf("Expected 1 log, got %d", len(logs))
		}
	})

	t.Run("SyntheticTopologySource", func(t *testing.T) {
		ctx := context.Background()

		topoFunc := func(ctx context.Context) (*models.Topology, error) {
			return &models.Topology{
				Nodes: []models.ServiceNode{{Name: "service-1"}},
				Edges: []models.DependencyEdge{{From: "service-1", To: "service-2"}},
			}, nil
		}

		source := &SyntheticTopologySource{topologyFunc: topoFunc}

		topology, err := source.GetTopology(ctx)
		if err != nil {
			t.Fatalf("GetTopology failed: %v", err)
		}
		if len(topology.Nodes) != 1 {
			t.Errorf("Expected 1 node, got %d", len(topology.Nodes))
		}
	})

	t.Run("SyntheticChangeSource", func(t *testing.T) {
		ctx := context.Background()
		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()

		dataFunc := func(ctx context.Context, start, end time.Time) ([]models.Change, error) {
			return []models.Change{
				{Type: "deployment"},
			}, nil
		}

		source := &SyntheticChangeSource{dataFunc: dataFunc}

		changes, err := source.QueryChanges(ctx, start, end)
		if err != nil {
			t.Fatalf("QueryChanges failed: %v", err)
		}
		if len(changes) != 1 {
			t.Errorf("Expected 1 change, got %d", len(changes))
		}
	})
}
