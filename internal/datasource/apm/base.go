// Package apm provides base functionality for APM data sources.
package apm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yourusername/OpsAgent/internal/datasource/core"
	"github.com/yourusername/OpsAgent/internal/datasource/credentials"
)

// BaseAPMDataSource provides common functionality for APM data sources.
type BaseAPMDataSource struct {
	mu            sync.RWMutex
	config        *core.Config
	connected     bool
	credentials   *credentials.Credentials
	healthStatus  core.HealthStatus
	lastHealthCheck time.Time
	retryer       *core.Retryer
}

// NewBaseAPMDataSource creates a new base APM data source.
func NewBaseAPMDataSource(config *core.Config) *BaseAPMDataSource {
	return &BaseAPMDataSource{
		config:       config,
		healthStatus: core.StatusUnknown,
		retryer:      core.NewRetryer(config.RetryConfig),
	}
}

// Connect establishes a connection to the data source.
func (b *BaseAPMDataSource) Connect(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.connected = true
	b.healthStatus = core.StatusHealthy
	b.lastHealthCheck = time.Now()

	return nil
}

// Close closes the connection to the data source.
func (b *BaseAPMDataSource) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.connected = false
	b.healthStatus = core.StatusUnknown

	return nil
}

// Ping checks if the data source is reachable.
func (b *BaseAPMDataSource) Ping(ctx context.Context) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.connected {
		return fmt.Errorf("data source is not connected")
	}

	return nil
}

// Health returns the detailed health status of the data source.
func (b *BaseAPMDataSource) Health(ctx context.Context) (*core.HealthInfo, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	start := time.Now()
	info := &core.HealthInfo{
		Status:    b.healthStatus,
		CheckedAt: b.lastHealthCheck,
		Details:   make(map[string]interface{}),
	}

	// Perform health check
	err := b.ping(ctx)
	info.Latency = time.Since(start)

	if err != nil {
		info.Status = core.StatusUnhealthy
		info.Message = err.Error()
		b.mu.RUnlock()
		b.mu.Lock()
		b.healthStatus = core.StatusUnhealthy
		b.mu.Unlock()
		b.mu.RLock()
	} else {
		info.Status = core.StatusHealthy
		info.Message = "OK"
		b.mu.RUnlock()
		b.mu.Lock()
		b.healthStatus = core.StatusHealthy
		b.mu.Unlock()
		b.mu.RLock()
	}

	info.Details["connected"] = b.connected
	info.Details["type"] = string(b.Type())
	info.Details["name"] = b.Name()

	return info, nil
}

// Type returns the type of the data source.
func (b *BaseAPMDataSource) Type() core.DataSourceType {
	return b.config.Type
}

// Name returns the name/identifier of the data source instance.
func (b *BaseAPMDataSource) Name() string {
	return b.config.Name
}

// SetCredentials sets the credentials for the data source.
func (b *BaseAPMDataSource) SetCredentials(creds *credentials.Credentials) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.credentials = creds
}

// GetCredentials returns the credentials for the data source.
func (b *BaseAPMDataSource) GetCredentials() *credentials.Credentials {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.credentials
}

// IsConnected returns true if the data source is connected.
func (b *BaseAPMDataSource) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.connected
}

// GetConfig returns the configuration.
func (b *BaseAPMDataSource) GetConfig() *core.Config {
	return b.config
}

// ping performs a ping operation. Should be overridden by implementations.
func (b *BaseAPMDataSource) ping(ctx context.Context) error {
	return nil
}

// ExecuteWithRetry executes a function with retry logic.
func (b *BaseAPMDataSource) ExecuteWithRetry(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	return b.retryer.Do(ctx, fn)
}

// ValidateTraceQueryRequest validates a trace query request.
func (b *BaseAPMDataSource) ValidateTraceQueryRequest(req *core.TraceQueryRequest) error {
	if req == nil {
		return fmt.Errorf("trace query request cannot be nil")
	}

	if req.StartTime.IsZero() {
		return fmt.Errorf("start_time is required")
	}

	if req.EndTime.IsZero() {
		return fmt.Errorf("end_time is required")
	}

	if req.EndTime.Before(req.StartTime) {
		return fmt.Errorf("end_time must be after start_time")
	}

	if req.TraceID == "" && req.ServiceName == "" {
		return fmt.Errorf("either trace_id or service_name must be specified")
	}

	if req.Limit <= 0 {
		req.Limit = 100 // Default limit
	}

	if req.Limit > 1000 {
		return fmt.Errorf("limit cannot exceed 1000")
	}

	return nil
}

// NormalizeTraceTimeRange ensures the trace time range is valid and applies defaults.
func (b *BaseAPMDataSource) NormalizeTraceTimeRange(req *core.TraceQueryRequest) {
	// Set default end time to now if not specified
	if req.EndTime.IsZero() {
		req.EndTime = time.Now()
	}

	// Set default start time to 1 hour before end time if not specified
	if req.StartTime.IsZero() {
		req.StartTime = req.EndTime.Add(-1 * time.Hour)
	}

	// Ensure start time is before end time
	if req.StartTime.After(req.EndTime) {
		req.StartTime, req.EndTime = req.EndTime, req.StartTime
	}
}

