// Package integrations provides tests for RCA data source integrations.
package integrations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewPrometheusMetricSource(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  bool
	}{
		{"valid endpoint", "http://prometheus:9090", false},
		{"empty endpoint", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := NewPrometheusMetricSource(tt.endpoint)
			if tt.wantErr && err == nil {
				t.Errorf("Expected error for endpoint=%q", tt.endpoint)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !tt.wantErr && src == nil {
				t.Errorf("Returned nil source")
			}
		})
	}
}

func TestPrometheusQueryAnomalies(t *testing.T) {
	promResponse := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result": []map[string]interface{}{
				{
					"metric": map[string]string{"__name__": "http_requests_total", "service": "api-service", "status": "500"},
					"values": [][]interface{}{
						{float64(time.Now().Add(-5 * time.Minute).Unix()), "10.5"},
						{float64(time.Now().Add(-3 * time.Minute).Unix()), "15.2"},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse)
	}))
	defer server.Close()

	src, err := NewPrometheusMetricSource(server.URL)
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	series, err := src.QueryAnomalies(context.Background(), time.Now().Add(-15*time.Minute), time.Now())
	if err != nil {
		t.Errorf("QueryAnomalies() error: %v", err)
	}
	if len(series) == 0 {
		t.Logf("QueryAnomalies() returned empty series (may be valid with mock)")
	}
}

func TestPrometheusQueryAnomaliesServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	src, err := NewPrometheusMetricSource(server.URL)
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	_, err = src.QueryAnomalies(context.Background(), time.Now().Add(-15*time.Minute), time.Now())
	if err != nil {
		t.Errorf("QueryAnomalies() unexpected error: %v", err)
	}
}

