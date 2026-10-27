// Package sources provides data source interfaces for RCA analysis.
package sources

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"

	"go.uber.org/zap"
)

// NamedLogSource pairs a LogSource with a human-readable name (typically the
// cloud/registry identifier, e.g. "aliyun-sls-prod" or "volc-tls-bj"). The
// name is attached to every returned log entry so downstream analysis can tell
// which cloud a line came from.
type NamedLogSource struct {
	Name   string
	Source LogSource
}

// AggregateLogSource fans a log query out across multiple backing LogSources
// (e.g. Alibaba Cloud SLS + Volcengine TLS) concurrently, then merges the
// results into a single time-ordered stream.
//
// Partial-failure tolerance is the key property: if one cloud errors or times
// out, its failure is logged and skipped, and the results from the healthy
// clouds are still returned. The whole RCA does not fail just because one log
// backend is unavailable. An error is returned only when EVERY backing source
// fails (so a total outage is still surfaced).
//
// Each returned entry is tagged with its origin under Labels["cloud_source"].
type AggregateLogSource struct {
	sources []NamedLogSource
	logger  *zap.Logger // optional; nil is safe
}

// SourceLabelKey is the entry label under which the origin source name is set.
const SourceLabelKey = "cloud_source"

// NewAggregateLogSource creates an aggregate over the given named sources. The
// logger may be nil.
func NewAggregateLogSource(logger *zap.Logger, sources ...NamedLogSource) *AggregateLogSource {
	return &AggregateLogSource{sources: sources, logger: logger}
}

// Count returns the number of backing sources.
func (a *AggregateLogSource) Count() int { return len(a.sources) }

// logEntriesResult is the per-source outcome of a fan-out query.
type logEntriesResult struct {
	name    string
	entries []models.LogEntry
	err     error
}

// fanOut runs queryOne against every source concurrently and returns the
// collected per-source results (order not significant).
func (a *AggregateLogSource) fanOut(
	ctx context.Context,
	queryOne func(ctx context.Context, src LogSource) ([]models.LogEntry, error),
) []logEntriesResult {
	results := make([]logEntriesResult, len(a.sources))
	var wg sync.WaitGroup
	for i, ns := range a.sources {
		wg.Add(1)
		go func(i int, ns NamedLogSource) {
			defer wg.Done()
			entries, err := queryOne(ctx, ns.Source)
			results[i] = logEntriesResult{name: ns.Name, entries: entries, err: err}
		}(i, ns)
	}
	wg.Wait()
	return results
}

// merge combines per-source results into a single slice: it tags each entry
// with its source, drops failed sources (logging a warning), sorts the merged
// entries by timestamp descending (newest first), and returns an error only if
// ALL sources failed.
func (a *AggregateLogSource) merge(results []logEntriesResult, op string) ([]models.LogEntry, error) {
	var merged []models.LogEntry
	var failures int
	var lastErr error

	for _, r := range results {
		if r.err != nil {
			failures++
			lastErr = r.err
			if a.logger != nil {
				a.logger.Warn("log source failed; skipping",
					zap.String("op", op),
					zap.String("source", r.name),
					zap.Error(r.err),
				)
			}
			continue
		}
		for i := range r.entries {
			if r.entries[i].Labels == nil {
				r.entries[i].Labels = make(map[string]string)
			}
			r.entries[i].Labels[SourceLabelKey] = r.name
			merged = append(merged, r.entries[i])
		}
	}

	// Every source failed: surface a total outage.
	if failures == len(results) && len(results) > 0 {
		return nil, lastErr
	}

	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Timestamp.After(merged[j].Timestamp)
	})
	return merged, nil
}

// QueryErrors retrieves error logs within the time window across all sources.
func (a *AggregateLogSource) QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
	results := a.fanOut(ctx, func(ctx context.Context, src LogSource) ([]models.LogEntry, error) {
		return src.QueryErrors(ctx, start, end)
	})
	return a.merge(results, "query_errors")
}

// QueryLogsByService retrieves logs for a service across all sources.
func (a *AggregateLogSource) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	results := a.fanOut(ctx, func(ctx context.Context, src LogSource) ([]models.LogEntry, error) {
		return src.QueryLogsByService(ctx, service, start, end)
	})
	return a.merge(results, "query_logs_by_service")
}

// GetLogCount returns the summed count of logs for a service across all
// sources. Failed sources are logged and skipped; an error is returned only if
// every source fails.
func (a *AggregateLogSource) GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error) {
	type countResult struct {
		name  string
		count int64
		err   error
	}
	results := make([]countResult, len(a.sources))
	var wg sync.WaitGroup
	for i, ns := range a.sources {
		wg.Add(1)
		go func(i int, ns NamedLogSource) {
			defer wg.Done()
			c, err := ns.Source.GetLogCount(ctx, service, start, end)
			results[i] = countResult{name: ns.Name, count: c, err: err}
		}(i, ns)
	}
	wg.Wait()

	var total int64
	var failures int
	var lastErr error
	for _, r := range results {
		if r.err != nil {
			failures++
			lastErr = r.err
			if a.logger != nil {
				a.logger.Warn("log source count failed; skipping",
					zap.String("source", r.name),
					zap.Error(r.err),
				)
			}
			continue
		}
		total += r.count
	}
	if failures == len(results) && len(results) > 0 {
		return 0, lastErr
	}
	return total, nil
}
