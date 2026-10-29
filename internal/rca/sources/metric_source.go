// Package sources provides metric data source interfaces for RCA analysis.
package sources

import (
	"context"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// MetricSource provides metric data for RCA analysis.
// Primarily integrated with Prometheus for alert-driven workflows.
type MetricSource interface {
	// QueryAnomalies retrieves metrics with anomalies within the time window.
	QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error)

	// QueryMetric retrieves a specific metric by name.
	QueryMetric(ctx context.Context, metricName string, start, end time.Time, filters map[string]string) (*models.MetricSeries, error)

	// QueryMetricsForService retrieves all metrics for a specific service.
	QueryMetricsForService(ctx context.Context, service string, start, end time.Time) ([]models.MetricSeries, error)

	// GetAlerts returns current active alerts from Prometheus.
	GetAlerts(ctx context.Context) ([]Alert, error)
}

// Alert represents a Prometheus alert.
type Alert struct {
	Labels      map[string]string
	Annotations map[string]string
	StartsAt    time.Time
	EndsAt      time.Time
	State       string
	Summary     string
	Description string
}

// MetricRangeSource is an OPTIONAL extension implemented by metric sources
// that can execute an arbitrary range query and return every matching series,
// honouring the caller's step. It is kept separate from MetricSource on
// purpose: adding a method to MetricSource would force every test double that
// satisfies it (including those in internal/agent and the RCA engine tests) to
// grow the same method, and those packages are out of scope here. The MCP
// metrics tool type-asserts this interface to get multi-series results.
type MetricRangeSource interface {
	// QueryMetricRange runs query over [start, end] with the given step and
	// returns ALL matching series (never just the first one).
	QueryMetricRange(ctx context.Context, query string, start, end time.Time, step string) ([]models.MetricSeries, error)
}