// ValidateAPMMetricsRequest validates an APM metrics request.
func (b *BaseAPMDataSource) ValidateAPMMetricsRequest(req *core.APMMetricsRequest) error {
	if req == nil {
		return fmt.Errorf("APM metrics request cannot be nil")
	}

	if req.StartTime.IsZero() {
		return fmt.Errorf("start_time is required")
	}

	if req.EndTime.IsZero() {
		return fmt.Errorf("end_time is required")
	}

	if req.EndTime.Before(req.StartTime) {
		return fmt.Errorf("end_time must be after start_time")
	}

	if req.MetricType == "" {
		return fmt.Errorf("metric_type is required")
	}

	// Valid metric types
	validTypes := map[string]bool{
		"response_time":  true,
		"error_rate":     true,
		"throughput":     true,
		"apdex":          true,
		"request_count":  true,
		"error_count":    true,
		"slow_count":     true,
	}

	if !validTypes[req.MetricType] {
		return fmt.Errorf("invalid metric_type: %s", req.MetricType)
	}

	return nil
}

// NormalizeAPMMetricsTimeRange ensures the APM metrics time range is valid and applies defaults.
func (b *BaseAPMDataSource) NormalizeAPMMetricsTimeRange(req *core.APMMetricsRequest) {
	// Set default end time to now if not specified
	if req.EndTime.IsZero() {
		req.EndTime = time.Now()
	}

	// Set default start time to 15 minutes before end time if not specified
	if req.StartTime.IsZero() {
		req.StartTime = req.EndTime.Add(-15 * time.Minute)
	}

	// Ensure start time is before end time
	if req.StartTime.After(req.EndTime) {
		req.StartTime, req.EndTime = req.EndTime, req.StartTime
	}

	// Set default interval to 1 minute if not specified
	if req.Interval == 0 {
		req.Interval = 1 * time.Minute
	}
}

// ApplyDefaults applies default values to a trace query request.
func (b *BaseAPMDataSource) ApplyTraceDefaults(req *core.TraceQueryRequest) {
	b.NormalizeTraceTimeRange(req)

	if req.Limit <= 0 {
		req.Limit = 100
	}

	if req.Filter == nil {
		req.Filter = make(map[string]string)
	}
}

// QueryTraces queries traces from the APM data source.
// This is a base implementation that should be overridden.
func (b *BaseAPMDataSource) QueryTraces(ctx context.Context, req *core.TraceQueryRequest) (*core.TraceQueryResponse, error) {
	return nil, fmt.Errorf("QueryTraces not implemented for %s", b.Type())
}

// QueryMetrics queries APM-specific metrics.
// This is a base implementation that should be overridden.
func (b *BaseAPMDataSource) QueryMetrics(ctx context.Context, req *core.APMMetricsRequest) (*core.APMMetricsResponse, error) {
	return nil, fmt.Errorf("QueryMetrics not implemented for %s", b.Type())
}

// GetTraceDetails retrieves details for a specific trace.
// This is a base implementation that should be overridden.
func (b *BaseAPMDataSource) GetTraceDetails(ctx context.Context, traceID string) (*core.TraceDetails, error) {
	return nil, fmt.Errorf("GetTraceDetails not implemented for %s", b.Type())
}

// IsHealthy returns true if the data source is healthy.
func (b *BaseAPMDataSource) IsHealthy() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.healthStatus == core.StatusHealthy
}

// GetHealthStatus returns the current health status.
func (b *BaseAPMDataSource) GetHealthStatus() core.HealthStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.healthStatus
}

// GetRetryer returns the retryer.
func (b *BaseAPMDataSource) GetRetryer() *core.Retryer {
	return b.retryer
}

// SetRetryer sets a custom retryer.
func (b *BaseAPMDataSource) SetRetryer(retryer *core.Retryer) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.retryer = retryer
}

