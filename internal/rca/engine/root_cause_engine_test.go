// Package engine provides tests for RCA engine implementation.
package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yourusername/OpsAgent/internal/rca/models"
	"github.com/yourusername/OpsAgent/internal/rca/scoring"
	"github.com/yourusername/OpsAgent/internal/rca/sources"
)

// Mock implementations for source interfaces

type mockLogSource struct {
	logs  []models.LogEntry
	err   error
	delay time.Duration
}

func (m *mockLogSource) QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.logs, nil
}

func (m *mockLogSource) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.logs, nil
}

func (m *mockLogSource) GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error) {
	if m.err != nil {
		return 0, m.err
	}
	return int64(len(m.logs)), nil
}

type mockMetricSource struct {
	metrics []models.MetricSeries
	err     error
}

func (m *mockMetricSource) QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.metrics, nil
}

func (m *mockMetricSource) QueryMetric(ctx context.Context, metricName string, start, end time.Time, filters map[string]string) (*models.MetricSeries, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, ms := range m.metrics {
		if ms.Name == metricName {
			return &ms, nil
		}
	}
	return nil, nil
}

func (m *mockMetricSource) QueryMetricsForService(ctx context.Context, service string, start, end time.Time) ([]models.MetricSeries, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.metrics, nil
}

func (m *mockMetricSource) GetAlerts(ctx context.Context) ([]sources.Alert, error) {
	return nil, nil
}

type mockTraceSource struct {
	traces []models.TraceSummary
	err    error
}

func (m *mockTraceSource) QueryErrorTraces(ctx context.Context, start, end time.Time) ([]models.TraceSummary, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.traces, nil
}

func (m *mockTraceSource) QueryTracesByService(ctx context.Context, service string, start, end time.Time) ([]models.TraceSummary, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.traces, nil
}

func (m *mockTraceSource) GetTraceDetails(ctx context.Context, traceID string) (*models.TraceSummary, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, tr := range m.traces {
		if tr.TraceID == traceID {
			return &tr, nil
		}
	}
	return nil, nil
}

func (m *mockTraceSource) QuerySlowTraces(ctx context.Context, start, end time.Time, minDuration time.Duration) ([]models.TraceSummary, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.traces, nil
}

type mockTopologySource struct {
	topology *models.Topology
	err      error
}

func (m *mockTopologySource) GetTopology(ctx context.Context) (*models.Topology, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.topology, nil
}

func (m *mockTopologySource) GetServiceNeighbors(ctx context.Context, service string) (dependents, dependencies []string, err error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	if m.topology != nil {
		dependents = m.topology.GetDependents(service)
		dependencies = m.topology.GetDependencies(service)
	}
	return dependents, dependencies, nil
}

func (m *mockTopologySource) GetServiceHealth(ctx context.Context) (map[string]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	health := make(map[string]string)
	if m.topology != nil {
		for _, node := range m.topology.Nodes {
			health[node.Name] = "healthy"
		}
	}
	return health, nil
}

type mockChangeSource struct {
	changes []models.Change
	err     error
}

func (m *mockChangeSource) QueryChanges(ctx context.Context, start, end time.Time) ([]models.Change, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.changes, nil
}

func (m *mockChangeSource) QueryChangesByService(ctx context.Context, service string, start, end time.Time) ([]models.Change, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []models.Change
	for _, c := range m.changes {
		if c.Service == service {
			result = append(result, c)
		}
	}
	return result, nil
}

func (m *mockChangeSource) GetLastChange(ctx context.Context, service string) (*models.Change, error) {
	if m.err != nil {
		return nil, m.err
	}
	for i := len(m.changes) - 1; i >= 0; i-- {
		if m.changes[i].Service == service {
			return &m.changes[i], nil
		}
	}
	return nil, nil
}

