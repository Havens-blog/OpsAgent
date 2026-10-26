// Package scoring provides tests for multi-dimensional scoring.
package scoring

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

func TestMultiDimensionalScorer(t *testing.T) {
	weights := DefaultWeights()
	scorer := NewMultiDimensionalScorer(weights)

	incident := &models.Incident{
		ID:        "test-001",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
		Severity:  "P0",
		AffectedServices: []string{"service-a"},
	}

	normalized := &models.NormalizedSignals{
		TimeWindows: generateTestWindows(incident.StartTime, incident.EndTime),
		Topology: &models.Topology{
			Nodes: []models.ServiceNode{
				{Name: "service-a", Type: "application"},
				{Name: "service-b", Type: "database"},
			},
			Edges: []models.DependencyEdge{
				{From: "service-a", To: "service-b", Type: "database"},
			},
		},
		LogByWindow: map[int][]models.LogEntry{
			0: {}, 1: {}, 2: {},
			3: {
				{Timestamp: incident.StartTime.Add(30 * time.Minute), Service: "service-a", IsError: true, Level: "error"},
				{Timestamp: incident.StartTime.Add(35 * time.Minute), Service: "service-a", IsError: true, Level: "error"},
			},
			4: {},
		},
		MetricTrends: map[string]map[int]models.Trend{
			"http_errors": {
				2: models.TrendUp,
				3: models.TrendUp,
			},
		},
		Changes: []models.Change{
			{
				ID:          "change-001",
				Timestamp:   incident.StartTime.Add(10 * time.Minute),
				Service:     "service-a",
				Type:        "deployment",
				Description: "Deployment: image-v2.0",
			},
		},
	}

	tests := []struct {
		name       string
		hypothesis models.Hypothesis
		minScore   float64
	}{
		{
			name: "error spike hypothesis",
			hypothesis: models.Hypothesis{
				Type:        models.HypothesisTypeErrorSpike,
				Service:     "service-a",
				Description: "Service service-a experienced error spike",
			},
			minScore: 0.1,
		},
		{
			name: "metric anomaly hypothesis",
			hypothesis: models.Hypothesis{
				Type:        models.HypothesisTypeMetricAnomaly,
				Metric:      "http_errors",
				Description: "Metric http_errors showed anomalous trend",
			},
			minScore: 0.1,
		},
		{
			name: "change hypothesis",
			hypothesis: models.Hypothesis{
				Type:        models.HypothesisTypeChange,
				Service:     "service-a",
				ChangeID:    "change-001",
				Description: "Recent change detected",
			},
			minScore: 0.05,
		},
		{
			name: "topology dependency hypothesis",
			hypothesis: models.Hypothesis{
				Type:        models.HypothesisTypeDependency,
				Service:     "service-b",
				Description: "Dependency service-b may be causing issues",
			},
			minScore: 0.1,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.Score(ctx, &tt.hypothesis, normalized, incident)

			if result.Total < tt.minScore {
				t.Errorf("Score() = %v, want >= %v", result.Total, tt.minScore)
			}

			if result.Confidence < 0 || result.Confidence > 1 {
				t.Errorf("Confidence = %v, want 0-1", result.Confidence)
			}

			breakdownSum := result.Breakdown.TimeCorrelation + result.Breakdown.TopologyScore +
				result.Breakdown.ChangeProximity + result.Breakdown.MetricSeverity +
				result.Breakdown.ErrorFrequency + result.Breakdown.TraceEvidence
			if breakdownSum < 0 || breakdownSum > 6 {
				t.Errorf("Breakdown sum = %v, want 0-6", breakdownSum)
			}
		})
	}
}

func TestNormalizeConfidence(t *testing.T) {
	tests := []struct {
		name   string
		score  float64
		minC   float64
		maxC   float64
	}{
		{"zero score", 0.0, 0.0, 0.5},
		{"low score", 0.2, 0.0, 0.5},
		{"medium score", 0.5, 0.4, 0.6},
		{"high score", 0.8, 0.5, 1.0},
		{"perfect score", 1.0, 0.9, 1.0},
	}

	scorer := NewMultiDimensionalScorer(DefaultWeights())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			confidence := scorer.normalizeConfidence(tt.score)
			if confidence < tt.minC || confidence > tt.maxC {
				t.Errorf("normalizeConfidence(%v) = %v, want %v-%v", tt.score, confidence, tt.minC, tt.maxC)
			}
		})
	}
}

