// Package sources provides topology data source interfaces for RCA analysis.
package sources

import (
	"context"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// TopologySource provides service topology/dependency graph data.
type TopologySource interface {
	GetTopology(ctx context.Context) (*models.Topology, error)
	GetServiceNeighbors(ctx context.Context, service string) (dependents, dependencies []string, err error)
	GetServiceHealth(ctx context.Context) (map[string]string, error)
}

// ServiceMetrics holds per-service RED (Rate/Errors/Duration) figures.
// Values are already sanitized: no Inf/NaN (JSON-safe), rates are per-second,
// latency is milliseconds, and ErrorRate is a 0..1 fraction.
type ServiceMetrics struct {
	Throughput float64 // requests per second
	ErrorRate  float64 // 0..1 fraction of requests that errored
	LatencyMs  float64 // average request latency in milliseconds
}

// ServiceMetricsProvider is an OPTIONAL capability: a topology source may also
// expose per-service traffic metrics. Handlers type-assert for it, so sources
// that cannot provide metrics (e.g. the mock) simply don't implement it.
type ServiceMetricsProvider interface {
	GetServiceMetrics(ctx context.Context) (map[string]ServiceMetrics, error)
}
