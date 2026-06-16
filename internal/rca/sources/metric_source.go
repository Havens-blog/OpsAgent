// Package sources provides metric data source interfaces for RCA analysis.
package sources

import (
	"context"
	"time"

	"github.com/yourusername/OpsAgent/internal/rca/models"
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
	Labels       map[string]string
	Annotations  map[string]string
	StartsAt     time.Time
	EndsAt       time.Time
	State        string
	Summary      string
	Description  string
}