func generateTestWindows(start, end time.Time) []models.TimeWindow {
	windows := []models.TimeWindow{}
	subWindow := end.Sub(start) / 5
	for t := start; t.Before(end); t = t.Add(subWindow) {
		windows = append(windows, models.TimeWindow{
			Start: t,
			End:   t.Add(subWindow),
		})
	}
	return windows
}

func TestScoreTraceEvidence(t *testing.T) {
	weights := DefaultWeights()
	scorer := NewMultiDimensionalScorer(weights)

	incident := &models.Incident{
		ID:        "test-trace-001",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
		Severity:  "P1",
		AffectedServices: []string{"api-service"},
	}

	windows := generateTestWindows(incident.StartTime, incident.EndTime)

	tests := []struct {
		name       string
		hypothesis models.Hypothesis
		signals    *models.NormalizedSignals
		wantMin    float64
		wantMax    float64
	}{
		{
			name: "high error trace rate",
			hypothesis: models.Hypothesis{
				Type:    models.HypothesisTypeErrorSpike,
				Service: "api-service",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				TraceByWindow: map[int][]models.TraceSummary{
					3: {
						{TraceID: "t1", ServiceName: "api-service", HasError: true},
						{TraceID: "t2", ServiceName: "api-service", HasError: true},
						{TraceID: "t3", ServiceName: "api-service", HasError: false},
					},
				},
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends:  map[string]map[int]models.Trend{},
			},
			wantMin: 0.8, // errorRate 2/3=0.67 > 0.5 → score=1.0, weighted
			wantMax: 1.0,
		},
		{
			name: "no service specified",
			hypothesis: models.Hypothesis{
				Type:   models.HypothesisTypeMetricAnomaly,
				Metric: "http_errors",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				TraceByWindow: map[int][]models.TraceSummary{},
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends:  map[string]map[int]models.Trend{},
			},
			wantMin: 0.0,
			wantMax: 0.0,
		},
		{
			name: "no traces for service",
			hypothesis: models.Hypothesis{
				Type:    models.HypothesisTypeErrorSpike,
				Service: "unknown-service",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				TraceByWindow: map[int][]models.TraceSummary{
					3: {
						{TraceID: "t1", ServiceName: "api-service", HasError: true},
					},
				},
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends:  map[string]map[int]models.Trend{},
			},
			wantMin: 0.0,
			wantMax: 0.0,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.Score(ctx, &tt.hypothesis, tt.signals, incident)
			traceScore := result.Breakdown.TraceEvidence
			if traceScore < tt.wantMin {
				t.Errorf("TraceEvidence = %v, want >= %v", traceScore, tt.wantMin)
			}
			if traceScore > tt.wantMax {
				t.Errorf("TraceEvidence = %v, want <= %v", traceScore, tt.wantMax)
			}
		})
	}
}

func TestScoreChangeProximity(t *testing.T) {
	weights := DefaultWeights()
	scorer := NewMultiDimensionalScorer(weights)

	incident := &models.Incident{
		ID:        "test-change-001",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
		Severity:  "P1",
		AffectedServices: []string{"api-service"},
	}

	windows := generateTestWindows(incident.StartTime, incident.EndTime)

	tests := []struct {
		name       string
		hypothesis models.Hypothesis
		signals    *models.NormalizedSignals
		wantMin    float64
	}{
		{
			name: "change matches hypothesis service",
			hypothesis: models.Hypothesis{
				Type:     models.HypothesisTypeChange,
				Service:  "api-service",
				ChangeID: "change-001",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends:  map[string]map[int]models.Trend{},
				Changes: []models.Change{
					{ID: "change-001", Service: "api-service", Timestamp: incident.StartTime.Add(5 * time.Minute), Type: "deployment", Description: "v2.0 deploy"},
				},
			},
			wantMin: 0.5,
		},
		{
			name: "change for different service",
			hypothesis: models.Hypothesis{
				Type:     models.HypothesisTypeChange,
				Service:  "database",
				ChangeID: "change-002",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends:  map[string]map[int]models.Trend{},
				Changes: []models.Change{
					{ID: "change-001", Service: "api-service", Timestamp: incident.StartTime.Add(5 * time.Minute), Type: "deployment"},
				},
			},
			wantMin: 0.0,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.Score(ctx, &tt.hypothesis, tt.signals, incident)
			changeScore := result.Breakdown.ChangeProximity
			if changeScore < tt.wantMin {
				t.Errorf("ChangeProximity = %v, want >= %v", changeScore, tt.wantMin)
			}
		})
	}
}

