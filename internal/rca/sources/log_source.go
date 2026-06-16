// Package sources provides data source interfaces for RCA analysis.
package sources

import (
	"context"
	"time"

	"github.com/yourusername/OpsAgent/internal/rca/models"
)

// LogSource provides log data for RCA analysis.
type LogSource interface {
	// QueryErrors retrieves error logs within the time window.
	QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error)

	// QueryLogsByService retrieves logs for a specific service.
	QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error)

	// GetLogCount returns the count of logs for a service.
	GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error)
}
