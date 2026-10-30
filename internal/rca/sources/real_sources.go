// Package sources provides real data source implementations for RCA analysis.
package sources

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/datasource/core"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// DSLogSource implements LogSource using a LogDataSource.
//
// It expresses query INTENT (errors-only, by-service) and lets the underlying
// cloud data source translate that intent into its own dialect. It no longer
// builds any provider-specific query string — that was the SLS-dialect leak
// this design removes, so the same source works over SLS, Volcengine TLS, etc.
type DSLogSource struct {
	logDS core.LogDataSource
}

// NewDSLogSource creates a new LogSource backed by a LogDataSource.
func NewDSLogSource(logDS core.LogDataSource) *DSLogSource {
	return &DSLogSource{
		logDS: logDS,
	}
}

// QueryErrors retrieves error logs within the time window.
func (s *DSLogSource) QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
	req := &core.LogQueryRequest{
		StartTime: start,
		EndTime:   end,
		Intent:    &core.QueryIntent{ErrorsOnly: true},
		Limit:     1000,
	}

	resp, err := s.logDS.QueryLogs(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query error logs: %w", err)
	}

	return s.convertToModelLogs(resp.Logs), nil
}

// QueryErrorsByService retrieves error logs for a specific service.
func (s *DSLogSource) QueryErrorsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	req := &core.LogQueryRequest{
		StartTime: start,
		EndTime:   end,
		Intent:    &core.QueryIntent{ErrorsOnly: true, ServiceName: service},
		Limit:     1000,
	}

	resp, err := s.logDS.QueryLogs(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query error logs for service %s: %w", service, err)
	}

	return s.convertToModelLogs(resp.Logs), nil
}

// QueryLogsByService retrieves logs for a specific service.
func (s *DSLogSource) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	req := &core.LogQueryRequest{
		StartTime: start,
		EndTime:   end,
		Intent:    &core.QueryIntent{ServiceName: service},
		Limit:     1000,
	}

	resp, err := s.logDS.QueryLogs(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query logs for service %s: %w", service, err)
	}

	return s.convertToModelLogs(resp.Logs), nil
}

// GetLogCount returns the count of logs for a service.
func (s *DSLogSource) GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error) {
	req := &core.LogQueryRequest{
		StartTime: start,
		EndTime:   end,
		Intent:    &core.QueryIntent{ServiceName: service},
	}

	count, err := s.logDS.GetLogCount(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("failed to get log count for service %s: %w", service, err)
	}

	return count, nil
}

// convertToModelLogs converts core log entries to RCA model log entries.
func (s *DSLogSource) convertToModelLogs(logs []core.LogEntry) []models.LogEntry {
	result := make([]models.LogEntry, 0, len(logs))

	for _, log := range logs {
		entry := models.LogEntry{
			Timestamp: log.Timestamp,
			Message:   log.Message,
			TraceID:   log.TraceID,
			SpanID:    log.SpanID,
		}

		// Extract level from fields or message
		if log.Level != "" {
			entry.Level = log.Level
		} else if level, ok := log.Fields["level"].(string); ok {
			entry.Level = level
		}

		// Extract service name
		if log.Source != "" {
			entry.Service = log.Source
		} else if service, ok := log.Fields["service"].(string); ok {
			entry.Service = service
		} else if service, ok := log.Fields["app_name"].(string); ok {
			entry.Service = service
		}

		// Check if this is an error log
		entry.IsError = strings.ToUpper(entry.Level) == "ERROR" ||
			strings.ToUpper(entry.Level) == "ERR" ||
			strings.Contains(strings.ToLower(entry.Message), "error") ||
			strings.Contains(strings.ToLower(entry.Message), "failed")

		// Copy labels
		entry.Labels = make(map[string]string)
		for k, v := range log.Fields {
			if vs, ok := v.(string); ok {
				entry.Labels[k] = vs
			}
		}

		result = append(result, entry)
	}

	return result
}

// DSMetricSource implements MetricSource using a MetricsDataSource.
type DSMetricSource struct {
	metricDS core.MetricsDataSource
}

// NewDSMetricSource creates a new MetricSource backed by a MetricsDataSource.
func NewDSMetricSource(metricDS core.MetricsDataSource) *DSMetricSource {
	return &DSMetricSource{
		metricDS: metricDS,
	}
}

