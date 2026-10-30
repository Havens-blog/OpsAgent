package sources

import (
	"context"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// Empty sources are the HONEST fallback for an unconfigured dimension: they
// satisfy the source interface but return no data (and no error), so the RCA
// engine simply produces no hypotheses from that dimension.
//
// This replaces the former Mock* fallback in the progressive-enhancement path.
// The mocks fabricate plausible-looking demo signals (e.g. a hard-coded
// "replica count scaled from 5 to 8" change pinned to always land inside the
// incident window and score high). That made every analysis surface the same
// invented root cause regardless of the real incident — worse than useless,
// because it manufactures false confidence. A dimension with no real backend
// must stay silent, not lie. The Mock* sources remain for tests only.

// ---- Logs ----

type EmptyLogSource struct{}

func NewEmptyLogSource() *EmptyLogSource { return &EmptyLogSource{} }

func (EmptyLogSource) QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
	return nil, nil
}

func (EmptyLogSource) QueryErrorsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	return nil, nil
}

func (EmptyLogSource) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	return nil, nil
}

func (EmptyLogSource) GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error) {
	return 0, nil
}

// ---- Changes ----

type EmptyChangeSource struct{}

func NewEmptyChangeSource() *EmptyChangeSource { return &EmptyChangeSource{} }

func (EmptyChangeSource) QueryChanges(ctx context.Context, start, end time.Time) ([]models.Change, error) {
	return nil, nil
}

func (EmptyChangeSource) QueryChangesByService(ctx context.Context, service string, start, end time.Time) ([]models.Change, error) {
	return nil, nil
}

func (EmptyChangeSource) GetLastChange(ctx context.Context, service string) (*models.Change, error) {
	return nil, nil
}

// ---- Traces ----

type EmptyTraceSource struct{}

func NewEmptyTraceSource() *EmptyTraceSource { return &EmptyTraceSource{} }

func (EmptyTraceSource) QueryErrorTraces(ctx context.Context, start, end time.Time) ([]models.TraceSummary, error) {
	return nil, nil
}

func (EmptyTraceSource) QueryTracesByService(ctx context.Context, service string, start, end time.Time) ([]models.TraceSummary, error) {
	return nil, nil
}

func (EmptyTraceSource) GetTraceDetails(ctx context.Context, traceID string) (*models.TraceSummary, error) {
	return nil, nil
}

func (EmptyTraceSource) QuerySlowTraces(ctx context.Context, start, end time.Time, minDuration time.Duration) ([]models.TraceSummary, error) {
	return nil, nil
}

// ---- Metrics ----

type EmptyMetricSource struct{}

func NewEmptyMetricSource() *EmptyMetricSource { return &EmptyMetricSource{} }

func (EmptyMetricSource) QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
	return nil, nil
}

func (EmptyMetricSource) QueryMetric(ctx context.Context, metricName string, start, end time.Time, filters map[string]string) (*models.MetricSeries, error) {
	return nil, nil
}

func (EmptyMetricSource) QueryMetricsForService(ctx context.Context, service string, start, end time.Time) ([]models.MetricSeries, error) {
	return nil, nil
}

func (EmptyMetricSource) GetAlerts(ctx context.Context) ([]Alert, error) {
	return nil, nil
}

// QueryMetricRange returns an honest empty slice: no real metric backend is
// configured, so there is nothing to query and no series to fabricate.
func (EmptyMetricSource) QueryMetricRange(ctx context.Context, query string, start, end time.Time, step string) ([]models.MetricSeries, error) {
	return []models.MetricSeries{}, nil
}

// ---- Topology ----

type EmptyTopologySource struct{}

func NewEmptyTopologySource() *EmptyTopologySource { return &EmptyTopologySource{} }

// GetTopology returns an empty (non-nil) graph so callers can range over it
// without a nil check.
func (EmptyTopologySource) GetTopology(ctx context.Context) (*models.Topology, error) {
	return &models.Topology{}, nil
}

func (EmptyTopologySource) GetServiceNeighbors(ctx context.Context, service string) (dependents, dependencies []string, err error) {
	return nil, nil, nil
}

func (EmptyTopologySource) GetServiceHealth(ctx context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

// Compile-time interface conformance.
var (
	_ LogSource      = (*EmptyLogSource)(nil)
	_ ChangeSource   = (*EmptyChangeSource)(nil)
	_ TraceSource    = (*EmptyTraceSource)(nil)
	_ MetricSource   = (*EmptyMetricSource)(nil)
	_ TopologySource = (*EmptyTopologySource)(nil)
)