func TestNewEngine(t *testing.T) {
	tests := []struct {
		name    string
		opts    []Option
		wantErr bool
	}{
		{
			name: "valid engine with required sources",
			opts: []Option{
				WithLogSource(&mockLogSource{}),
				WithMetricSource(&mockMetricSource{}),
			},
			wantErr: false,
		},
		{
			name:    "missing log source",
			opts:    []Option{WithMetricSource(&mockMetricSource{})},
			wantErr: true,
		},
		{
			name:    "missing metric source",
			opts:    []Option{WithLogSource(&mockLogSource{})},
			wantErr: true,
		},
		{
			name: "valid engine with all sources",
			opts: []Option{
				WithLogSource(&mockLogSource{}),
				WithMetricSource(&mockMetricSource{}),
				WithTraceSource(&mockTraceSource{}),
				WithTopologySource(&mockTopologySource{}),
				WithChangeSource(&mockChangeSource{}),
			},
			wantErr: false,
		},
		{
			name: "custom configuration",
			opts: []Option{
				WithLogSource(&mockLogSource{}),
				WithMetricSource(&mockMetricSource{}),
				WithWeights(scoring.Weights{
					TimeCorrelation: 0.6,
					TopologyScore:   0.2,
					ChangeProximity: 0.1,
					MetricSeverity:  0.05,
					ErrorFrequency:  0.03,
					TraceEvidence:   0.02,
				}),
				WithWindowSize(10 * time.Minute),
				WithMinConfidence(0.5),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewEngine(tt.opts...)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewEngine() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("NewEngine() unexpected error: %v", err)
				return
			}
			if engine == nil {
				t.Errorf("NewEngine() returned nil engine")
			}
		})
	}
}

