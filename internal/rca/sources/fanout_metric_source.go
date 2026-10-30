// Package sources provides metric data source interfaces for RCA analysis.
package sources

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// FanoutMetricSource fans a metric query out across multiple MetricSources
// (e.g. an application-level ARMS APM workspace plus a CloudMonitor
// infrastructure workspace) concurrently, then merges the results into one
// deduplicated, deterministically sorted slice.
//
// Partial-failure tolerance mirrors AggregateLogSource: if one backing source
// errors, its failure is skipped and the healthy sources' data is still
// returned. An error is returned only when EVERY backing source fails, so a
// single workspace outage never discards the other workspace's metrics.
type FanoutMetricSource struct {
	sources []MetricSource
}

// NewFanoutMetricSource builds a fanout over the given sources. nil sources are
// ignored; an empty fanout behaves like an empty metric source.
func NewFanoutMetricSource(sources ...MetricSource) *FanoutMetricSource {
	f := &FanoutMetricSource{}
	for _, src := range sources {
		if src != nil {
			f.sources = append(f.sources, src)
		}
	}
	return f
}

// Count returns the number of backing sources.
func (f *FanoutMetricSource) Count() int { return len(f.sources) }

// seriesResult is the per-source outcome of a fan-out series query.
type seriesResult struct {
	series []models.MetricSeries
	err    error
}

// fanOutSeries runs query against every source concurrently and merges the
// per-source results.
func (f *FanoutMetricSource) fanOutSeries(
	ctx context.Context,
	query func(ctx context.Context, src MetricSource) ([]models.MetricSeries, error),
) ([]models.MetricSeries, error) {
	results := make([]seriesResult, len(f.sources))
	var wg sync.WaitGroup
	for i, src := range f.sources {
		wg.Add(1)
		go func(i int, src MetricSource) {
			defer wg.Done()
			series, err := query(ctx, src)
			results[i] = seriesResult{series: series, err: err}
		}(i, src)
	}
	wg.Wait()
	return mergeSeries(results)
}

// mergeSeries combines per-source series, dedupes by Name+Labels, sorts
// deterministically, and returns an error only when every source failed.
func mergeSeries(results []seriesResult) ([]models.MetricSeries, error) {
	if len(results) == 0 {
		return nil, nil
	}

	var merged []models.MetricSeries
	var failures int
	var lastErr error
	for _, r := range results {
		if r.err != nil {
			failures++
			lastErr = r.err
			continue
		}
		merged = append(merged, r.series...)
	}

	if failures == len(results) {
		return nil, lastErr
	}
	return dedupeAndSortSeries(merged), nil
}

// metricSeriesKey canonicalizes a series' identity as Name + sorted labels.
func metricSeriesKey(s models.MetricSeries) string {
	var b strings.Builder
	b.WriteString(s.Name)
	keys := make([]string, 0, len(s.Labels))
	for k := range s.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteByte(0)
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(s.Labels[k])
	}
	return b.String()
}

