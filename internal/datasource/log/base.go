// Package log provides base functionality for log data sources.
package log

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/datasource/core"
	"github.com/Havens-blog/OpsAgent/internal/datasource/credentials"
)

// BaseLogDataSource provides common functionality for log data sources.
type BaseLogDataSource struct {
	mu            sync.RWMutex
	config        *core.Config
	connected     bool
	credentials   *credentials.Credentials
	healthStatus  core.HealthStatus
	lastHealthCheck time.Time
	retryer       *core.Retryer
}

// NewBaseLogDataSource creates a new base log data source.
func NewBaseLogDataSource(config *core.Config) *BaseLogDataSource {
	return &BaseLogDataSource{
		config:       config,
		healthStatus: core.StatusUnknown,
		retryer:      core.NewRetryer(config.RetryConfig),
	}
}

// Connect establishes a connection to the data source.
// This is a base implementation that should be overridden by specific implementations.
func (b *BaseLogDataSource) Connect(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.connected = true
	b.healthStatus = core.StatusHealthy
	b.lastHealthCheck = time.Now()

	return nil
}

// Close closes the connection to the data source.
func (b *BaseLogDataSource) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.connected = false
	b.healthStatus = core.StatusUnknown

	return nil
}

// Ping checks if the data source is reachable.
func (b *BaseLogDataSource) Ping(ctx context.Context) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.connected {
		return fmt.Errorf("data source is not connected")
	}

	return nil
}

// Health returns the detailed health status of the data source.
func (b *BaseLogDataSource) Health(ctx context.Context) (*core.HealthInfo, error) {
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
func (b *BaseLogDataSource) Type() core.DataSourceType {
	return b.config.Type
}

// Name returns the name/identifier of the data source instance.
func (b *BaseLogDataSource) Name() string {
	return b.config.Name
}

// SetCredentials sets the credentials for the data source.
func (b *BaseLogDataSource) SetCredentials(creds *credentials.Credentials) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.credentials = creds
}

// GetCredentials returns the credentials for the data source.
func (b *BaseLogDataSource) GetCredentials() *credentials.Credentials {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.credentials
}

// IsConnected returns true if the data source is connected.
func (b *BaseLogDataSource) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.connected
}

// GetConfig returns the configuration.
func (b *BaseLogDataSource) GetConfig() *core.Config {
	return b.config
}

// ping performs a ping operation. Should be overridden by implementations.
func (b *BaseLogDataSource) ping(ctx context.Context) error {
	return nil
}

// ExecuteWithRetry executes a function with retry logic.
func (b *BaseLogDataSource) ExecuteWithRetry(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	return b.retryer.Do(ctx, fn)
}