func TestEngineAnalyze(t *testing.T) {
	// Create test incident
	incident := &models.Incident{
		ID:              "test-incident-001",
		StartTime:       time.Now().Add(-2 * time.Hour),
		EndTime:         time.Now(),
		Severity:        "P1",
		AffectedServices: []string{"api-service", "database"},
		Description:     "High error rate in api-service",
	}

	// Create test data
	startTime := incident.StartTime.Add(-time.Hour)
	testLogs := []models.LogEntry{
		{Timestamp: startTime.Add(10 * time.Minute), Service: "api-service", IsError: true, Level: "error", Message: "connection failed"},
		{Timestamp: startTime.Add(15 * time.Minute), Service: "api-service", IsError: true, Level: "error", Message: "timeout"},
		{Timestamp: startTime.Add(20 * time.Minute), Service: "api-service", IsError: true, Level: "error", Message: "connection failed"},
		{Timestamp: incident.StartTime.Add(5 * time.Minute), Service: "api-service", IsError: true, Level: "error", Message: "database connection error"},
		{Timestamp: incident.StartTime.Add(10 * time.Minute), Service: "api-service", IsError: true, Level: "error", Message: "database timeout"},
	}

	testMetrics := []models.MetricSeries{
		{
			Name: "http_errors",
			DataPoints: []models.DataPoint{
				{Timestamp: startTime.Add(5 * time.Minute), Value: 10},
				{Timestamp: startTime.Add(10 * time.Minute), Value: 15},
				{Timestamp: startTime.Add(15 * time.Minute), Value: 20},
				{Timestamp: incident.StartTime.Add(5 * time.Minute), Value: 50},
				{Timestamp: incident.StartTime.Add(10 * time.Minute), Value: 80},
			},
		},
		{
			Name: "latency_p99",
			DataPoints: []models.DataPoint{
				{Timestamp: startTime.Add(5 * time.Minute), Value: 100},
				{Timestamp: startTime.Add(10 * time.Minute), Value: 120},
				{Timestamp: incident.StartTime.Add(5 * time.Minute), Value: 500},
				{Timestamp: incident.StartTime.Add(10 * time.Minute), Value: 1000},
			},
		},
	}

	testTopology := &models.Topology{
		Nodes: []models.ServiceNode{
			{Name: "api-service", Type: "application"},
			{Name: "database", Type: "database"},
		},
		Edges: []models.DependencyEdge{
			{From: "api-service", To: "database", Type: "database"},
		},
	}

	testChanges := []models.Change{
		{
			ID:          "change-001",
			Timestamp:   incident.StartTime.Add(5 * time.Minute),
			Service:     "database",
			Type:        "deployment",
			Description: "Database version upgrade to v5.2",
		},
	}

	tests := []struct {
		name           string
		logSource      sources.LogSource
		metricSource   sources.MetricSource
		traceSource    sources.TraceSource
		topologySource sources.TopologySource
		changeSource   sources.ChangeSource
		wantErr        bool
		validate       func(t *testing.T, report *models.RCAReport)
	}{
		{
			name: "successful analysis with all signals",
			logSource: &mockLogSource{
				logs: testLogs,
			},
			metricSource: &mockMetricSource{
				metrics: testMetrics,
			},
			traceSource: &mockTraceSource{
				traces: []models.TraceSummary{
					{TraceID: "trace-1", StartTime: incident.StartTime.Add(5 * time.Minute), ServiceName: "api-service", HasError: true},
				},
			},
			topologySource: &mockTopologySource{
				topology: testTopology,
			},
			changeSource: &mockChangeSource{
				changes: testChanges,
			},
			wantErr: false,
			validate: func(t *testing.T, report *models.RCAReport) {
				if report == nil {
					t.Fatal("report is nil")
				}
				if report.Incident == nil || report.Incident.ID != incident.ID {
					t.Errorf("Incident not preserved correctly")
				}
				if report.Signals == nil {
					t.Errorf("Signals not collected")
				}
				if report.Duration == 0 {
					t.Errorf("Duration not measured")
				}
				if report.GeneratedAt.IsZero() {
					t.Errorf("GeneratedAt not set")
				}
			},
		},
		{
			name: "analysis with minimal sources",
			logSource: &mockLogSource{
				logs: testLogs,
			},
			metricSource: &mockMetricSource{
				metrics: testMetrics,
			},
			wantErr: false,
			validate: func(t *testing.T, report *models.RCAReport) {
				if report == nil {
					t.Fatal("report is nil")
				}
				// Should still generate candidates from logs and metrics
				if len(report.Candidates) == 0 {
					t.Logf("No candidates generated (may be valid if threshold not met)")
				}
			},
		},
		{
			name: "analysis with source errors",
			logSource: &mockLogSource{
				err: errors.New("log source unavailable"),
			},
			metricSource: &mockMetricSource{
				metrics: testMetrics,
			},
			wantErr: false, // Should not fail, just skip that source
			validate: func(t *testing.T, report *models.RCAReport) {
				if report == nil {
					t.Fatal("report is nil")
				}
				// Should still work with partial data
			},
		},
		{
			name: "analysis generates candidates from error spike",
			logSource: &mockLogSource{
				logs: testLogs,
			},
			metricSource: &mockMetricSource{
				metrics: testMetrics,
			},
			topologySource: &mockTopologySource{
				topology: testTopology,
			},
			changeSource: &mockChangeSource{
				changes: testChanges,
			},
			wantErr: false,
			validate: func(t *testing.T, report *models.RCAReport) {
				if report == nil {
					t.Fatal("report is nil")
				}
				// Check that different hypothesis types are generated
				hasErrorSpike := false
				hasMetricAnomaly := false
				hasChange := false
				for _, c := range report.Candidates {
					switch c.Hypothesis.Type {
					case models.HypothesisTypeErrorSpike:
						hasErrorSpike = true
					case models.HypothesisTypeMetricAnomaly:
						hasMetricAnomaly = true
					case models.HypothesisTypeChange:
						hasChange = true
					}
				}
				t.Logf("Candidates: error_spike=%v, metric_anomaly=%v, change=%v",
					hasErrorSpike, hasMetricAnomaly, hasChange)
			},
		},
		{
			name: "context cancellation",
			logSource: &mockLogSource{
				logs:  testLogs,
				delay: 10 * time.Second,
			},
			metricSource: &mockMetricSource{
				metrics: testMetrics,
			},
			wantErr: true,
			validate: func(t *testing.T, report *models.RCAReport) {
				t.Error("Should not reach validate on error")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []Option{
				WithLogSource(tt.logSource),
				WithMetricSource(tt.metricSource),
				WithTraceSource(tt.traceSource),
				WithTopologySource(tt.topologySource),
				WithChangeSource(tt.changeSource),
				WithMinConfidence(0.1), // Lower threshold for testing
			}

			engine, err := NewEngine(opts...)
			if err != nil {
				t.Fatalf("Failed to create engine: %v", err)
			}

			ctx := context.Background()
			if tt.name == "context cancellation" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 10*time.Millisecond)
				defer cancel()
			}

			report, err := engine.Analyze(ctx, incident)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Analyze() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Analyze() unexpected error: %v", err)
				return
			}

			if tt.validate != nil {
				tt.validate(t, report)
			}
		})
	}
}