func TestScoreTopology(t *testing.T) {
	weights := DefaultWeights()
	scorer := NewMultiDimensionalScorer(weights)

	incident := &models.Incident{
		ID:        "test-topo-001",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
		Severity:  "P1",
		AffectedServices: []string{"api-service"},
	}

	windows := generateTestWindows(incident.StartTime, incident.EndTime)

	tests := []struct {
		name       string
		hypothesis models.Hypothesis
		signals    *models.NormalizedSignals
		wantMin    float64
	}{
		{
			name: "dependency hypothesis with direct dependency",
			hypothesis: models.Hypothesis{
				Type:    models.HypothesisTypeDependency,
				Service: "database",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends:  map[string]map[int]models.Trend{},
				Topology: &models.Topology{
					Nodes: []models.ServiceNode{
						{Name: "api-service", Type: "application"},
						{Name: "database", Type: "database"},
					},
					Edges: []models.DependencyEdge{
						{From: "api-service", To: "database", Type: "database"},
					},
				},
			},
			wantMin: 0.5,
		},
		{
			name: "service not in topology",
			hypothesis: models.Hypothesis{
				Type:    models.HypothesisTypeDependency,
				Service: "unknown-service",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends:  map[string]map[int]models.Trend{},
				Topology: &models.Topology{
					Nodes: []models.ServiceNode{{Name: "api-service"}},
					Edges: []models.DependencyEdge{},
				},
			},
			wantMin: 0.0,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.Score(ctx, &tt.hypothesis, tt.signals, incident)
			topoScore := result.Breakdown.TopologyScore
			if topoScore < tt.wantMin {
				t.Errorf("TopologyScore = %v, want >= %v", topoScore, tt.wantMin)
			}
		})
	}
}

func TestScoreTimeCorrelationWithMetricTrend(t *testing.T) {
	weights := DefaultWeights()
	scorer := NewMultiDimensionalScorer(weights)

	incident := &models.Incident{
		ID:        "test-tc-001",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
		Severity:  "P1",
		AffectedServices: []string{"api-service"},
	}

	windows := generateTestWindows(incident.StartTime, incident.EndTime)

	tests := []struct {
		name       string
		hypothesis models.Hypothesis
		signals    *models.NormalizedSignals
		wantMin    float64
	}{
		{
			name: "metric trend in incident window",
			hypothesis: models.Hypothesis{
				Type:   models.HypothesisTypeMetricAnomaly,
				Metric: "http_errors",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends: map[string]map[int]models.Trend{
					"http_errors": {
						3: models.TrendUp,
						4: models.TrendUp,
					},
				},
			},
			wantMin: 0.5, // trend during incident → score >= 0.8 * weight
		},
		{
			name: "no matching metric trends",
			hypothesis: models.Hypothesis{
				Type:   models.HypothesisTypeMetricAnomaly,
				Metric: "latency_p99",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends: map[string]map[int]models.Trend{
					"http_errors": {3: models.TrendUp},
				},
			},
			wantMin: 0.0,
		},
		{
			name: "log spike ratio >2x baseline",
			hypothesis: models.Hypothesis{
				Type:    models.HypothesisTypeErrorSpike,
				Service: "api-service",
			},
			signals: &models.NormalizedSignals{
				TimeWindows: windows,
				LogByWindow: map[int][]models.LogEntry{
					0: {{Service: "api-service", IsError: true}}, // baseline: 1 log
					1: {},                                         // baseline: 0
					2: {},                                         // baseline: 0
					3: {                                           // incident: 3 logs (>2x baseline avg)
						{Service: "api-service", IsError: true},
						{Service: "api-service", IsError: true},
						{Service: "api-service", IsError: true},
					},
				},
				MetricTrends: map[string]map[int]models.Trend{},
			},
			wantMin: 0.3,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.Score(ctx, &tt.hypothesis, tt.signals, incident)
			tcScore := result.Breakdown.TimeCorrelation
			if tcScore < tt.wantMin {
				t.Errorf("TimeCorrelation = %v, want >= %v", tcScore, tt.wantMin)
			}
		})
	}
}

