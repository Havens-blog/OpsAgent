// Package sources provides change detection data source interfaces for RCA analysis.
package sources

import (
	"context"
	"time"

	"github.com/yourusername/OpsAgent/internal/rca/models"
)

// ChangeSource provides change/event data for RCA analysis.
type ChangeSource interface {
	QueryChanges(ctx context.Context, start, end time.Time) ([]models.Change, error)
	QueryChangesByService(ctx context.Context, service string, start, end time.Time) ([]models.Change, error)
	GetLastChange(ctx context.Context, service string) (*models.Change, error)
}