// QueryAnomalies retrieves metrics with anomalies within the time window.
// This queries all available metrics for the time range.
func (s *DSMetricSource) QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
	// Query common error rate and latency metrics
	queries := []string{
		"rate(http_requests_total{status=~\"5..\"}[5m])",
		"rate(http_requests_total{status=~\"4..\"}[5m])",
		"histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))",
		"histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))",
	}

	var allSeries []models.MetricSeries

	for _, query := range queries {
		series, err := s.queryByPromQL(ctx, query, start, end)
		if err != nil {
			// Log but continue with other queries
			continue
		}
		allSeries = append(allSeries, series...)
	}

	return allSeries, nil
}

// QueryMetric retrieves a specific metric by name using PromQL.
func (s *DSMetricSource) QueryMetric(ctx context.Context, metricName string, start, end time.Time, filters map[string]string) (*models.MetricSeries, error) {
	// Build PromQL query from metric name and filters
	query := metricName
	if len(filters) > 0 {
		filterParts := make([]string, 0, len(filters))
		for k, v := range filters {
			filterParts = append(filterParts, fmt.Sprintf("%s=\"%s\"", k, v))
		}
		query = fmt.Sprintf("%s{%s}", metricName, strings.Join(filterParts, ","))
	}

	series, err := s.queryByPromQL(ctx, query, start, end)
	if err != nil {
		return nil, err
	}

	if len(series) == 0 {
		return nil, fmt.Errorf("metric not found: %s", metricName)
	}

	// Return the first series
	return &series[0], nil
}

// QueryMetricsForService retrieves all metrics for a specific service.
func (s *DSMetricSource) QueryMetricsForService(ctx context.Context, service string, start, end time.Time) ([]models.MetricSeries, error) {
	// Build query to filter by service label
	query := fmt.Sprintf("{service=\"%s\"}", service)

	return s.queryByPromQL(ctx, query, start, end)
}

// GetAlerts returns current active alerts from Prometheus.
func (s *DSMetricSource) GetAlerts(ctx context.Context) ([]Alert, error) {
	// For Prometheus, alerts are available via /api/v1/alerts
	// This would require extending the MetricsDataSource interface
	// For now, return empty slice
	return []Alert{}, nil
}

// queryByPromQL executes a PromQL query and returns model metric series.
func (s *DSMetricSource) queryByPromQL(ctx context.Context, query string, start, end time.Time) ([]models.MetricSeries, error) {
	req := &core.MetricsQueryRequest{
		StartTime: start,
		EndTime:   end,
		Query:     query,
		Step:      time.Minute, // 1-minute resolution
	}

	resp, err := s.metricDS.QueryMetrics(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query metrics: %w", err)
	}

	// Convert to model format
	result := make([]models.MetricSeries, 0, len(resp.Series))
	for _, series := range resp.Series {
		name := s.extractMetricName(query)
		modelSeries := models.MetricSeries{
			Name:   name,
			Labels: series.Labels,
			Unit:   "unknown", // Unit not provided by Prometheus
			DataPoints: make([]models.DataPoint, 0, len(series.DataPoints)),
		}

		for _, dp := range series.DataPoints {
			modelSeries.DataPoints = append(modelSeries.DataPoints, models.DataPoint{
				Timestamp: dp.Timestamp,
				Value:     dp.Value,
			})
		}

		result = append(result, modelSeries)
	}

	return result, nil
}

// extractMetricName extracts the metric name from a PromQL query.
func (s *DSMetricSource) extractMetricName(query string) string {
	// Simple extraction: remove label selectors and functions
	query = strings.TrimSpace(query)

	// Remove label selectors {...}
	if idx := strings.Index(query, "{"); idx >= 0 && strings.HasSuffix(query, "}") {
		return query[:idx]
	}

	// Remove function calls
	if idx := strings.Index(query, "("); idx >= 0 {
		// Find the first identifier after the opening parenthesis
		rest := query[idx+1:]
		if idx2 := strings.Index(rest, "{"); idx2 >= 0 {
			return rest[:idx2]
		}
		return rest
	}

	return query
}

