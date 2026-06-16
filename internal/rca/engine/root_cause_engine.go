// Package engine provides Root Cause Analysis (RCA) engine implementation.
package engine

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/yourusername/OpsAgent/internal/rca/models"
	"github.com/yourusername/OpsAgent/internal/rca/scoring"
	"github.com/yourusername/OpsAgent/internal/rca/sources"
)

// Engine is the RCA engine that orchestrates root cause analysis.
type Engine struct {
	logSource      sources.LogSource
	metricSource   sources.MetricSource
	traceSource    sources.TraceSource
	topologySource sources.TopologySource
	changeSource   sources.ChangeSource
	weights        scoring.Weights
	windowSize     time.Duration
	minConfidence  float64
}

// NewEngine creates a new RCA engine.
func NewEngine(opts ...Option) (*Engine, error) {
	e := &Engine{
		weights:       scoring.DefaultWeights(),
		windowSize:    15 * time.Minute,
		minConfidence: 0.3,
	}

	for _, opt := range opts {
		opt(e)
	}

	if e.logSource == nil {
		return nil, fmt.Errorf("log source is required")
	}
	if e.metricSource == nil {
		return nil, fmt.Errorf("metric source is required")
	}

	return e, nil
}

// Option configures the Engine.
type Option func(*Engine)

func WithLogSource(s sources.LogSource) Option {
	return func(e *Engine) { e.logSource = s }
}

func WithMetricSource(s sources.MetricSource) Option {
	return func(e *Engine) { e.metricSource = s }
}

func WithTraceSource(s sources.TraceSource) Option {
	return func(e *Engine) { e.traceSource = s }
}

func WithTopologySource(s sources.TopologySource) Option {
	return func(e *Engine) { e.topologySource = s }
}

func WithChangeSource(s sources.ChangeSource) Option {
	return func(e *Engine) { e.changeSource = s }
}

func WithWeights(w scoring.Weights) Option {
	return func(e *Engine) { e.weights = w }
}

func WithWindowSize(d time.Duration) Option {
	return func(e *Engine) { e.windowSize = d }
}

func WithMinConfidence(c float64) Option {
	return func(e *Engine) { e.minConfidence = c }
}

// Analyze performs root cause analysis for the given incident.
func (e *Engine) Analyze(ctx context.Context, incident *models.Incident) (*models.RCAReport, error) {
	startTime := time.Now()

	// Step 1: Gather signals in parallel
	signals, err := e.gatherSignals(ctx, incident)
	if err != nil {
		return nil, fmt.Errorf("failed to gather signals: %w", err)
	}

	// Step 2: Normalize time windows (trend-based alignment)
	normalized := e.normalizeTimeWindows(signals, incident)

	// Step 3: Generate and score hypotheses
	candidates := e.generateHypotheses(ctx, normalized, incident)
	scores := e.scoreCandidates(ctx, candidates, normalized, incident)

	// Step 4: Rank by confidence and filter
	ranked := e.rankAndFilter(scores, e.minConfidence)

	// Step 5: Generate report
	report := &models.RCAReport{
		Incident:        incident,
		Signals:         signals,
		Candidates:      ranked,
		TopCandidate:    getTopCandidate(ranked),
		Confidence:      getOverallConfidence(ranked),
		AnalysisWindow:  getAnalysisWindow(incident, e.windowSize),
		Duration:        time.Since(startTime),
		GeneratedAt:     time.Now(),
	}

	return report, nil
}

// gatherSignals collects data from all sources in parallel.
func (e *Engine) gatherSignals(ctx context.Context, incident *models.Incident) (*models.Signals, error) {
	signals := &models.Signals{}
	errChan := make(chan error, 5)
	doneChan := make(chan struct{}, 5)

	// Collect logs
	go func() {
		logs, err := e.logSource.QueryErrors(ctx, incident.StartTime.Add(-e.windowSize), incident.EndTime)
		if err == nil {
			signals.Logs = logs
		}
		doneChan <- struct{}{}
	}()

	// Collect metrics
	go func() {
		metrics, err := e.metricSource.QueryAnomalies(ctx, incident.StartTime.Add(-e.windowSize), incident.EndTime)
		if err == nil {
			signals.Metrics = metrics
		}
		doneChan <- struct{}{}
	}()

	// Collect traces
	if e.traceSource != nil {
		go func() {
			traces, err := e.traceSource.QueryErrorTraces(ctx, incident.StartTime, incident.EndTime)
			if err == nil {
				signals.Traces = traces
			}
			doneChan <- struct{}{}
		}()
	} else {
		doneChan <- struct{}{}
	}

	// Collect topology
	if e.topologySource != nil {
		go func() {
			topo, err := e.topologySource.GetTopology(ctx)
			if err == nil {
				signals.Topology = topo
			}
			doneChan <- struct{}{}
		}()
	} else {
		doneChan <- struct{}{}
	}

	// Collect changes
	if e.changeSource != nil {
		go func() {
			changes, err := e.changeSource.QueryChanges(ctx, incident.StartTime.Add(-e.windowSize), incident.EndTime)
			if err == nil {
				signals.Changes = changes
			}
			doneChan <- struct{}{}
		}()
	} else {
		doneChan <- struct{}{}
	}

	// Wait for all to complete
	for i := 0; i < 5; i++ {
		select {
		case <-doneChan:
		case <-ctx.Done():
			return signals, ctx.Err()
		case err := <-errChan:
			return signals, err
		}
	}

	return signals, nil
}

