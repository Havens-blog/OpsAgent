// Package agent provides tests for RCA data source integration.
package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
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

// TestCreateRCASourcesPingGate verifies the reachability probe added to
// CreateRCASources: a metric source whose endpoint answers is kept, while one
// whose endpoint fails is dropped to nil (so the fallback supplies a mock).
// This is what makes active["metrics"]=true mean "verified reachable" rather
// than merely "constructed".
func TestCreateRCASourcesPingGate(t *testing.T) {
	t.Run("KeptWhenReachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Ping issues GET /api/v1/query?query=vector(1); a 200 suffices.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		}))
		defer srv.Close()

		sources, err := CreateRCASources(&DataSourceConfig{PrometheusEndpoint: srv.URL})
		if err != nil {
			t.Fatalf("CreateRCASources failed: %v", err)
		}
		if sources.MetricSource == nil {
			t.Error("expected MetricSource kept when endpoint is reachable")
		}
	})

	t.Run("DroppedWhenUnreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()

		sources, err := CreateRCASources(&DataSourceConfig{PrometheusEndpoint: srv.URL})
		if err != nil {
			t.Fatalf("CreateRCASources failed: %v", err)
		}
		if sources.MetricSource != nil {
			t.Error("expected MetricSource dropped to nil when endpoint ping fails")
		}
	})

	t.Run("FallbackSuppliesMockForDroppedSource", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()

		result, active := CreateRCASourcesWithFallback(&DataSourceConfig{PrometheusEndpoint: srv.URL})
		if active["metrics"] {
			t.Error("expected active[metrics]=false when ping fails")
		}
		if result.MetricSource == nil {
			t.Error("expected fallback mock MetricSource to be present")
		}
	})
}

// TestCreateRCASourcesDualMetricWorkspaces verifies that when both the ARMS APM
// workspace and the Prometheus/CloudMonitor workspace are reachable, the metric
// source becomes a fanout combining the two; with only one configured it stays a
// single source; and with neither it stays nil.
func TestCreateRCASourcesDualMetricWorkspaces(t *testing.T) {
	newOKServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		}))
	}

	t.Run("BothWorkspacesBecomeFanout", func(t *testing.T) {
		srv := newOKServer()
		defer srv.Close()

		got, err := CreateRCASources(&DataSourceConfig{
			ARMSAPMEndpoint:    srv.URL,
			PrometheusEndpoint: srv.URL,
		})
		if err != nil {
			t.Fatalf("CreateRCASources failed: %v", err)
		}
		fanout, ok := got.MetricSource.(*sources.FanoutMetricSource)
		if !ok {
			t.Fatalf("expected fanout metric source, got %T", got.MetricSource)
		}
		if fanout.Count() != 2 {
			t.Errorf("expected 2 backing sources, got %d", fanout.Count())
		}
	})

	t.Run("SingleWorkspaceStaysSingle", func(t *testing.T) {
		srv := newOKServer()
		defer srv.Close()

		got, err := CreateRCASources(&DataSourceConfig{PrometheusEndpoint: srv.URL})
		if err != nil {
			t.Fatalf("CreateRCASources failed: %v", err)
		}
		if _, ok := got.MetricSource.(*sources.FanoutMetricSource); ok {
			t.Error("expected a single source, got a fanout")
		}
		if got.MetricSource == nil {
			t.Error("expected a non-nil metric source")
		}
	})
}

// TestCreateRCASourcesWithFallbackDualWorkspaces verifies the active[metrics]
// flag and fallback behaviour for the dual-workspace path.
func TestCreateRCASourcesWithFallbackDualWorkspaces(t *testing.T) {
	newOKServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		}))
	}

	t.Run("BothReachable", func(t *testing.T) {
		srv := newOKServer()
		defer srv.Close()

		result, active := CreateRCASourcesWithFallback(&DataSourceConfig{
			ARMSAPMEndpoint:    srv.URL,
			PrometheusEndpoint: srv.URL,
		})
		if !active["metrics"] {
			t.Error("expected active[metrics]=true")
		}
		if _, ok := result.MetricSource.(*sources.FanoutMetricSource); !ok {
			t.Errorf("expected fanout metric source, got %T", result.MetricSource)
		}
	})

	t.Run("OneReachableOneUnreachableStillActive", func(t *testing.T) {
		ok := newOKServer()
		defer ok.Close()
		bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer bad.Close()

		result, active := CreateRCASourcesWithFallback(&DataSourceConfig{
			ARMSAPMEndpoint:    ok.URL,
			PrometheusEndpoint: bad.URL,
		})
		if !active["metrics"] {
			t.Error("expected active[metrics]=true when at least one workspace is reachable")
		}
		if result.MetricSource == nil {
			t.Error("expected a non-nil metric source")
		}
		if _, ok := result.MetricSource.(*sources.FanoutMetricSource); ok {
			t.Error("expected a single source when only one workspace is reachable")
		}
	})

	t.Run("NeitherConfigured", func(t *testing.T) {
		result, active := CreateRCASourcesWithFallback(&DataSourceConfig{})
		if active["metrics"] {
			t.Error("expected active[metrics]=false with no workspace")
		}
		if _, ok := result.MetricSource.(*sources.EmptyMetricSource); !ok {
			t.Errorf("expected empty metric source, got %T", result.MetricSource)
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