// sortMetricSeries orders series by name, then service label, then the full
// canonical label set, giving a stable, reproducible order.
func sortMetricSeries(series []models.MetricSeries) {
	sort.SliceStable(series, func(i, j int) bool {
		a, b := series[i], series[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if as, bs := a.Labels["service"], b.Labels["service"]; as != bs {
			return as < bs
		}
		return metricSeriesKey(a) < metricSeriesKey(b)
	})
}

// dedupeAndSortSeries removes series sharing the same Name+Labels, keeping the
// first occurrence after a deterministic sort.
func dedupeAndSortSeries(series []models.MetricSeries) []models.MetricSeries {
	sortMetricSeries(series)
	out := make([]models.MetricSeries, 0, len(series))
	seen := make(map[string]struct{}, len(series))
	for _, s := range series {
		key := metricSeriesKey(s)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	return out
}

// QueryAnomalies retrieves anomalies across all sources.
func (f *FanoutMetricSource) QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
	return f.fanOutSeries(ctx, func(ctx context.Context, src MetricSource) ([]models.MetricSeries, error) {
		return src.QueryAnomalies(ctx, start, end)
	})
}

// QueryMetricsForService retrieves all metrics for a service across all sources.
func (f *FanoutMetricSource) QueryMetricsForService(ctx context.Context, service string, start, end time.Time) ([]models.MetricSeries, error) {
	return f.fanOutSeries(ctx, func(ctx context.Context, src MetricSource) ([]models.MetricSeries, error) {
		return src.QueryMetricsForService(ctx, service, start, end)
	})
}

// QueryMetric retrieves a specific metric by name. The first non-nil result
// across sources wins (in source order); a partial failure still returns a
// successful source's series, while an error is returned only if every source
// fails.
func (f *FanoutMetricSource) QueryMetric(ctx context.Context, metricName string, start, end time.Time, filters map[string]string) (*models.MetricSeries, error) {
	type result struct {
		series *models.MetricSeries
		err    error
	}

	results := make([]result, len(f.sources))
	var wg sync.WaitGroup
	for i, src := range f.sources {
		wg.Add(1)
		go func(i int, src MetricSource) {
			defer wg.Done()
			series, err := src.QueryMetric(ctx, metricName, start, end, filters)
			results[i] = result{series: series, err: err}
		}(i, src)
	}
	wg.Wait()

	var failures int
	var lastErr error
	for _, r := range results {
		if r.err != nil {
			failures++
			lastErr = r.err
			continue
		}
		if r.series != nil {
			return r.series, nil
		}
	}

	if failures == len(results) && len(results) > 0 {
		return nil, lastErr
	}
	return nil, nil
}

// GetAlerts retrieves current active alerts across all sources, deduplicating
// by labels+state.
func (f *FanoutMetricSource) GetAlerts(ctx context.Context) ([]Alert, error) {
	type result struct {
		alerts []Alert
		err    error
	}

	results := make([]result, len(f.sources))
	var wg sync.WaitGroup
	for i, src := range f.sources {
		wg.Add(1)
		go func(i int, src MetricSource) {
			defer wg.Done()
			alerts, err := src.GetAlerts(ctx)
			results[i] = result{alerts: alerts, err: err}
		}(i, src)
	}
	wg.Wait()

	var merged []Alert
	var failures int
	var lastErr error
	for _, r := range results {
		if r.err != nil {
			failures++
			lastErr = r.err
			continue
		}
		merged = append(merged, r.alerts...)
	}

	if failures == len(results) && len(results) > 0 {
		return nil, lastErr
	}
	return dedupeAndSortAlerts(merged), nil
}

// alertKey canonicalizes an alert's identity as sorted labels + state.
func alertKey(a Alert) string {
	var b strings.Builder
	keys := make([]string, 0, len(a.Labels))
	for k := range a.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteByte(0)
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(a.Labels[k])
	}
	b.WriteByte(0)
	b.WriteString(a.State)
	return b.String()
}

// dedupeAndSortAlerts orders alerts by alertname then canonical identity and
// drops duplicates.
func dedupeAndSortAlerts(alerts []Alert) []Alert {
	sort.SliceStable(alerts, func(i, j int) bool {
		a, b := alerts[i], alerts[j]
		if an, bn := a.Labels["alertname"], b.Labels["alertname"]; an != bn {
			return an < bn
		}
		return alertKey(a) < alertKey(b)
	})

	out := make([]Alert, 0, len(alerts))
	seen := make(map[string]struct{}, len(alerts))
	for _, a := range alerts {
		key := alertKey(a)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, a)
	}
	return out
}

// QueryMetricRange forwards an arbitrary range query to every backing source
// that supports the optional MetricRangeSource extension and merges the
// results. Sources without the extension are skipped. When none support it, an
// honest empty slice is returned.
func (f *FanoutMetricSource) QueryMetricRange(ctx context.Context, query string, start, end time.Time, step string) ([]models.MetricSeries, error) {
	var rangeSources []MetricSource
	for _, src := range f.sources {
		if _, ok := src.(MetricRangeSource); ok {
			rangeSources = append(rangeSources, src)
		}
	}
	if len(rangeSources) == 0 {
		return []models.MetricSeries{}, nil
	}

	results := make([]seriesResult, len(rangeSources))
	var wg sync.WaitGroup
	for i, src := range rangeSources {
		wg.Add(1)
		go func(i int, src MetricSource) {
			defer wg.Done()
			series, err := src.(MetricRangeSource).QueryMetricRange(ctx, query, start, end, step)
			results[i] = seriesResult{series: series, err: err}
		}(i, src)
	}
	wg.Wait()
	return mergeSeries(results)
}

// Compile-time interface conformance.
var (
	_ MetricSource      = (*FanoutMetricSource)(nil)
	_ MetricRangeSource = (*FanoutMetricSource)(nil)
)