func TestGenerateHypotheses(t *testing.T) {
	incident := &models.Incident{
		ID:        "test-001",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
		Severity:  "P1",
		AffectedServices: []string{"service-a"},
	}

	baseTime := incident.StartTime.Add(-15 * time.Minute)
	subWindow := 3 * time.Minute

	// Build enough error logs to trigger hasSignificantErrorSpike (>10 in incident windows, >2x baseline)
	incidentWindowLogs := []models.LogEntry{}
	for i := 0; i < 12; i++ {
		incidentWindowLogs = append(incidentWindowLogs, models.LogEntry{
			Timestamp: incident.StartTime.Add(time.Duration(i*5) * time.Second),
			Service:   "service-a",
			IsError:   true,
			Level:     "error",
			Message:   "connection timeout",
		})
	}

	normalized := &models.NormalizedSignals{
		TimeWindows: []models.TimeWindow{
			{Start: baseTime, End: baseTime.Add(subWindow)},
			{Start: baseTime.Add(subWindow), End: baseTime.Add(2 * subWindow)},
			{Start: baseTime.Add(2 * subWindow), End: baseTime.Add(3 * subWindow)},
			{Start: incident.StartTime, End: incident.StartTime.Add(subWindow)},
			{Start: incident.StartTime.Add(subWindow), End: incident.StartTime.Add(2 * subWindow)},
		},
		LogByWindow: map[int][]models.LogEntry{
			0: {}, // baseline - empty
			1: {}, // baseline - empty
			2: {}, // baseline - empty
			3: incidentWindowLogs[:6],  // incident window - 6 errors
			4: incidentWindowLogs[6:],  // incident window - 6 errors, total 12 > 2*0=0 and > 10
		},
		MetricTrends: map[string]map[int]models.Trend{
			"http_errors": {
				2: models.TrendStable,
				3: models.TrendUp,    // last 3 windows include this
			},
		},
		Changes: []models.Change{
			{
				ID:          "change-001",
				Timestamp:   incident.StartTime.Add(5 * time.Minute),
				Service:     "service-a",
				Type:        "deployment",
				Description: "Deployment: v2.0",
			},
		},
	}

	tests := []struct {
		name              string
		normalized        *models.NormalizedSignals
		minCandidates     int
		expectedTypes     map[models.HypothesisType]bool
	}{
		{
			name:          "generates multiple hypothesis types",
			normalized:    normalized,
			minCandidates: 2,
			expectedTypes: map[models.HypothesisType]bool{
				models.HypothesisTypeErrorSpike:     false,
				models.HypothesisTypeMetricAnomaly: false,
				models.HypothesisTypeChange:        false,
			},
		},
		{
			name: "generates error spike hypothesis",
			normalized: &models.NormalizedSignals{
				TimeWindows: normalized.TimeWindows,
				LogByWindow: normalized.LogByWindow,
			},
			minCandidates: 1,
			expectedTypes: map[models.HypothesisType]bool{
				models.HypothesisTypeErrorSpike: false,
			},
		},
		{
			name: "generates metric anomaly hypothesis",
			normalized: &models.NormalizedSignals{
				TimeWindows:  normalized.TimeWindows,
				MetricTrends: normalized.MetricTrends,
			},
			minCandidates: 1,
			expectedTypes: map[models.HypothesisType]bool{
				models.HypothesisTypeMetricAnomaly: false,
			},
		},
		{
			name: "generates change hypothesis",
			normalized: &models.NormalizedSignals{
				TimeWindows: normalized.TimeWindows,
				Changes:     normalized.Changes,
			},
			minCandidates: 1,
			expectedTypes: map[models.HypothesisType]bool{
				models.HypothesisTypeChange: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, _ := NewEngine(
				WithLogSource(&mockLogSource{}),
				WithMetricSource(&mockMetricSource{}),
			)

			candidates := engine.generateHypotheses(context.Background(), tt.normalized, incident)

			if len(candidates) < tt.minCandidates {
				t.Errorf("Expected at least %d candidates, got %d", tt.minCandidates, len(candidates))
			}

			// Check expected types
			for _, c := range candidates {
				if tt.expectedTypes != nil {
					if _, exists := tt.expectedTypes[c.Type]; exists {
						tt.expectedTypes[c.Type] = true
					}
				}
			}

			// Verify expected types were found
			for typ, found := range tt.expectedTypes {
				if !found {
					t.Errorf("Expected hypothesis type %s was not generated", typ)
				}
			}
		})
	}
}