func TestScoreMetricSeverity(t *testing.T) {
	weights := DefaultWeights()
	scorer := NewMultiDimensionalScorer(weights)

	incident := &models.Incident{
		ID:        "test-ms-001",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
		Severity:  "P1",
		AffectedServices: []string{"api-service"},
	}

	windows := generateTestWindows(incident.StartTime, incident.EndTime)

	tests := []struct {
		name       string
		hypothesis models.Hypothesis
		signals    *models.NormalizedSignals
		wantMin    float64
	}{
		{
			name: "3 incident trends → score 1.0",
			hypothesis: models.Hypothesis{
				Type:   models.HypothesisTypeMetricAnomaly,
				Metric: "http_errors",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends: map[string]map[int]models.Trend{
					"http_errors": {
						3: models.TrendUp,
						4: models.TrendUp,
					},
				},
			},
			wantMin: 0.3, // 2 incident trends → 0.7 * 0.05 weight
		},
		{
			name: "not metric anomaly type → 0",
			hypothesis: models.Hypothesis{
				Type:    models.HypothesisTypeErrorSpike,
				Service: "api-service",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends:  map[string]map[int]models.Trend{},
			},
			wantMin: 0.0,
		},
		{
			name: "metric not in trends → 0",
			hypothesis: models.Hypothesis{
				Type:   models.HypothesisTypeMetricAnomaly,
				Metric: "nonexistent",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends: map[string]map[int]models.Trend{
					"http_errors": {3: models.TrendUp},
				},
			},
			wantMin: 0.0,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.Score(ctx, &tt.hypothesis, tt.signals, incident)
			msScore := result.Breakdown.MetricSeverity
			if msScore < tt.wantMin {
				t.Errorf("MetricSeverity = %v, want >= %v", msScore, tt.wantMin)
			}
		})
	}
}

func TestScoreErrorFrequency(t *testing.T) {
	weights := DefaultWeights()
	scorer := NewMultiDimensionalScorer(weights)

	incident := &models.Incident{
		ID:        "test-ef-001",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
		Severity:  "P1",
		AffectedServices: []string{"api-service"},
	}

	windows := generateTestWindows(incident.StartTime, incident.EndTime)

	tests := []struct {
		name       string
		hypothesis models.Hypothesis
		signals    *models.NormalizedSignals
		wantMin    float64
	}{
		{
			name: "high error ratio in incident",
			hypothesis: models.Hypothesis{
				Type:    models.HypothesisTypeErrorSpike,
				Service: "api-service",
			},
			signals: &models.NormalizedSignals{
				TimeWindows: windows,
				LogByWindow: map[int][]models.LogEntry{
					0: {{Service: "api-service", IsError: true}}, // baseline
					1: {},                                         // baseline
					2: {},                                         // baseline
					3: {                                           // incident: all 3 are errors
						{Service: "api-service", IsError: true},
						{Service: "api-service", IsError: true},
						{Service: "api-service", IsError: true},
					},
				},
				MetricTrends: map[string]map[int]models.Trend{},
			},
			wantMin: 0.01, // ratio 3/4=0.75 > 0.7 → score 1.0 * weight 0.03
		},
		{
			name: "no errors for service",
			hypothesis: models.Hypothesis{
				Type:    models.HypothesisTypeErrorSpike,
				Service: "other-service",
			},
			signals: &models.NormalizedSignals{
				TimeWindows: windows,
				LogByWindow: map[int][]models.LogEntry{
					3: {{Service: "api-service", IsError: true}},
				},
				MetricTrends: map[string]map[int]models.Trend{},
			},
			wantMin: 0.0,
		},
		{
			name: "not error spike type → 0",
			hypothesis: models.Hypothesis{
				Type:   models.HypothesisTypeMetricAnomaly,
				Metric: "http_errors",
			},
			signals: &models.NormalizedSignals{
				TimeWindows:   windows,
				LogByWindow:   map[int][]models.LogEntry{},
				MetricTrends:  map[string]map[int]models.Trend{},
			},
			wantMin: 0.0,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.Score(ctx, &tt.hypothesis, tt.signals, incident)
			efScore := result.Breakdown.ErrorFrequency
			if efScore < tt.wantMin {
				t.Errorf("ErrorFrequency = %v, want >= %v", efScore, tt.wantMin)
			}
		})
	}
}

