// Package sources provides mock data source implementations for RCA analysis.
package sources

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// MockLogSource provides a mock implementation of LogSource for demo purposes.
type MockLogSource struct{}

// NewMockLogSource creates a new mock log source.
func NewMockLogSource() *MockLogSource {
	return &MockLogSource{}
}

// QueryErrors retrieves mock error logs within the time window.
//
// The data is positioned RELATIVE to the requested [start, end] window rather
// than anchored to time.Now(), so analysis works for any incident window the
// engine passes (the engine queries [incident.StartTime-windowSize,
// incident.EndTime]). The error spike is concentrated in the last ~20% of the
// window — which falls inside the incident window for typical incidents — with
// a light baseline earlier, so hasSignificantErrorSpike (>10 errors, >2x
// baseline) reliably triggers.
func (m *MockLogSource) QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
	span := end.Sub(start)
	if span <= 0 {
		span = 20 * time.Minute
		start = end.Add(-span)
	}

	logs := make([]models.LogEntry, 0, 20)

	// Light baseline: a couple of order-service errors near the FRONT of the
	// query window (before the incident window begins, which sits at
	// start+windowSize) so the baseline average is non-zero and
	// scoreTimeCorrelation can compute an incident/baseline ratio.
	for _, frac := range []float64{0.02, 0.05} {
		logs = append(logs, models.LogEntry{
			Timestamp: start.Add(time.Duration(frac * float64(span))),
			Service:   "order-service",
			Level:     "ERROR",
			Message:   "transient upstream error",
			IsError:   true,
		})
	}

	// Error spike: 15 order-service errors in the last ~20% of the window.
	spikeMessages := []string{
		"connection pool exhausted",
		"timeout waiting for connection from postgres-order",
		"request to postgres-order failed: connection timeout",
	}
	spikeStart := end.Add(-time.Duration(0.20 * float64(span)))
	spikeSpan := end.Sub(spikeStart)
	const spikeCount = 15
	for i := 0; i < spikeCount; i++ {
		ts := spikeStart.Add(time.Duration(float64(i) / float64(spikeCount) * float64(spikeSpan)))
		logs = append(logs, models.LogEntry{
			Timestamp: ts,
			Service:   "order-service",
			Level:     "ERROR",
			Message:   spikeMessages[i%len(spikeMessages)],
			IsError:   true,
		})
	}

	// Supporting signal: postgres-order saturation warning near the spike.
	logs = append(logs, models.LogEntry{
		Timestamp: end.Add(-time.Duration(0.10 * float64(span))),
		Service:   "postgres-order",
		Level:     "WARN",
		Message:   "active connections: 200/200",
		IsError:   false,
	})

	return logs, nil
}

// QueryErrorsByService retrieves mock error logs for a specific service.
func (m *MockLogSource) QueryErrorsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	allLogs, err := m.QueryErrors(ctx, start, end)
	if err != nil {
		return nil, err
	}

	var filtered []models.LogEntry
	for _, entry := range allLogs {
		if entry.Service == service {
			filtered = append(filtered, entry)
		}
	}

	return filtered, nil
}

// QueryLogsByService retrieves mock logs for a specific service.
func (m *MockLogSource) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	allLogs, err := m.QueryErrors(ctx, start, end)
	if err != nil {
		return nil, err
	}

	var filtered []models.LogEntry
	for _, log := range allLogs {
		if log.Service == service {
			filtered = append(filtered, log)
		}
	}

	return filtered, nil
}

// GetLogCount returns the count of mock logs for a service.
func (m *MockLogSource) GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error) {
	logs, err := m.QueryLogsByService(ctx, service, start, end)
	if err != nil {
		return 0, err
	}
	return int64(len(logs)), nil
}

// MockMetricSource provides a mock implementation of MetricSource.
type MockMetricSource struct{}

// NewMockMetricSource creates a new mock metric source.
func NewMockMetricSource() *MockMetricSource {
	return &MockMetricSource{}
}

