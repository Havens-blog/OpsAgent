// Package agent provides data source factory functions for RCA integration.
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/integrations"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

// DataSourceConfig holds configuration for all RCA data sources.
type DataSourceConfig struct {
	PrometheusEndpoint string
	ARMSEndpoint       string
	ARMSPid            string
	ARMSAccessKey      string
	ARMSSecretKey      string
	K8sEndpoint        string
	K8sToken           string
	K8sNamespace       string
}

// CreateRCASources creates all RCA data sources from configuration.
func CreateRCASources(cfg *DataSourceConfig) (*RCASources, error) {
	if cfg == nil {
		return nil, fmt.Errorf("data source config is required")
	}

	sources := &RCASources{}

	if cfg.PrometheusEndpoint != "" {
		promSource, err := integrations.NewPrometheusMetricSource(cfg.PrometheusEndpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to create Prometheus source: %w", err)
		}
		sources.MetricSource = promSource
	}

	if cfg.ARMSEndpoint != "" && cfg.ARMSPid != "" {
		armsSource, err := integrations.NewARMSTopologySource(cfg.ARMSEndpoint, cfg.ARMSPid, cfg.ARMSAccessKey, cfg.ARMSSecretKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create ARMS source: %w", err)
		}
		sources.TopologySource = armsSource
	}

	if cfg.K8sEndpoint != "" && cfg.K8sToken != "" {
		k8sSource, err := integrations.NewK8sChangeSource(cfg.K8sEndpoint, cfg.K8sToken, cfg.K8sNamespace)
		if err != nil {
			return nil, fmt.Errorf("failed to create K8s source: %w", err)
		}
		sources.ChangeSource = k8sSource
	}

	return sources, nil
}

// CreateMockRCASources creates mock data sources for testing.
func CreateMockRCASources() *RCASources {
	return &RCASources{
		LogSource:      &MockLogSource{},
		MetricSource:   &MockMetricSource{},
		TopologySource: &MockTopologySource{},
		ChangeSource:   &MockChangeSource{},
	}
}

// MockMetricSource implements sources.MetricSource for testing.
type MockMetricSource struct{}

func (m *MockMetricSource) QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
	return []models.MetricSeries{
		{
			Name:   "http_requests_total",
			Labels: map[string]string{"service": "order-service", "status": "5xx"},
			Unit:   "requests",
			DataPoints: []models.DataPoint{
				{Timestamp: start, Value: 0.01},
				{Timestamp: start.Add(5 * time.Minute), Value: 0.25},
			},
		},
	}, nil
}

func (m *MockMetricSource) QueryMetric(ctx context.Context, metricName string, start, end time.Time, filters map[string]string) (*models.MetricSeries, error) {
	return &models.MetricSeries{
		Name:   metricName,
		Labels: filters,
		Unit:   "value",
		DataPoints: []models.DataPoint{
			{Timestamp: start, Value: 100},
			{Timestamp: end, Value: 200},
		},
	}, nil
}

func (m *MockMetricSource) QueryMetricsForService(ctx context.Context, service string, start, end time.Time) ([]models.MetricSeries, error) {
	return []models.MetricSeries{
		{
			Name:   "http_requests_total",
			Labels: map[string]string{"service": service},
			Unit:   "requests",
			DataPoints: []models.DataPoint{
				{Timestamp: start, Value: 50},
			},
		},
	}, nil
}

func (m *MockMetricSource) GetAlerts(ctx context.Context) ([]sources.Alert, error) {
	return []sources.Alert{
		{
			Labels:      map[string]string{"alert": "HighErrorRate", "service": "order-service"},
			Annotations: map[string]string{"summary": "High error rate detected"},
			State:       "firing",
		},
	}, nil
}

// MockTopologySource implements sources.TopologySource for testing.
type MockTopologySource struct{}

func (m *MockTopologySource) GetTopology(ctx context.Context) (*models.Topology, error) {
	return &models.Topology{
		Nodes: []models.ServiceNode{
			{Name: "order-service", Type: "application"},
			{Name: "payment-service", Type: "application"},
		},
		Edges: []models.DependencyEdge{
			{From: "order-service", To: "payment-service", Type: "http"},
		},
	}, nil
}

func (m *MockTopologySource) GetServiceNeighbors(ctx context.Context, service string) (dependents, dependencies []string, err error) {
	if service == "order-service" {
		return nil, []string{"payment-service"}, nil
	}
	return []string{"order-service"}, nil, nil
}

func (m *MockTopologySource) GetServiceHealth(ctx context.Context) (map[string]string, error) {
	return map[string]string{
		"order-service":   "healthy",
		"payment-service": "healthy",
	}, nil
}

// MockChangeSource implements sources.ChangeSource for testing.
type MockChangeSource struct{}

func (m *MockChangeSource) QueryChanges(ctx context.Context, start, end time.Time) ([]models.Change, error) {
	return []models.Change{
		{
			ID:        "deploy-001",
			Timestamp: start.Add(30 * time.Minute),
			Service:   "order-service",
			Type:      "deployment",
		},
	}, nil
}

func (m *MockChangeSource) QueryChangesByService(ctx context.Context, service string, start, end time.Time) ([]models.Change, error) {
	return []models.Change{
		{
			ID:        "deploy-001",
			Timestamp: start.Add(30 * time.Minute),
			Service:   service,
			Type:      "deployment",
		},
	}, nil
}

func (m *MockChangeSource) GetLastChange(ctx context.Context, service string) (*models.Change, error) {
	return &models.Change{
		ID:        "deploy-001",
		Service:   service,
		Timestamp: time.Now(),
		Type:      "deployment",
	}, nil
}

// MockLogSource implements sources.LogSource for testing.
type MockLogSource struct{}

func (m *MockLogSource) QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
	return []models.LogEntry{
		{
			Timestamp: start.Add(10 * time.Minute),
			Service:   "order-service",
			Level:     "error",
			Message:   "Connection refused to database at 192.168.1.100",
		},
		{
			Timestamp: start.Add(15 * time.Minute),
			Service:   "order-service",
			Level:     "error",
			Message:   "Connection refused to database at 192.168.1.101",
		},
		{
			Timestamp: start.Add(20 * time.Minute),
			Service:   "order-service",
			Level:     "error",
			Message:   "Timeout waiting for response from api-gateway",
		},
		{
			Timestamp: start.Add(25 * time.Minute),
			Service:   "order-service",
			Level:     "error",
			Message:   "Timeout waiting for response from api-gateway",
		},
	}, nil
}

func (m *MockLogSource) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	return []models.LogEntry{
		{
			Timestamp: start.Add(5 * time.Minute),
			Service:   service,
			Level:     "info",
			Message:   "Request received",
		},
		{
			Timestamp: start.Add(10 * time.Minute),
			Service:   service,
			Level:     "error",
			Message:   "Connection refused",
		},
	}, nil
}

func (m *MockLogSource) GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error) {
	// Return different counts based on service
	if service == "order-service" {
		return 100, nil
	}
	return 50, nil
}