// normalizeTimeWindows performs trend-based time alignment.
func (e *Engine) normalizeTimeWindows(signals *models.Signals, incident *models.Incident) *models.NormalizedSignals {
	normalized := &models.NormalizedSignals{
		TimeWindows: generateTimeWindows(incident, e.windowSize),
	}
	normalized.LogByWindow = bucketLogsByTime(signals.Logs, normalized.TimeWindows)
	normalized.MetricTrends = calculateMetricTrends(signals.Metrics, normalized.TimeWindows)
	normalized.TraceByWindow = bucketTracesByTime(signals.Traces, normalized.TimeWindows)
	normalized.Changes = signals.Changes
	normalized.Topology = signals.Topology
	return normalized
}

func generateTimeWindows(incident *models.Incident, windowSize time.Duration) []models.TimeWindow {
	windows := []models.TimeWindow{}
	subWindow := windowSize / 5
	start := incident.StartTime.Add(-windowSize)
	end := incident.EndTime

	for t := start; t.Before(end); t = t.Add(subWindow) {
		windows = append(windows, models.TimeWindow{
			Start: t,
			End:   t.Add(subWindow),
		})
	}
	return windows
}

func bucketLogsByTime(logs []models.LogEntry, windows []models.TimeWindow) map[int][]models.LogEntry {
	result := make(map[int][]models.LogEntry)
	for _, log := range logs {
		for i, window := range windows {
			if (log.Timestamp.Equal(window.Start) || log.Timestamp.After(window.Start)) &&
				log.Timestamp.Before(window.End) {
				result[i] = append(result[i], log)
				break
			}
		}
	}
	return result
}

func bucketTracesByTime(traces []models.TraceSummary, windows []models.TimeWindow) map[int][]models.TraceSummary {
	result := make(map[int][]models.TraceSummary)
	for _, trace := range traces {
		for i, window := range windows {
			if (trace.StartTime.Equal(window.Start) || trace.StartTime.After(window.Start)) &&
				trace.StartTime.Before(window.End) {
				result[i] = append(result[i], trace)
				break
			}
		}
	}
	return result
}

func calculateMetricTrends(metrics []models.MetricSeries, windows []models.TimeWindow) map[string]map[int]models.Trend {
	result := make(map[string]map[int]models.Trend)
	for _, metric := range metrics {
		trends := make(map[int]models.Trend)
		result[metric.Name] = trends
		for i := 0; i < len(windows)-1; i++ {
			avg1 := getAverageMetricValue(metric, windows[i])
			avg2 := getAverageMetricValue(metric, windows[i+1])
			change := 0.0
			if avg1 != 0 {
				change = (avg2 - avg1) / avg1
			}
			switch {
			case change > 0.1:
				trends[i] = models.TrendUp
			case change < -0.1:
				trends[i] = models.TrendDown
			default:
				trends[i] = models.TrendStable
			}
		}
	}
	return result
}