// ParseTraceSummary parses a raw trace summary into a structured format.
// This is a helper method that can be used by implementations.
func (b *BaseAPMDataSource) ParseTraceSummary(raw map[string]interface{}) (*core.TraceSummary, error) {
	summary := &core.TraceSummary{
		Labels: make(map[string]string),
	}

	// Extract trace ID
	if traceID, ok := raw["trace_id"]; ok {
		if s, ok := traceID.(string); ok {
			summary.TraceID = s
		}
		delete(raw, "trace_id")
	}

	// Extract service name
	if serviceName, ok := raw["service_name"]; ok {
		if s, ok := serviceName.(string); ok {
			summary.ServiceName = s
		}
		delete(raw, "service_name")
	}

	// Extract operation name
	if opName, ok := raw["operation_name"]; ok {
		if s, ok := opName.(string); ok {
			summary.OperationName = s
		}
		delete(raw, "operation_name")
	}

	// Extract timestamps
	if start, ok := raw["start_time"]; ok {
		switch v := start.(type) {
		case time.Time:
			summary.StartTime = v
		case string:
			if parsed, err := time.Parse(time.RFC3339, v); err == nil {
				summary.StartTime = parsed
			} else if parsed, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
				summary.StartTime = parsed
			}
		case float64:
			summary.StartTime = time.Unix(int64(v), 0)
		}
		delete(raw, "start_time")
	}

	if duration, ok := raw["duration"]; ok {
		switch v := duration.(type) {
		case time.Duration:
			summary.Duration = v
		case float64:
			// Assume milliseconds
			summary.Duration = time.Duration(v) * time.Millisecond
		case string:
			if parsed, err := time.ParseDuration(v); err == nil {
				summary.Duration = parsed
			}
		}
		delete(raw, "duration")
	}

	// Extract error flag
	if hasError, ok := raw["has_error"]; ok {
		if b, ok := hasError.(bool); ok {
			summary.HasError = b
		}
		delete(raw, "has_error")
	}

	// Extract span count
	if spanCount, ok := raw["span_count"]; ok {
		switch v := spanCount.(type) {
		case int:
			summary.SpanCount = v
		case float64:
			summary.SpanCount = int(v)
		}
		delete(raw, "span_count")
	}

	// Remaining fields go to Labels
	for k, v := range raw {
		if s, ok := v.(string); ok {
			summary.Labels[k] = s
		} else {
			summary.Labels[k] = fmt.Sprintf("%v", v)
		}
	}

	return summary, nil
}

// ParseSpan parses a raw span into a structured format.
// This is a helper method that can be used by implementations.
func (b *BaseAPMDataSource) ParseSpan(raw map[string]interface{}) (*core.Span, error) {
	span := &core.Span{
		Tags: make(map[string]string),
		Logs: make([]core.SpanLog, 0),
	}

	// Extract span ID
	if spanID, ok := raw["span_id"]; ok {
		if s, ok := spanID.(string); ok {
			span.SpanID = s
		}
		delete(raw, "span_id")
	}

	// Extract parent span ID
	if parentSpanID, ok := raw["parent_span_id"]; ok {
		if s, ok := parentSpanID.(string); ok {
			span.ParentSpanID = s
		}
		delete(raw, "parent_span_id")
	}

	// Extract operation name
	if opName, ok := raw["operation_name"]; ok {
		if s, ok := opName.(string); ok {
			span.OperationName = s
		}
		delete(raw, "operation_name")
	}

	// Extract service name
	if serviceName, ok := raw["service_name"]; ok {
		if s, ok := serviceName.(string); ok {
			span.ServiceName = s
		}
		delete(raw, "service_name")
	}

	// Extract timestamps
	if start, ok := raw["start_time"]; ok {
		switch v := start.(type) {
		case time.Time:
			span.StartTime = v
		case float64:
			span.StartTime = time.Unix(int64(v), 0)
		}
		delete(raw, "start_time")
	}

	if duration, ok := raw["duration"]; ok {
		switch v := duration.(type) {
		case time.Duration:
			span.Duration = v
		case float64:
			span.Duration = time.Duration(v) * time.Millisecond
		}
		delete(raw, "duration")
	}

	// Extract error flag
	if hasError, ok := raw["has_error"]; ok {
		if b, ok := hasError.(bool); ok {
			span.HasError = b
		}
		delete(raw, "has_error")
	}

	// Extract logs
	if logs, ok := raw["logs"].([]interface{}); ok {
		for _, log := range logs {
			if logMap, ok := log.(map[string]interface{}); ok {
				spanLog := b.parseSpanLog(logMap)
				span.Logs = append(span.Logs, spanLog)
			}
		}
		delete(raw, "logs")
	}

	// Remaining fields go to Tags
	for k, v := range raw {
		if s, ok := v.(string); ok {
			span.Tags[k] = s
		} else {
			span.Tags[k] = fmt.Sprintf("%v", v)
		}
	}

	return span, nil
}

// parseSpanLog parses a span log from raw data.
func (b *BaseAPMDataSource) parseSpanLog(raw map[string]interface{}) core.SpanLog {
	spanLog := core.SpanLog{
		Fields: make(map[string]interface{}),
	}

	// Extract timestamp
	if ts, ok := raw["timestamp"]; ok {
		switch v := ts.(type) {
		case time.Time:
			spanLog.Timestamp = v
		case float64:
			spanLog.Timestamp = time.Unix(int64(v), 0)
		case string:
			if parsed, err := time.Parse(time.RFC3339, v); err == nil {
				spanLog.Timestamp = parsed
			}
		}
		delete(raw, "timestamp")
	} else {
		spanLog.Timestamp = time.Now()
	}

	// Remaining fields go to Fields
	for k, v := range raw {
		spanLog.Fields[k] = v
	}

	return spanLog
}