// QueryMetricRange executes an arbitrary range query and returns every
// matching series, honouring the requested step. It is the multi-series
// counterpart to QueryMetric, used by the MCP metrics tool.
func (s *DSMetricSource) QueryMetricRange(ctx context.Context, query string, start, end time.Time, step string) ([]models.MetricSeries, error) {
	req := &core.MetricsQueryRequest{
		StartTime: start,
		EndTime:   end,
		Query:     query,
		Step:      parseStepDuration(step),
	}

	resp, err := s.metricDS.QueryMetrics(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query metrics: %w", err)
	}

	result := make([]models.MetricSeries, 0, len(resp.Series))
	for _, series := range resp.Series {
		modelSeries := models.MetricSeries{
			Name:       s.extractMetricName(query),
			Labels:     series.Labels,
			Unit:       "unknown",
			DataPoints: make([]models.DataPoint, 0, len(series.DataPoints)),
		}

		for _, dp := range series.DataPoints {
			modelSeries.DataPoints = append(modelSeries.DataPoints, models.DataPoint{
				Timestamp: dp.Timestamp,
				Value:     dp.Value,
			})
		}

		result = append(result, modelSeries)
	}

	return result, nil
}

// parseStepDuration converts a Prometheus-style step string into a duration.
// An empty or unparseable value falls back to one minute, matching the MCP
// tool's default step.
func parseStepDuration(step string) time.Duration {
	step = strings.TrimSpace(step)
	if step == "" {
		return time.Minute
	}
	if d, err := time.ParseDuration(step); err == nil && d > 0 {
		return d
	}
	return time.Minute
}

// DSTraceSource implements TraceSource using an APMDataSource.
type DSTraceSource struct {
	apmDS core.APMDataSource
}

// NewDSTraceSource creates a new TraceSource backed by an APMDataSource.
func NewDSTraceSource(apmDS core.APMDataSource) *DSTraceSource {
	return &DSTraceSource{
		apmDS: apmDS,
	}
}

// QueryErrorTraces retrieves error traces within the time window.
func (s *DSTraceSource) QueryErrorTraces(ctx context.Context, start, end time.Time) ([]models.TraceSummary, error) {
	req := &core.TraceQueryRequest{
		StartTime: start,
		EndTime:   end,
		// Filter for error traces - this may need APM-specific query syntax
		Limit: 100,
	}

	resp, err := s.apmDS.QueryTraces(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query error traces: %w", err)
	}

	// Filter for traces with errors
	result := make([]models.TraceSummary, 0)
	for _, trace := range resp.Traces {
		if trace.HasError {
			result = append(result, s.convertTraceSummary(trace))
		}
	}

	return result, nil
}

// QueryTracesByService retrieves traces for a specific service.
func (s *DSTraceSource) QueryTracesByService(ctx context.Context, service string, start, end time.Time) ([]models.TraceSummary, error) {
	req := &core.TraceQueryRequest{
		StartTime:   start,
		EndTime:     end,
		ServiceName: service,
		Limit:       100,
	}

	resp, err := s.apmDS.QueryTraces(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query traces for service %s: %w", service, err)
	}

	result := make([]models.TraceSummary, len(resp.Traces))
	for i, trace := range resp.Traces {
		result[i] = s.convertTraceSummary(trace)
	}

	return result, nil
}

// GetTraceDetails retrieves detailed information about a specific trace.
func (s *DSTraceSource) GetTraceDetails(ctx context.Context, traceID string) (*models.TraceSummary, error) {
	details, err := s.apmDS.GetTraceDetails(ctx, traceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get trace details: %w", err)
	}

	summary := s.convertTraceSummary(details.TraceSummary)
	summary.Spans = make([]models.Span, len(details.Spans))

	for i, span := range details.Spans {
		summary.Spans[i] = models.Span{
			SpanID:        span.SpanID,
			ParentSpanID:  span.ParentSpanID,
			ServiceName:   span.ServiceName,
			OperationName: span.OperationName,
			StartTime:     span.StartTime,
			Duration:      span.Duration,
			HasError:      span.HasError,
			Tags:          span.Tags,
		}
	}

	return &summary, nil
}

// QuerySlowTraces retrieves traces that exceed a minimum duration.
func (s *DSTraceSource) QuerySlowTraces(ctx context.Context, start, end time.Time, minDuration time.Duration) ([]models.TraceSummary, error) {
	req := &core.TraceQueryRequest{
		StartTime:    start,
		EndTime:      end,
		MinDuration:  minDuration,
		Limit:        100,
	}

	resp, err := s.apmDS.QueryTraces(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query slow traces: %w", err)
	}

	result := make([]models.TraceSummary, len(resp.Traces))
	for i, trace := range resp.Traces {
		result[i] = s.convertTraceSummary(trace)
	}

	return result, nil
}