// QueryAnomalies retrieves mock metric anomalies within the time window.
//
// Datapoints are spread across the requested [start, end] window with values
// that stay flat through the baseline portion and ramp up sharply in the last
// ~30% (the incident window), so calculateMetricTrends reports TrendUp in the
// final sub-windows and hasAnomalousTrend fires regardless of the absolute
// incident time.
func (m *MockMetricSource) QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
	span := end.Sub(start)
	if span <= 0 {
		span = 20 * time.Minute
		start = end.Add(-span)
	}

	// ramp returns a 0..1 multiplier: 0 in the baseline, rising to 1 at end.
	ramp := func(frac float64) float64 {
		const kneeAt = 0.70
		if frac <= kneeAt {
			return 0
		}
		return (frac - kneeAt) / (1 - kneeAt)
	}

	// Build each series as N points across the window.
	const n = 12
	build := func(base, peak float64) []models.DataPoint {
		pts := make([]models.DataPoint, 0, n)
		for i := 0; i <= n; i++ {
			frac := float64(i) / float64(n)
			ts := start.Add(time.Duration(frac * float64(span)))
			val := base + ramp(frac)*(peak-base)
			pts = append(pts, models.DataPoint{Timestamp: ts, Value: val})
		}
		return pts
	}

	series := []models.MetricSeries{
		{
			Name:       "order_service_error_rate",
			Labels:     map[string]string{"service": "order-service"},
			Unit:       "rate",
			DataPoints: build(0.001, 0.025),
		},
		{
			Name:       "postgres_order_connections",
			Labels:     map[string]string{"service": "postgres-order"},
			Unit:       "count",
			DataPoints: build(150, 200),
		},
		{
			Name:       "order_service_latency_p99",
			Labels:     map[string]string{"service": "order-service"},
			Unit:       "seconds",
			DataPoints: build(0.120, 0.480),
		},
	}

	return series, nil
}

// QueryMetric retrieves a specific metric by name.
func (m *MockMetricSource) QueryMetric(ctx context.Context, metricName string, start, end time.Time, filters map[string]string) (*models.MetricSeries, error) {
	allSeries, err := m.QueryAnomalies(ctx, start, end)
	if err != nil {
		return nil, err
	}

	for _, series := range allSeries {
		if series.Name == metricName {
			return &series, nil
		}
	}

	return nil, fmt.Errorf("metric not found: %s", metricName)
}

// QueryMetricsForService retrieves all metrics for a specific service.
func (m *MockMetricSource) QueryMetricsForService(ctx context.Context, service string, start, end time.Time) ([]models.MetricSeries, error) {
	allSeries, err := m.QueryAnomalies(ctx, start, end)
	if err != nil {
		return nil, err
	}

	var filtered []models.MetricSeries
	for _, series := range allSeries {
		if series.Labels["service"] == service {
			filtered = append(filtered, series)
		}
	}

	return filtered, nil
}

// GetAlerts returns current active alerts (mock).
func (m *MockMetricSource) GetAlerts(ctx context.Context) ([]Alert, error) {
	return []Alert{}, nil
}

// QueryMetricRange returns the same fabricated series as QueryAnomalies.
// TEST-ONLY: it does not contact a real backend and ignores the query/step.
func (m *MockMetricSource) QueryMetricRange(ctx context.Context, query string, start, end time.Time, step string) ([]models.MetricSeries, error) {
	return m.QueryAnomalies(ctx, start, end)
}

// MockChangeSource provides a mock implementation of ChangeSource.
type MockChangeSource struct{}

// NewMockChangeSource creates a new mock change source.
func NewMockChangeSource() *MockChangeSource {
	return &MockChangeSource{}
}