// ValidateQueryRequest validates a log query request.
func (b *BaseLogDataSource) ValidateQueryRequest(req *core.LogQueryRequest) error {
	if req == nil {
		return fmt.Errorf("query request cannot be nil")
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

	if req.Limit <= 0 {
		req.Limit = 100 // Default limit
	}

	if req.Limit > 10000 {
		return fmt.Errorf("limit cannot exceed 10000")
	}

	// A request must carry EITHER a raw Query string OR a structured Intent.
	// Under the "express intent, not dialect" design, intent-based requests
	// leave Query empty and each data source builds its own dialect from
	// Intent, so an empty Query is valid as long as Intent is set.
	if req.Query == "" && req.Intent == nil {
		return fmt.Errorf("either query or intent is required")
	}

	return nil
}

// NormalizeTimeRange ensures the time range is valid and applies defaults.
func (b *BaseLogDataSource) NormalizeTimeRange(req *core.LogQueryRequest) {
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

// ApplyDefaults applies default values to a log query request.
func (b *BaseLogDataSource) ApplyDefaults(req *core.LogQueryRequest) {
	b.NormalizeTimeRange(req)

	if req.Limit <= 0 {
		req.Limit = 100
	}

	if req.Sort == "" {
		req.Sort = "desc" // Default to descending (newest first)
	}

	if req.Filter == nil {
		req.Filter = make(map[string]string)
	}
}

// LogQueryOptions contains optional parameters for log queries.
type LogQueryOptions struct {
	// Fields specifies which fields to return.
	Fields []string
	// IncludeTraceID includes trace ID in results.
	IncludeTraceID bool
	// Highlight enables highlighting of matching terms.
	Highlight bool
}

// ApplyOptions applies options to a log query request.
func (b *BaseLogDataSource) ApplyOptions(req *core.LogQueryRequest, opts *LogQueryOptions) {
	if opts == nil {
		return
	}

	if len(opts.Fields) > 0 {
		req.Fields = opts.Fields
	}
}

// QueryLogs queries logs from the data source.
// This is a base implementation that should be overridden.
func (b *BaseLogDataSource) QueryLogs(ctx context.Context, req *core.LogQueryRequest) (*core.LogQueryResponse, error) {
	return nil, fmt.Errorf("QueryLogs not implemented for %s", b.Type())
}

// GetLogCount returns the count of logs matching the query.
// This is a base implementation that should be overridden.
func (b *BaseLogDataSource) GetLogCount(ctx context.Context, req *core.LogQueryRequest) (int64, error) {
	return 0, fmt.Errorf("GetLogCount not implemented for %s", b.Type())
}

// ParseLogEntry parses a raw log entry into a structured format.
// This is a helper method that can be used by implementations.
func (b *BaseLogDataSource) ParseLogEntry(raw map[string]interface{}) (*core.LogEntry, error) {
	entry := &core.LogEntry{
		Fields: make(map[string]interface{}),
	}

	// Extract timestamp
	if ts, ok := raw["timestamp"]; ok {
		switch v := ts.(type) {
		case time.Time:
			entry.Timestamp = v
		case string:
			if parsed, err := time.Parse(time.RFC3339, v); err == nil {
				entry.Timestamp = parsed
			} else if parsed, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
				entry.Timestamp = parsed
			}
		case float64:
			entry.Timestamp = time.Unix(int64(v), 0)
		}
	} else {
		entry.Timestamp = time.Now()
	}

	// Extract level
	if level, ok := raw["level"]; ok {
		if s, ok := level.(string); ok {
			entry.Level = s
		}
		delete(raw, "level")
	}

	// Extract message
	if msg, ok := raw["message"]; ok {
		if s, ok := msg.(string); ok {
			entry.Message = s
		}
		delete(raw, "message")
	}

	// Extract source
	if src, ok := raw["source"]; ok {
		if s, ok := src.(string); ok {
			entry.Source = s
		}
		delete(raw, "source")
	}

	// Extract trace ID
	if traceID, ok := raw["trace_id"]; ok {
		if s, ok := traceID.(string); ok {
			entry.TraceID = s
		}
		delete(raw, "trace_id")
	}

	// Extract span ID
	if spanID, ok := raw["span_id"]; ok {
		if s, ok := spanID.(string); ok {
			entry.SpanID = s
		}
		delete(raw, "span_id")
	}

	// Remaining fields go to Fields
	for k, v := range raw {
		entry.Fields[k] = v
	}

	return entry, nil
}

// backfillSource fills entry.Source from the configured service field when the
// parser could not populate it from a conventional "source" key. The service
// identifier is deployment-specific — e.g. this cluster's Volcengine TLS logs
// carry it in "document_type", not "service" — so the datasource layer, which
// alone knows its serviceField config, is the right place to resolve it. This
// is what lets a real log line reach RCA WITH a service name, so error-spike
// hypotheses can be attributed to a service and correlated with topology,
// instead of collapsing into one empty-service bucket.
//
// No-op when Source is already set, the field name is empty, or the field is
// absent/non-string — so a deployment that does not configure serviceField
// behaves exactly as before (no regression).
func backfillSource(entry *core.LogEntry, serviceField string) {
	if entry == nil || entry.Source != "" || serviceField == "" {
		return
	}
	if v, ok := entry.Fields[serviceField]; ok {
		if s, ok := v.(string); ok && s != "" {
			entry.Source = s
		}
	}
}

// IsHealthy returns true if the data source is healthy.
func (b *BaseLogDataSource) IsHealthy() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.healthStatus == core.StatusHealthy
}

// GetHealthStatus returns the current health status.
func (b *BaseLogDataSource) GetHealthStatus() core.HealthStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.healthStatus
}

// GetRetryer returns the retryer.
func (b *BaseLogDataSource) GetRetryer() *core.Retryer {
	return b.retryer
}

// SetRetryer sets a custom retryer.
func (b *BaseLogDataSource) SetRetryer(retryer *core.Retryer) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.retryer = retryer
}