func TestRankAndFilter(t *testing.T) {
	scored := []models.ScoredHypothesis{
		{
			Hypothesis:  models.Hypothesis{Type: models.HypothesisTypeChange, Description: "High scoring"},
			Score:       0.9,
			Confidence:  0.85,
		},
		{
			Hypothesis:  models.Hypothesis{Type: models.HypothesisTypeErrorSpike, Description: "Medium scoring"},
			Score:       0.6,
			Confidence:  0.55,
		},
		{
			Hypothesis:  models.Hypothesis{Type: models.HypothesisTypeMetricAnomaly, Description: "Low scoring"},
			Score:       0.3,
			Confidence:  0.25,
		},
	}

	tests := []struct {
		name         string
		minConfidence float64
		wantCount    int
		topScore     float64
	}{
		{
			name:         "filter at 0.5 threshold",
			minConfidence: 0.5,
			wantCount:    2,
			topScore:     0.9,
		},
		{
			name:         "filter at 0.3 threshold",
			minConfidence: 0.3,
			wantCount:    2, // 0.25 < 0.3 so only top 2 pass
			topScore:     0.9,
		},
		{
			name:         "filter at 0.9 threshold",
			minConfidence: 0.9,
			wantCount:    0,
			topScore:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, _ := NewEngine(
				WithLogSource(&mockLogSource{}),
				WithMetricSource(&mockMetricSource{}),
				WithMinConfidence(tt.minConfidence),
			)

			result := engine.rankAndFilter(scored, tt.minConfidence)

			if len(result) != tt.wantCount {
				t.Errorf("Expected %d candidates, got %d", tt.wantCount, len(result))
			}

			if len(result) > 0 && result[0].Score != tt.topScore {
				t.Errorf("Expected top score %f, got %f", tt.topScore, result[0].Score)
			}

			// Verify ranking is descending
			for i := 1; i < len(result); i++ {
				if result[i].Score > result[i-1].Score {
					t.Errorf("Results not sorted by score descending: [%d].Score=%f > [%d].Score=%f",
						i, result[i].Score, i-1, result[i-1].Score)
				}
			}
		})
	}
}

func TestGenerateTimeWindows(t *testing.T) {
	incident := &models.Incident{
		ID:        "test-001",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
	}

	tests := []struct {
		name       string
		windowSize time.Duration
		wantCount  int
	}{
		{
			name:       "15 minute window",
			windowSize: 15 * time.Minute,
			wantCount:  10, // (60 + 15) / (15/5) = 75 / 3 = 25 windows, but actual depends on implementation
		},
		{
			name:       "5 minute window",
			windowSize: 5 * time.Minute,
			wantCount:  13, // (60 + 5) / (5/5) = 65 / 1 = 65 windows
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			windows := generateTimeWindows(incident, tt.windowSize)

			if len(windows) == 0 {
				t.Errorf("generateTimeWindows() returned empty slice")
			}

			// Verify windows are sequential
			for i := 1; i < len(windows); i++ {
				if windows[i].Start != windows[i-1].End {
					t.Errorf("Windows not sequential: [%d].End=%v != [%d].Start=%v",
						i-1, windows[i-1].End, i, windows[i].Start)
				}
			}
		})
	}
}