// QueryChanges retrieves mock changes within the time window.
//
// The deployment is placed just before the incident window begins (the engine
// queries [incident.StartTime-windowSize, incident.EndTime], so positioning the
// change at ~75% of the query span lands it near incident start). This keeps
// isChangeRelevant true and gives scoreChangeProximity a high proximity score
// for any incident time.
func (m *MockChangeSource) QueryChanges(ctx context.Context, start, end time.Time) ([]models.Change, error) {
	span := end.Sub(start)
	if span <= 0 {
		span = 20 * time.Minute
		start = end.Add(-span)
	}

	changes := []models.Change{
		{
			ID:          "deploy-001",
			Timestamp:   start.Add(time.Duration(0.72 * float64(span))),
			Service:     "order-service",
			Type:        "deployment",
			Description: "order-service deployment: replica count scaled from 5 to 8",
			Details: map[string]string{
				"commit":   "abc123def456",
				"image":    "order-service:v2.3.1",
				"replicas": "8",
			},
		},
	}

	return changes, nil
}

// MockTopologySource provides a mock implementation of TopologySource.
type MockTopologySource struct{}

// NewMockTopologySource creates a new mock topology source.
func NewMockTopologySource() *MockTopologySource {
	return &MockTopologySource{}
}

// GetTopology retrieves a mock service topology.
func (m *MockTopologySource) GetTopology(ctx context.Context) (*models.Topology, error) {
	topology := &models.Topology{
		Nodes: []models.ServiceNode{
			{Name: "api-gateway", Type: "application", Labels: map[string]string{"tier": "gateway"}},
			{Name: "order-service", Type: "application", Labels: map[string]string{"tier": "backend"}},
			{Name: "payment-service", Type: "application", Labels: map[string]string{"tier": "backend"}},
			{Name: "user-service", Type: "application", Labels: map[string]string{"tier": "backend"}},
			{Name: "postgres-order", Type: "database", Labels: map[string]string{"type": "postgresql"}},
			{Name: "postgres-payment", Type: "database", Labels: map[string]string{"type": "postgresql"}},
			{Name: "redis-cache", Type: "cache", Labels: map[string]string{"type": "redis"}},
		},
		Edges: []models.DependencyEdge{
			{From: "api-gateway", To: "order-service", Type: "http"},
			{From: "api-gateway", To: "payment-service", Type: "http"},
			{From: "api-gateway", To: "user-service", Type: "http"},
			{From: "order-service", To: "postgres-order", Type: "database"},
			{From: "order-service", To: "redis-cache", Type: "cache"},
			{From: "payment-service", To: "postgres-payment", Type: "database"},
			{From: "user-service", To: "redis-cache", Type: "cache"},
		},
	}

	return topology, nil
}