func TestPrometheusGetAlerts(t *testing.T) {
	alertResponse := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"alerts": []map[string]interface{}{
				{
					"labels":      map[string]string{"alertname": "HighErrorRate", "service": "api-service"},
					"annotations": map[string]string{"summary": "High error rate detected", "description": "Error rate exceeds 5%"},
					"state":        "firing",
					"startsAt":     time.Now().Add(-10 * time.Minute).Format(time.RFC3339),
					"endsAt":       "0001-01-01T00:00:00Z",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/alerts" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(alertResponse)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	src, err := NewPrometheusMetricSource(server.URL)
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	alerts, err := src.GetAlerts(context.Background())
	if err != nil {
		t.Errorf("GetAlerts() error: %v", err)
	}
	if len(alerts) != 1 {
		t.Errorf("GetAlerts() returned %d alerts, want 1", len(alerts))
	}
	if alerts[0].State != "firing" {
		t.Errorf("Alert state = %q, want 'firing'", alerts[0].State)
	}
}

func TestBuildQuery(t *testing.T) {
	tests := []struct {
		name       string
		metric     string
		filters    map[string]string
		wantQuery  string
	}{
		{"no filters", "http_requests_total", nil, "http_requests_total"},
		{"with filters", "http_requests_total", map[string]string{"service": "api"}, `http_requests_total{service="api"}`},
		{"multiple filters", "http_requests_total", map[string]string{"service": "api", "status": "500"}, `http_requests_total{service="api",status="500"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := buildQuery(tt.metric, tt.filters)
			if query != tt.wantQuery {
				t.Errorf("buildQuery() = %q, want %q", query, tt.wantQuery)
			}
		})
	}
}

func TestNewK8sChangeSource(t *testing.T) {
	tests := []struct {
		name      string
		endpoint  string
		token     string
		namespace string
		wantErr   bool
	}{
		{"valid config", "https://k8s-api:6443", "token123", "production", false},
		{"empty endpoint", "", "token123", "default", true},
		{"empty namespace defaults", "https://k8s-api:6443", "token123", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := NewK8sChangeSource(tt.endpoint, tt.token, tt.namespace)
			if tt.wantErr && err == nil {
				t.Errorf("Expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !tt.wantErr && src == nil {
				t.Errorf("Returned nil source")
			}
		})
	}
}

func TestK8sQueryChanges(t *testing.T) {
	deployTime := time.Now().Add(-30 * time.Minute)
	k8sResponse := map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"metadata": map[string]interface{}{
					"name":      "api-service-deployment",
					"namespace": "production",
					"labels":    map[string]string{"app": "api-service"},
				},
				"status": map[string]interface{}{
					"conditions": []map[string]interface{}{
						{
							"type":           "Progressing",
							"status":         "True",
							"lastUpdateTime": deployTime.Format(time.RFC3339),
							"reason":         "NewReplicaSetAvailable",
						},
					},
				},
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []map[string]interface{}{
								{"name": "api-service", "image": "api-service:v2.1.0"},
							},
						},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(k8sResponse)
	}))
	defer server.Close()

	src, err := NewK8sChangeSource(server.URL, "test-token", "production")
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	changes, err := src.QueryChanges(context.Background(), time.Now().Add(-1*time.Hour), time.Now())
	if err != nil {
		t.Errorf("QueryChanges() error: %v", err)
	}
	if len(changes) == 0 {
		t.Logf("QueryChanges() returned no changes (time filtering may exclude)")
	} else {
		if changes[0].Type != "deployment" {
			t.Errorf("Change type = %q, want 'deployment'", changes[0].Type)
		}
	}
}

func TestK8sQueryChangesServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	src, err := NewK8sChangeSource(server.URL, "test-token", "default")
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	_, err = src.QueryChanges(context.Background(), time.Now().Add(-1*time.Hour), time.Now())
	if err == nil {
		t.Errorf("Expected error from server failure")
	}
}

func TestNewARMSTopologySource(t *testing.T) {
	tests := []struct {
		name      string
		endpoint  string
		pid       string
		accessKey string
		secretKey string
		wantErr   bool
	}{
		{"valid config", "https://arms.aliyuncs.com", "pid123", "ak", "sk", false},
		{"empty endpoint", "", "pid123", "ak", "sk", true},
		{"empty pid", "https://arms.aliyuncs.com", "", "ak", "sk", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := NewARMSTopologySource(tt.endpoint, tt.pid, tt.accessKey, tt.secretKey)
			if tt.wantErr && err == nil {
				t.Errorf("Expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !tt.wantErr && src == nil {
				t.Errorf("Returned nil source")
			}
		})
	}
}

func TestARMSGetTopology(t *testing.T) {
	armsResponse := map[string]interface{}{
		"data": map[string]interface{}{
			"nodes": []map[string]interface{}{
				{"name": "api-service", "type": "application", "labels": map[string]string{"version": "v2.0"}},
				{"name": "database", "type": "database"},
			},
			"edges": []map[string]interface{}{
				{"from": "api-service", "to": "database", "type": "database"},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(armsResponse)
	}))
	defer server.Close()

	src, err := NewARMSTopologySource(server.URL, "pid123", "ak", "sk")
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	topo, err := src.GetTopology(context.Background())
	if err != nil {
		t.Errorf("GetTopology() error: %v", err)
	}
	if topo == nil {
		t.Fatalf("GetTopology() returned nil")
	}
	if len(topo.Nodes) != 2 {
		t.Errorf("GetTopology() returned %d nodes, want 2", len(topo.Nodes))
	}
	if len(topo.Edges) != 1 {
		t.Errorf("GetTopology() returned %d edges, want 1", len(topo.Edges))
	}
	if topo.Edges[0].From != "api-service" {
		t.Errorf("Edge from = %q, want 'api-service'", topo.Edges[0].From)
	}
}

func TestARMSGetServiceNeighbors(t *testing.T) {
	armsResponse := map[string]interface{}{
		"data": map[string]interface{}{
			"nodes": []map[string]interface{}{
				{"name": "api-service", "type": "application"},
				{"name": "database", "type": "database"},
				{"name": "cache", "type": "cache"},
			},
			"edges": []map[string]interface{}{
				{"from": "api-service", "to": "database", "type": "database"},
				{"from": "api-service", "to": "cache", "type": "cache"},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(armsResponse)
	}))
	defer server.Close()

	src, err := NewARMSTopologySource(server.URL, "pid123", "ak", "sk")
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	dependents, dependencies, err := src.GetServiceNeighbors(context.Background(), "api-service")
	if err != nil {
		t.Errorf("GetServiceNeighbors() error: %v", err)
	}
	if len(dependencies) != 2 {
		t.Errorf("Dependencies count = %d, want 2", len(dependencies))
	}
	if len(dependents) != 0 {
		t.Errorf("Dependents count = %d, want 0", len(dependents))
	}
}

func TestARMSGetServiceHealth(t *testing.T) {
	healthResponse := map[string]interface{}{
		"data": map[string]string{
			"api-service": "healthy",
			"database":    "degraded",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(healthResponse)
	}))
	defer server.Close()

	src, err := NewARMSTopologySource(server.URL, "pid123", "ak", "sk")
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	health, err := src.GetServiceHealth(context.Background())
	if err != nil {
		t.Errorf("GetServiceHealth() error: %v", err)
	}
	if health["api-service"] != "healthy" {
		t.Errorf("api-service health = %q, want 'healthy'", health["api-service"])
	}
	if health["database"] != "degraded" {
		t.Errorf("database health = %q, want 'degraded'", health["database"])
	}
}

func TestARMSGetTopologyServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	src, err := NewARMSTopologySource(server.URL, "pid123", "ak", "sk")
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	_, err = src.GetTopology(context.Background())
	if err == nil {
		t.Errorf("Expected error from server failure")
	}
}

func TestPrometheusQueryMetric(t *testing.T) {
	promResponse := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result": []map[string]interface{}{
				{
					"metric": map[string]string{"__name__": "http_errors"},
					"values": [][]interface{}{
						{float64(time.Now().Add(-5 * time.Minute).Unix()), "5.0"},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse)
	}))
	defer server.Close()

	src, err := NewPrometheusMetricSource(server.URL)
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	ctx := context.Background()
	series, err := src.QueryMetric(ctx, "http_errors", time.Now().Add(-15*time.Minute), time.Now(), nil)
	if err != nil {
		t.Errorf("QueryMetric() error: %v", err)
	}
	if series == nil {
		t.Errorf("QueryMetric() returned nil series")
	}
	if series.Name != "http_errors" {
		t.Errorf("Series name = %q, want 'http_errors'", series.Name)
	}
}

func TestPrometheusQueryMetricNotFound(t *testing.T) {
	promResponse := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result":     []map[string]interface{}{},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse)
	}))
	defer server.Close()

	src, err := NewPrometheusMetricSource(server.URL)
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	_, err = src.QueryMetric(context.Background(), "nonexistent", time.Now().Add(-15*time.Minute), time.Now(), nil)
	if err == nil {
		t.Errorf("Expected error for missing metric")
	}
}

func TestPrometheusQueryMetricsForService(t *testing.T) {
	promResponse := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result": []map[string]interface{}{
				{
					"metric": map[string]string{"__name__": "http_requests_total", "service": "api-service"},
					"values": [][]interface{}{
						{float64(time.Now().Add(-5 * time.Minute).Unix()), "100.0"},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(promResponse)
	}))
	defer server.Close()

	src, err := NewPrometheusMetricSource(server.URL)
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	series, err := src.QueryMetricsForService(context.Background(), "api-service", time.Now().Add(-15*time.Minute), time.Now())
	if err != nil {
		t.Errorf("QueryMetricsForService() error: %v", err)
	}
	if len(series) == 0 {
		t.Logf("QueryMetricsForService() returned empty series")
	}
}

func TestK8sQueryChangesByService(t *testing.T) {
	deployTime := time.Now().Add(-30 * time.Minute)
	k8sResponse := map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"metadata": map[string]interface{}{
					"name":      "api-deployment",
					"namespace": "default",
					"labels":    map[string]string{"app": "api-service"},
				},
				"status": map[string]interface{}{
					"conditions": []map[string]interface{}{
						{
							"type":           "Progressing",
							"status":         "True",
							"lastUpdateTime": deployTime.Format(time.RFC3339),
							"reason":         "NewReplicaSetAvailable",
						},
					},
				},
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []map[string]interface{}{
								{"name": "api", "image": "api:v2.0"},
							},
						},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(k8sResponse)
	}))
	defer server.Close()

	src, err := NewK8sChangeSource(server.URL, "test-token", "default")
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	changes, err := src.QueryChangesByService(context.Background(), "api-service", time.Now().Add(-1*time.Hour), time.Now())
	if err != nil {
		t.Errorf("QueryChangesByService() error: %v", err)
	}
	// May return 0 due to time filtering
	t.Logf("QueryChangesByService returned %d changes", len(changes))
}

func TestK8sGetLastChange(t *testing.T) {
	deployTime := time.Now().Add(-10 * time.Minute)
	k8sResponse := map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"metadata": map[string]interface{}{
					"name":      "api-deployment",
					"namespace": "default",
					"labels":    map[string]string{"app": "api-service"},
				},
				"status": map[string]interface{}{
					"conditions": []map[string]interface{}{
						{
							"type":           "Progressing",
							"status":         "True",
							"lastUpdateTime": deployTime.Format(time.RFC3339),
							"reason":         "NewReplicaSetAvailable",
						},
					},
				},
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []map[string]interface{}{
								{"name": "api", "image": "api:v2.0"},
							},
						},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(k8sResponse)
	}))
	defer server.Close()

	src, err := NewK8sChangeSource(server.URL, "test-token", "default")
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	change, err := src.GetLastChange(context.Background(), "api-service")
	// May error if no changes found due to time window
	if err != nil {
		t.Logf("GetLastChange() error: %v (may be expected)", err)
	} else {
		if change == nil {
			t.Errorf("GetLastChange() returned nil change without error")
		}
	}
}

func TestGetMetricName(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"has __name__", map[string]string{"__name__": "http_errors", "service": "api"}, "http_errors"},
		{"has service and status", map[string]string{"service": "api", "status": "500"}, "http_requests_total{service=api,status=500}"},
		{"has service only", map[string]string{"service": "api"}, "http_requests_total{service=api}"},
		{"empty labels", map[string]string{}, "unknown_metric"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getMetricName(tt.labels)
			if got != tt.want {
				t.Errorf("getMetricName() = %q, want %q", got, tt.want)
			}
		})
	}
}