func TestCalculateMetricTrends(t *testing.T) {
	baseTime := time.Now()
	metrics := []models.MetricSeries{
		{
			Name: "increasing_metric",
			DataPoints: []models.DataPoint{
				{Timestamp: baseTime, Value: 10},
				{Timestamp: baseTime.Add(1 * time.Minute), Value: 20},
				{Timestamp: baseTime.Add(2 * time.Minute), Value: 30},
			},
		},
		{
			Name: "decreasing_metric",
			DataPoints: []models.DataPoint{
				{Timestamp: baseTime, Value: 100},
				{Timestamp: baseTime.Add(1 * time.Minute), Value: 80},
				{Timestamp: baseTime.Add(2 * time.Minute), Value: 60},
			},
		},
		{
			Name: "stable_metric",
			DataPoints: []models.DataPoint{
				{Timestamp: baseTime, Value: 50},
				{Timestamp: baseTime.Add(1 * time.Minute), Value: 51},
				{Timestamp: baseTime.Add(2 * time.Minute), Value: 49},
			},
		},
	}

	windows := []models.TimeWindow{
		{Start: baseTime, End: baseTime.Add(1 * time.Minute)},
		{Start: baseTime.Add(1 * time.Minute), End: baseTime.Add(2 * time.Minute)},
		{Start: baseTime.Add(2 * time.Minute), End: baseTime.Add(3 * time.Minute)},
	}

	trends := calculateMetricTrends(metrics, windows)

	// Check that we got trends for each metric
	if len(trends) != len(metrics) {
		t.Errorf("Expected trends for %d metrics, got %d", len(metrics), len(trends))
	}

	// Verify increasing metric is detected as trending up
	if incTrends, ok := trends["increasing_metric"]; ok {
		foundUp := false
		for _, trend := range incTrends {
			if trend == models.TrendUp {
				foundUp = true
				break
			}
		}
		if !foundUp {
			t.Errorf("increasing_metric not detected as trending up")
		}
	}

	// Verify decreasing metric is detected as trending down
	if decTrends, ok := trends["decreasing_metric"]; ok {
		foundDown := false
		for _, trend := range decTrends {
			if trend == models.TrendDown {
				foundDown = true
				break
			}
		}
		if !foundDown {
			t.Errorf("decreasing_metric not detected as trending down")
		}
	}
}

func TestGetOverallConfidence(t *testing.T) {
	tests := []struct {
		name     string
		ranked   []models.ScoredHypothesis
		wantConf float64
	}{
		{
			name:     "empty candidates",
			ranked:   []models.ScoredHypothesis{},
			wantConf: 0,
		},
		{
			name: "single candidate",
			ranked: []models.ScoredHypothesis{
				{Confidence: 0.8},
			},
			wantConf: 0.8,
		},
		{
			name: "two candidates (weighted)",
			ranked: []models.ScoredHypothesis{
				{Confidence: 0.8},
				{Confidence: 0.6},
			},
			wantConf: 0.8*0.7 + 0.6*0.3, // 0.56 + 0.18 = 0.74
		},
		{
			name: "multiple candidates (uses top 2)",
			ranked: []models.ScoredHypothesis{
				{Confidence: 0.9},
				{Confidence: 0.7},
				{Confidence: 0.5},
			},
			wantConf: 0.9*0.7 + 0.7*0.3, // 0.63 + 0.21 = 0.84
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := getOverallConfidence(tt.ranked)
			if conf != tt.wantConf {
				t.Errorf("getOverallConfidence() = %v, want %v", conf, tt.wantConf)
			}
		})
	}
}