// GetServiceNeighbors returns the dependents and dependencies of a service.
func (m *MockTopologySource) GetServiceNeighbors(ctx context.Context, service string) (dependents, dependencies []string, err error) {
	topo, err := m.GetTopology(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Find dependencies (services this service depends on)
	for _, edge := range topo.Edges {
		if edge.From == service {
			dependencies = append(dependencies, edge.To)
		}
	}

	// Find dependents (services that depend on this service)
	for _, edge := range topo.Edges {
		if edge.To == service {
			dependents = append(dependents, edge.From)
		}
	}

	return dependents, dependencies, nil
}

// GetServiceHealth returns mock health status for all services.
func (m *MockTopologySource) GetServiceHealth(ctx context.Context) (map[string]string, error) {
	return map[string]string{
		"api-gateway":      "healthy",
		"order-service":    "degraded",
		"payment-service":  "healthy",
		"user-service":     "healthy",
		"postgres-order":   "degraded",
		"postgres-payment": "healthy",
		"redis-cache":      "healthy",
	}, nil
}

// MockTraceSource provides a mock implementation of TraceSource.
type MockTraceSource struct{}

// NewMockTraceSource creates a new mock trace source.
func NewMockTraceSource() *MockTraceSource {
	return &MockTraceSource{}
}

// QueryErrorTraces retrieves mock error traces within the time window.
//
// The engine queries traces for [incident.StartTime, incident.EndTime], so the
// error traces are positioned inside that window (early and mid) to ensure they
// bucket into the incident sub-windows and feed scoreTraceEvidence.
func (m *MockTraceSource) QueryErrorTraces(ctx context.Context, start, end time.Time) ([]models.TraceSummary, error) {
	span := end.Sub(start)
	if span <= 0 {
		span = 6 * time.Minute
		start = end.Add(-span)
	}

	traces := []models.TraceSummary{
		{
			TraceID:       "trace-001",
			StartTime:     start.Add(time.Duration(0.20 * float64(span))),
			Duration:      450 * time.Millisecond,
			ServiceName:   "order-service",
			OperationName: "POST /api/orders/create",
			HasError:      true,
			SpanCount:     4,
		},
		{
			TraceID:       "trace-002",
			StartTime:     start.Add(time.Duration(0.55 * float64(span))),
			Duration:      520 * time.Millisecond,
			ServiceName:   "order-service",
			OperationName: "POST /api/orders/create",
			HasError:      true,
			SpanCount:     5,
		},
	}

	return traces, nil
}

// QueryChangesByService retrieves changes for a specific service.
func (m *MockChangeSource) QueryChangesByService(ctx context.Context, service string, start, end time.Time) ([]models.Change, error) {
	allChanges, err := m.QueryChanges(ctx, start, end)
	if err != nil {
		return nil, err
	}

	var filtered []models.Change
	for _, change := range allChanges {
		if change.Service == service {
			filtered = append(filtered, change)
		}
	}

	return filtered, nil
}

// GetLastChange retrieves the most recent change for a service.
func (m *MockChangeSource) GetLastChange(ctx context.Context, service string) (*models.Change, error) {
	now := time.Now()
	changes, err := m.QueryChangesByService(ctx, service, now.Add(-24*time.Hour), now)
	if err != nil {
		return nil, err
	}

	if len(changes) == 0 {
		return nil, fmt.Errorf("no changes found for service: %s", service)
	}

	// Return the most recent change
	return &changes[0], nil
}

// QueryTracesByService retrieves traces for a specific service.
func (m *MockTraceSource) QueryTracesByService(ctx context.Context, service string, start, end time.Time) ([]models.TraceSummary, error) {
	allTraces, err := m.QueryErrorTraces(ctx, start, end)
	if err != nil {
		return nil, err
	}

	var filtered []models.TraceSummary
	for _, trace := range allTraces {
		if trace.ServiceName == service {
			filtered = append(filtered, trace)
		}
	}

	return filtered, nil
}

// GetTraceDetails retrieves detailed information about a specific trace.
func (m *MockTraceSource) GetTraceDetails(ctx context.Context, traceID string) (*models.TraceSummary, error) {
	now := time.Now()
	// Return a mock trace detail
	trace := &models.TraceSummary{
		TraceID:      traceID,
		StartTime:    now.Add(-5 * time.Minute),
		Duration:     520 * time.Millisecond,
		ServiceName:  "order-service",
		OperationName: "POST /api/orders/create",
		HasError:     true,
		SpanCount:    5,
	}

	return trace, nil
}

// QuerySlowTraces retrieves traces that exceed a minimum duration.
func (m *MockTraceSource) QuerySlowTraces(ctx context.Context, start, end time.Time, minDuration time.Duration) ([]models.TraceSummary, error) {
	allTraces, err := m.QueryErrorTraces(ctx, start, end)
	if err != nil {
		return nil, err
	}

	var filtered []models.TraceSummary
	for _, trace := range allTraces {
		if trace.Duration >= minDuration {
			filtered = append(filtered, trace)
		}
	}

	return filtered, nil
}

// Verify interfaces are implemented (against the actual interfaces in their respective files)
var _ LogSource = (*MockLogSource)(nil)
var _ MetricSource = (*MockMetricSource)(nil)
var _ TraceSource = (*MockTraceSource)(nil)
var _ TopologySource = (*MockTopologySource)(nil)
var _ ChangeSource = (*MockChangeSource)(nil)
