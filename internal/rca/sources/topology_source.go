// Package sources provides topology data source interfaces for RCA analysis.
package sources

import (
	"context"

	"github.com/yourusername/OpsAgent/internal/rca/models"
)

// TopologySource provides service topology/dependency graph data.
type TopologySource interface {
	GetTopology(ctx context.Context) (*models.Topology, error)
	GetServiceNeighbors(ctx context.Context, service string) (dependents, dependencies []string, err error)
	GetServiceHealth(ctx context.Context) (map[string]string, error)
}