// convertTraceSummary converts a core trace summary to a model trace summary.
func (s *DSTraceSource) convertTraceSummary(trace core.TraceSummary) models.TraceSummary {
	return models.TraceSummary{
		TraceID:      trace.TraceID,
		StartTime:    trace.StartTime,
		Duration:     trace.Duration,
		ServiceName:  trace.ServiceName,
		OperationName: trace.OperationName,
		HasError:     trace.HasError,
		SpanCount:    trace.SpanCount,
	}
}

// CDChangeSource implements ChangeSource using a CICDCollector.
type CDChangeSource struct {
	// analyzer can be any type that implements change collection
	// We use interface{} to avoid import cycles
	analyzer interface{}
	getChangesFunc func(ctx context.Context, service string, start, end time.Time) ([]models.Change, error)
}

// NewCDChangeSource creates a new ChangeSource backed by change detection.
func NewCDChangeSource(
	getChangesFunc func(ctx context.Context, service string, start, end time.Time) ([]models.Change, error),
) *CDChangeSource {
	return &CDChangeSource{
		getChangesFunc: getChangesFunc,
	}
}

// QueryChanges retrieves changes within the time window.
func (s *CDChangeSource) QueryChanges(ctx context.Context, start, end time.Time) ([]models.Change, error) {
	// Use empty service to get all changes
	if s.getChangesFunc != nil {
		return s.getChangesFunc(ctx, "", start, end)
	}
	return []models.Change{}, nil
}

// QueryChangesByService retrieves changes for a specific service.
func (s *CDChangeSource) QueryChangesByService(ctx context.Context, service string, start, end time.Time) ([]models.Change, error) {
	if s.getChangesFunc != nil {
		return s.getChangesFunc(ctx, service, start, end)
	}
	return []models.Change{}, nil
}

// GetLastChange retrieves the most recent change for a service.
func (s *CDChangeSource) GetLastChange(ctx context.Context, service string) (*models.Change, error) {
	if s.getChangesFunc == nil {
		return nil, fmt.Errorf("no changes found for service: %s", service)
	}

	// Get changes from the last 24 hours
	end := time.Now()
	start := end.Add(-24 * time.Hour)

	changes, err := s.getChangesFunc(ctx, service, start, end)
	if err != nil {
		return nil, err
	}

	if len(changes) == 0 {
		return nil, fmt.Errorf("no changes found for service: %s", service)
	}

	// Return the most recent change
	return &changes[0], nil
}

// AR MSTopologySource implements TopologySource using an ARMS client.
type ARMASTopologySource struct {
	getTopologyFunc func(ctx context.Context) (*models.Topology, error)
	getHealthFunc   func(ctx context.Context) (map[string]string, error)
}

// NewARMASTopologySource creates a new TopologySource backed by ARMS.
func NewARMASTopologySource(
	getTopology func(ctx context.Context) (*models.Topology, error),
	getHealth func(ctx context.Context) (map[string]string, error),
) *ARMASTopologySource {
	return &ARMASTopologySource{
		getTopologyFunc: getTopology,
		getHealthFunc:   getHealth,
	}
}

// GetTopology retrieves the service topology.
func (s *ARMASTopologySource) GetTopology(ctx context.Context) (*models.Topology, error) {
	if s.getTopologyFunc != nil {
		return s.getTopologyFunc(ctx)
	}
	return nil, fmt.Errorf("topology function not configured")
}

// GetServiceNeighbors returns the dependents and dependencies of a service.
func (s *ARMASTopologySource) GetServiceNeighbors(ctx context.Context, service string) (dependents, dependencies []string, err error) {
	topo, err := s.GetTopology(ctx)
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

// GetServiceHealth returns health status for all services.
func (s *ARMASTopologySource) GetServiceHealth(ctx context.Context) (map[string]string, error) {
	if s.getHealthFunc != nil {
		return s.getHealthFunc(ctx)
	}
	return map[string]string{}, nil
}

// Verify interfaces are implemented
var _ LogSource = (*DSLogSource)(nil)
var _ MetricSource = (*DSMetricSource)(nil)
var _ TraceSource = (*DSTraceSource)(nil)
var _ ChangeSource = (*CDChangeSource)(nil)
var _ TopologySource = (*ARMASTopologySource)(nil)
