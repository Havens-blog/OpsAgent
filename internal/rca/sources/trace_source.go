// Package sources provides trace data source interfaces for RCA analysis.
package sources

import (
	"context"
	"time"

	"github.com/yourusername/OpsAgent/internal/rca/models"
)

// TraceSource provides distributed trace data for RCA analysis.
type TraceSource interface {
	QueryErrorTraces(ctx context.Context, start, end time.Time) ([]models.TraceSummary, error)
	QueryTracesByService(ctx context.Context, service string, start, end time.Time) ([]models.TraceSummary, error)
	GetTraceDetails(ctx context.Context, traceID string) (*models.TraceSummary, error)
	QuerySlowTraces(ctx context.Context, start, end time.Time, minDuration time.Duration) ([]models.TraceSummary, error)
}
