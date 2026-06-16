// Package core provides core abstractions for data sources.
package core

import (
	"context"
	"time"
)

// DataSourceType represents the type of data source.
type DataSourceType string

const (
	// DataSourceTypeSLS represents Alibaba Cloud SLS
	DataSourceTypeSLS DataSourceType = "aliyun_sls"
	// DataSourceTypeTLS represents Volcengine TLS
	DataSourceTypeTLS DataSourceType = "volcengine_tls"
	// DataSourceTypeLTS represents Huawei Cloud LTS
	DataSourceTypeLTS DataSourceType = "huawei_lts"
	// DataSourceTypeARMS represents Alibaba Cloud ARMS
	DataSourceTypeARMS DataSourceType = "aliyun_arms"
	// DataSourceTypePrometheus represents Prometheus
	DataSourceTypePrometheus DataSourceType = "prometheus"
	// DataSourceTypeInfluxDB represents InfluxDB
	DataSourceTypeInfluxDB DataSourceType = "influxdb"
	// DataSourceTypeElasticsearch represents Elasticsearch
	DataSourceTypeElasticsearch DataSourceType = "elasticsearch"
)

// HealthStatus represents the health status of a data source.
type HealthStatus string

const (
	// StatusHealthy represents a healthy data source
	StatusHealthy HealthStatus = "healthy"
	// StatusDegraded represents a degraded data source (slow but functional)
	StatusDegraded HealthStatus = "degraded"
	// StatusUnhealthy represents an unhealthy data source
	StatusUnhealthy HealthStatus = "unhealthy"
	// StatusUnknown represents an unknown health status
	StatusUnknown HealthStatus = "unknown"
)

// DataSource is the base interface for all data sources.
type DataSource interface {
	// Connect establishes a connection to the data source.
	Connect(ctx context.Context) error

	// Close closes the connection to the data source.
	Close() error

	// Ping checks if the data source is reachable.
	Ping(ctx context.Context) error

	// Health returns the detailed health status of the data source.
	Health(ctx context.Context) (*HealthInfo, error)

	// Type returns the type of the data source.
	Type() DataSourceType

	// Name returns the name/identifier of the data source instance.
	Name() string
}

// LogDataSource is the interface for log data sources.
type LogDataSource interface {
	DataSource

	// QueryLogs queries logs from the data source.
	QueryLogs(ctx context.Context, req *LogQueryRequest) (*LogQueryResponse, error)

	// GetLogCount returns the count of logs matching the query.
	GetLogCount(ctx context.Context, req *LogQueryRequest) (int64, error)
}

// MetricsDataSource is the interface for metrics data sources.
type MetricsDataSource interface {
	DataSource

	// QueryMetrics queries metrics from the data source.
	QueryMetrics(ctx context.Context, req *MetricsQueryRequest) (*MetricsQueryResponse, error)
}

// APMDataSource is the interface for APM data sources.
type APMDataSource interface {
	DataSource

	// QueryTraces queries traces from the APM data source.
	QueryTraces(ctx context.Context, req *TraceQueryRequest) (*TraceQueryResponse, error)

	// QueryMetrics queries APM-specific metrics.
	QueryMetrics(ctx context.Context, req *APMMetricsRequest) (*APMMetricsResponse, error)

	// GetTraceDetails retrieves details for a specific trace.
	GetTraceDetails(ctx context.Context, traceID string) (*TraceDetails, error)
}

// HealthInfo contains detailed health information about a data source.
type HealthInfo struct {
	Status      HealthStatus `json:"status"`
	Message     string       `json:"message"`
	Latency     time.Duration `json:"latency_ms"`
	CheckedAt   time.Time    `json:"checked_at"`
	Details     map[string]interface{} `json:"details,omitempty"`
}

// LogQueryRequest represents a request to query logs.
type LogQueryRequest struct {
	// StartTime is the start time of the query range.
	StartTime time.Time `json:"start_time"`
	// EndTime is the end time of the query range.
	EndTime time.Time `json:"end_time"`
	// Query is the query string or filter.
	Query string `json:"query"`
	// Limit is the maximum number of logs to return.
	Limit int `json:"limit,omitempty"`
	// Offset is the offset for pagination.
	Offset int `json:"offset,omitempty"`
	// Fields specifies which fields to return.
	Fields []string `json:"fields,omitempty"`
	// Sort specifies the sort order (asc/desc).
	Sort string `json:"sort,omitempty"`
	// Filter contains additional filter parameters.
	Filter map[string]string `json:"filter,omitempty"`
}

// LogQueryResponse represents the response from a log query.
type LogQueryResponse struct {
	// Logs contains the queried log entries.
	Logs []LogEntry `json:"logs"`
	// TotalCount is the total number of logs matching the query.
	TotalCount int64 `json:"total_count"`
	// HasMore indicates if there are more logs available.
	HasMore bool `json:"has_more"`
	// NextOffset can be used for pagination.
	NextOffset *string `json:"next_offset,omitempty"`
	// Aggregations contains any aggregation results.
	Aggregations map[string]interface{} `json:"aggregations,omitempty"`
}

// LogEntry represents a single log entry.
type LogEntry struct {
	// Timestamp is when the log was generated.
	Timestamp time.Time `json:"timestamp"`
	// Level is the log level (DEBUG, INFO, WARN, ERROR).
	Level string `json:"level,omitempty"`
	// Message is the log message.
	Message string `json:"message"`
	// Source is the source of the log (service, host, etc.).
	Source string `json:"source,omitempty"`
	// Fields contains additional structured fields.
	Fields map[string]interface{} `json:"fields,omitempty"`
	// TraceID is the trace ID if the log is part of a trace.
	TraceID string `json:"trace_id,omitempty"`
	// SpanID is the span ID if the log is part of a trace.
	SpanID string `json:"span_id,omitempty"`
}

