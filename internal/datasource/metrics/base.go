// Package metrics provides base functionality for metrics data sources.
package metrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/datasource/core"
)

// BaseMetricsDataSource provides common functionality for metrics data sources.
type BaseMetricsDataSource struct {
	mu              sync.RWMutex
	config          *core.Config
	connected       bool
	healthStatus    core.HealthStatus
	lastHealthCheck time.Time
	retryer         *core.Retryer
}

// NewBaseMetricsDataSource creates a new base metrics data source.
func NewBaseMetricsDataSource(config *core.Config) *BaseMetricsDataSource {
	return &BaseMetricsDataSource{
		config:       config,
		healthStatus: core.StatusUnknown,
		retryer:      core.NewRetryer(config.RetryConfig),
	}
}

// Connect establishes a connection to the data source.
func (b *BaseMetricsDataSource) Connect(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.connected = true
	b.healthStatus = core.StatusHealthy
	b.lastHealthCheck = time.Now()

	return nil
}

// Close closes the connection to the data source.
func (b *BaseMetricsDataSource) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.connected = false
	b.healthStatus = core.StatusUnknown

	return nil
}

// Ping checks if the data source is reachable.
func (b *BaseMetricsDataSource) Ping(ctx context.Context) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.connected {
		return fmt.Errorf("data source is not connected")
	}

	return nil
}

// Health returns the detailed health status of the data source.
func (b *BaseMetricsDataSource) Health(ctx context.Context) (*core.HealthInfo, error) {
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
func (b *BaseMetricsDataSource) Type() core.DataSourceType {
	return b.config.Type
}

// Name returns the name/identifier of the data source instance.
func (b *BaseMetricsDataSource) Name() string {
	return b.config.Name
}

// GetConfig returns the configuration.
func (b *BaseMetricsDataSource) GetConfig() *core.Config {
	return b.config
}

// IsConnected returns true if the data source is connected.
func (b *BaseMetricsDataSource) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connected
}

// IsHealthy returns true if the data source is healthy.
func (b *BaseMetricsDataSource) IsHealthy() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.healthStatus == core.StatusHealthy
}

// GetHealthStatus returns the current health status.
func (b *BaseMetricsDataSource) GetHealthStatus() core.HealthStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.healthStatus
}

// GetRetryer returns the retryer.
func (b *BaseMetricsDataSource) GetRetryer() *core.Retryer {
	return b.retryer
}

// SetRetryer sets a custom retryer.
func (b *BaseMetricsDataSource) SetRetryer(retryer *core.Retryer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.retryer = retryer
}

// ping performs a ping operation. Should be overridden by implementations.
func (b *BaseMetricsDataSource) ping(ctx context.Context) error {
	return nil
}

// ExecuteWithRetry executes a function with retry logic.
func (b *BaseMetricsDataSource) ExecuteWithRetry(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	return b.retryer.Do(ctx, fn)
}

// ValidateMetricsQueryRequest validates a metrics query request.
func (b *BaseMetricsDataSource) ValidateMetricsQueryRequest(req *core.MetricsQueryRequest) error {
	if req == nil {
		return fmt.Errorf("metrics query request cannot be nil")
	}

	if req.Query == "" {
		return fmt.Errorf("query is required")
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

	return nil
}

// NormalizeMetricsTimeRange ensures the metrics time range is valid and applies defaults.
func (b *BaseMetricsDataSource) NormalizeMetricsTimeRange(req *core.MetricsQueryRequest) {
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

	// Set default step to 1 minute if not specified
	if req.Step == 0 {
		req.Step = 1 * time.Minute
	}
}