func TestMaxFunction(t *testing.T) {
	tests := []struct {
		a, b, want float64
	}{
		{1.0, 2.0, 2.0},
		{3.0, 1.0, 3.0},
		{0.0, 0.0, 0.0},
		{-1.0, 1.0, 1.0},
	}

	for _, tt := range tests {
		result := max(tt.a, tt.b)
		if result != tt.want {
			t.Errorf("max(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.want)
		}
	}
}

// TestScoreTimeCorrelationPerService 锁定两个修复:
//  1. 时间相关性按假设的服务计数(别的服务的日志不再抬分);
//  2. 基线窗口零采样但事故窗有该服务日志(最近优先采样截断的尖峰形态)
//     按满值计时间相关,不当作无证据;窗口 0 构造性无前置,不得分。
func TestScoreTimeCorrelationPerService(t *testing.T) {
	scorer := NewMultiDimensionalScorer(DefaultWeights())
	// 显式窗口:4 个事故前基线窗 + 4 个事故窗(镜像引擎 generateTimeWindows 的
	// [start-windowSize, end] 形状,事故覆盖后半段)。
	base := time.Now().Add(-2 * time.Hour)
	incStart := base.Add(4 * 10 * time.Minute)
	incEnd := incStart.Add(4 * 10 * time.Minute)
	incident := &models.Incident{ID: "t", StartTime: incStart, EndTime: incEnd, Severity: "P1"}
	var windows []models.TimeWindow
	for i := 0; i < 8; i++ {
		st := base.Add(time.Duration(i) * 10 * time.Minute)
		windows = append(windows, models.TimeWindow{Start: st, End: st.Add(10 * time.Minute)})
	}

	// 形态一:全部日志只属于 svc-a 且全在事故窗;基线窗零采样(采样截断尖峰)。
	normalized := &models.NormalizedSignals{TimeWindows: windows, LogByWindow: map[int][]models.LogEntry{}}
	for i := 4; i < 8; i++ {
		normalized.LogByWindow[i] = append(normalized.LogByWindow[i],
			models.LogEntry{Service: "svc-a", Level: "error", IsError: true},
			models.LogEntry{Service: "svc-noise", Level: "error", IsError: true},
		)
	}
	hyp := &models.Hypothesis{Type: models.HypothesisTypeErrorSpike, Service: "svc-a"}
	if got := scorer.scoreTimeCorrelation(hyp, normalized, incident); got != 1.0 {
		t.Fatalf("零基线尖峰形态应计满分时间相关, got %v", got)
	}

	// 形态二:svc-noise 基线 1 条/窗,事故窗 4 条/窗(4 倍尖峰)→ 满分。
	normalized2 := &models.NormalizedSignals{TimeWindows: windows, LogByWindow: map[int][]models.LogEntry{}}
	for i := 0; i < 8; i++ {
		e := models.LogEntry{Service: "svc-noise", Level: "error", IsError: true}
		if i >= 4 {
			normalized2.LogByWindow[i] = append(normalized2.LogByWindow[i], e, e, e, e)
		} else {
			normalized2.LogByWindow[i] = append(normalized2.LogByWindow[i], e)
		}
	}
	hyp2 := &models.Hypothesis{Type: models.HypothesisTypeErrorSpike, Service: "svc-noise"}
	if got := scorer.scoreTimeCorrelation(hyp2, normalized2, incident); got != 1.0 {
		t.Fatalf("4 倍于基线的尖峰应计满分, got %v", got)
	}

	// 形态三:svc-calm 基线 5 条/窗,事故窗 1 条/窗(0.2 倍,无尖峰)→ 0 分。
	normalized3 := &models.NormalizedSignals{TimeWindows: windows, LogByWindow: map[int][]models.LogEntry{}}
	for i := 0; i < 8; i++ {
		e := models.LogEntry{Service: "svc-calm", Level: "info", IsError: false}
		if i >= 4 {
			normalized3.LogByWindow[i] = append(normalized3.LogByWindow[i], e)
		} else {
			normalized3.LogByWindow[i] = append(normalized3.LogByWindow[i], e, e, e, e, e)
		}
	}
	hyp3 := &models.Hypothesis{Type: models.HypothesisTypeErrorSpike, Service: "svc-calm"}
	if got := scorer.scoreTimeCorrelation(hyp3, normalized3, incident); got != 0 {
		t.Fatalf("事故窗低于基线不应得分, got %v", got)
	}

	// 形态四:按服务归因 —— svc-a 的假设不得因 svc-noise 的日志而得分
	// (基线与事故窗都只有 svc-noise 日志;svc-a 全程零日志)。
	normalized4 := &models.NormalizedSignals{TimeWindows: windows, LogByWindow: map[int][]models.LogEntry{}}
	for i := 0; i < 8; i++ {
		normalized4.LogByWindow[i] = append(normalized4.LogByWindow[i],
			models.LogEntry{Service: "svc-noise", Level: "error", IsError: true})
	}
	hyp4 := &models.Hypothesis{Type: models.HypothesisTypeErrorSpike, Service: "svc-a"}
	if got := scorer.scoreTimeCorrelation(hyp4, normalized4, incident); got != 0 {
		t.Fatalf("其他服务的日志不应抬分(按服务归因), got %v", got)
	}
}