func getAverageMetricValue(metric models.MetricSeries, window models.TimeWindow) float64 {
	sum := 0.0
	count := 0
	for _, point := range metric.DataPoints {
		if (point.Timestamp.Equal(window.Start) || point.Timestamp.After(window.Start)) &&
			point.Timestamp.Before(window.End) {
			sum += point.Value
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// generateHypotheses creates candidate root cause hypotheses.
func (e *Engine) generateHypotheses(ctx context.Context, normalized *models.NormalizedSignals, incident *models.Incident) []models.Hypothesis {
	candidates := []models.Hypothesis{}

	// Error spike hypotheses
	for service, logs := range groupLogsByService(normalized.LogByWindow) {
		if hasSignificantErrorSpike(logs, normalized, incident) {
			candidates = append(candidates, models.Hypothesis{
				Type:        models.HypothesisTypeErrorSpike,
				Service:     service,
				Description: fmt.Sprintf("Service %s experienced a significant error spike", service),
				Evidence:    []string{fmt.Sprintf("%d error logs in affected window", len(logs))},
			})
		}
	}

	// Metric anomaly hypotheses
	for metricName, trends := range normalized.MetricTrends {
		if hasAnomalousTrend(trends, normalized, incident) {
			candidates = append(candidates, models.Hypothesis{
				Type:        models.HypothesisTypeMetricAnomaly,
				Description: fmt.Sprintf("Metric %s showed anomalous trend", metricName),
				Metric:      metricName,
				Evidence:    describeTrend(trends),
			})
		}
	}

	// Change hypotheses
	if len(normalized.Changes) > 0 {
		for _, change := range normalized.Changes {
			if isChangeRelevant(change, incident) {
				candidates = append(candidates, models.Hypothesis{
					Type:        models.HypothesisTypeChange,
					Service:     change.Service,
					Description: fmt.Sprintf("Recent change detected: %s", change.Description),
					ChangeID:    change.ID,
					Evidence:    []string{change.Description, change.Timestamp.Format(time.RFC3339)},
				})
			}
		}
	}

	return candidates
}

func groupLogsByService(logByWindow map[int][]models.LogEntry) map[string][]models.LogEntry {
	result := make(map[string][]models.LogEntry)
	for _, logs := range logByWindow {
		for _, log := range logs {
			result[log.Service] = append(result[log.Service], log)
		}
	}
	return result
}

func hasSignificantErrorSpike(logs []models.LogEntry, normalized *models.NormalizedSignals, incident *models.Incident) bool {
	incidentCount := 0
	baselineCount := 0
	for i, window := range normalized.TimeWindows {
		isIncidentWindow := window.End.After(incident.StartTime) && window.Start.Before(incident.EndTime)
		if isIncidentWindow {
			incidentCount += len(normalized.LogByWindow[i])
		} else {
			baselineCount += len(normalized.LogByWindow[i])
		}
	}
	return incidentCount > baselineCount*2 && incidentCount > 10
}

func hasAnomalousTrend(trends map[int]models.Trend, normalized *models.NormalizedSignals, incident *models.Incident) bool {
	startIdx := len(normalized.TimeWindows) - 3
	if startIdx < 0 {
		startIdx = 0
	}
	for i := startIdx; i < len(normalized.TimeWindows); i++ {
		if trend, ok := trends[i]; ok && (trend == models.TrendUp || trend == models.TrendDown) {
			return true
		}
	}
	return false
}

func describeTrend(trends map[int]models.Trend) []string {
	var descriptions []string
	for i, trend := range trends {
		switch trend {
		case models.TrendUp:
			descriptions = append(descriptions, fmt.Sprintf("Window %d: trending up", i))
		case models.TrendDown:
			descriptions = append(descriptions, fmt.Sprintf("Window %d: trending down", i))
		}
	}
	return descriptions
}

func isChangeRelevant(change models.Change, incident *models.Incident) bool {
	return !change.Timestamp.Before(incident.StartTime.Add(-15*time.Minute)) &&
		!change.Timestamp.After(incident.EndTime)
}

// scoreCandidates applies multi-dimensional scoring.
func (e *Engine) scoreCandidates(ctx context.Context, candidates []models.Hypothesis, normalized *models.NormalizedSignals, incident *models.Incident) []models.ScoredHypothesis {
	scorer := scoring.NewMultiDimensionalScorer(e.weights)
	scored := make([]models.ScoredHypothesis, 0, len(candidates))
	for _, candidate := range candidates {
		score := scorer.Score(ctx, &candidate, normalized, incident)
		scored = append(scored, models.ScoredHypothesis{
			Hypothesis: candidate,
			Score:      score.Total,
			Confidence: score.Confidence,
			Breakdown:  score.Breakdown,
		})
	}
	return scored
}

// rankAndFilter sorts candidates by score and filters by confidence threshold.
func (e *Engine) rankAndFilter(scored []models.ScoredHypothesis, minConfidence float64) []models.ScoredHypothesis {
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	filtered := make([]models.ScoredHypothesis, 0)
	for _, s := range scored {
		if s.Confidence >= minConfidence {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func getTopCandidate(ranked []models.ScoredHypothesis) *models.ScoredHypothesis {
	if len(ranked) == 0 {
		return nil
	}
	return &ranked[0]
}

func getOverallConfidence(ranked []models.ScoredHypothesis) float64 {
	if len(ranked) == 0 {
		return 0
	}
	if len(ranked) >= 2 {
		return ranked[0].Confidence*0.7 + ranked[1].Confidence*0.3
	}
	return ranked[0].Confidence
}

func getAnalysisWindow(incident *models.Incident, windowSize time.Duration) models.TimeWindow {
	return models.TimeWindow{
		Start: incident.StartTime.Add(-windowSize),
		End:   incident.EndTime,
	}
}