// MetricsQueryRequest represents a request to query metrics.
type MetricsQueryRequest struct {
	// StartTime is the start time of the query range.
	StartTime time.Time `json:"start_time"`
	// EndTime is the end time of the query range.
	EndTime time.Time `json:"end_time"`
	// Query is the query string (e.g., PromQL).
	Query string `json:"query"`
	// Step is the step between data points.
	Step time.Duration `json:"step,omitempty"`
}

// MetricsQueryResponse represents the response from a metrics query.
type MetricsQueryResponse struct {
	// Series contains the queried time series.
	Series []MetricSeries `json:"series"`
}

// MetricSeries represents a single time series.
type MetricSeries struct {
	// Labels are the labels for this series.
	Labels map[string]string `json:"labels"`
	// DataPoints contains the data points.
	DataPoints []DataPoint `json:"data_points"`
}

// DataPoint represents a single data point.
type DataPoint struct {
	// Timestamp is when the data point was recorded.
	Timestamp time.Time `json:"timestamp"`
	// Value is the value of the data point.
	Value float64 `json:"value"`
}

// TraceQueryRequest represents a request to query traces.
type TraceQueryRequest struct {
	// StartTime is the start time of the query range.
	StartTime time.Time `json:"start_time"`
	// EndTime is the end time of the query range.
	EndTime time.Time `json:"end_time"`
	// TraceID is the specific trace ID to query (optional).
	TraceID string `json:"trace_id,omitempty"`
	// ServiceName filters by service name.
	ServiceName string `json:"service_name,omitempty"`
	// MinDuration filters traces with duration greater than this.
	MinDuration time.Duration `json:"min_duration,omitempty"`
	// MaxDuration filters traces with duration less than this.
	MaxDuration time.Duration `json:"max_duration,omitempty"`
	// Limit is the maximum number of traces to return.
	Limit int `json:"limit,omitempty"`
	// Filter contains additional filter parameters.
	Filter map[string]string `json:"filter,omitempty"`
}

// TraceQueryResponse represents the response from a trace query.
type TraceQueryResponse struct {
	// Traces contains the queried traces.
	Traces []TraceSummary `json:"traces"`
	// TotalCount is the total number of traces matching the query.
	TotalCount int64 `json:"total_count"`
}

// TraceSummary represents a summary of a trace.
type TraceSummary struct {
	// TraceID is the unique identifier for the trace.
	TraceID string `json:"trace_id"`
	// ServiceName is the name of the service.
	ServiceName string `json:"service_name"`
	// OperationName is the name of the operation.
	OperationName string `json:"operation_name"`
	// StartTime is when the trace started.
	StartTime time.Time `json:"start_time"`
	// Duration is the duration of the trace.
	Duration time.Duration `json:"duration"`
	// HasError indicates if the trace contains errors.
	HasError bool `json:"has_error"`
	// SpanCount is the number of spans in the trace.
	SpanCount int `json:"span_count"`
}

// TraceDetails represents detailed information about a trace.
type TraceDetails struct {
	// TraceSummary contains the summary information.
	TraceSummary
	// Spans contains all the spans in the trace.
	Spans []Span `json:"spans"`
}

// Span represents a single span in a trace.
type Span struct {
	// SpanID is the unique identifier for the span.
	SpanID string `json:"span_id"`
	// ParentSpanID is the ID of the parent span (if any).
	ParentSpanID string `json:"parent_span_id,omitempty"`
	// OperationName is the name of the operation.
	OperationName string `json:"operation_name"`
	// ServiceName is the name of the service.
	ServiceName string `json:"service_name"`
	// StartTime is when the span started.
	StartTime time.Time `json:"start_time"`
	// Duration is the duration of the span.
	Duration time.Duration `json:"duration"`
	// Tags contains additional tags.
	Tags map[string]string `json:"tags,omitempty"`
	// Logs contains log entries within the span.
	Logs []SpanLog `json:"logs,omitempty"`
	// HasError indicates if the span contains errors.
	HasError bool `json:"has_error"`
}

// SpanLog represents a log entry within a span.
type SpanLog struct {
	// Timestamp is when the log was created.
	Timestamp time.Time `json:"timestamp"`
	// Fields contains the log fields.
	Fields map[string]interface{} `json:"fields"`
}

// APMMetricsRequest represents a request to query APM-specific metrics.
type APMMetricsRequest struct {
	// StartTime is the start time of the query range.
	StartTime time.Time `json:"start_time"`
	// EndTime is the end time of the query range.
	EndTime time.Time `json:"end_time"`
	// ServiceName is the name of the service.
	ServiceName string `json:"service_name,omitempty"`
	// MetricType is the type of metric (e.g., "response_time", "error_rate").
	MetricType string `json:"metric_type"`
	// Interval is the aggregation interval.
	Interval time.Duration `json:"interval,omitempty"`
}

// APMMetricsResponse represents the response from an APM metrics query.
type APMMetricsResponse struct {
	// ServiceName is the name of the service.
	ServiceName string `json:"service_name"`
	// MetricType is the type of metric.
	MetricType string `json:"metric_type"`
	// DataPoints contains the aggregated data points.
	DataPoints []APMDataPoint `json:"data_points"`
}

// APMDataPoint represents a single aggregated data point.
type APMDataPoint struct {
	// Timestamp is when the data point was recorded.
	Timestamp time.Time `json:"timestamp"`
	// Value is the aggregated value.
	Value float64 `json:"value"`
	// Count is the number of samples (for averages).
	Count int64 `json:"count,omitempty"`
}
