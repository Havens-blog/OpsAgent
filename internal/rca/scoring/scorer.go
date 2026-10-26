// Package scoring provides hypothesis scoring for RCA.
package scoring

import (
	"context"
	"math"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// ScoreResult represents the result of scoring a hypothesis.
type ScoreResult struct {
	Total      float64
	Confidence float64
	Breakdown  models.ScoreBreakdown
}

// Scorer scores hypotheses based on multiple dimensions.
type Scorer interface {
	Score(ctx context.Context, hypothesis *models.Hypothesis, signals *models.NormalizedSignals, incident *models.Incident) ScoreResult
}

// MultiDimensionalScorer implements multi-dimensional hypothesis scoring.
type MultiDimensionalScorer struct {
	weights Weights
}

// NewMultiDimensionalScorer creates a new multi-dimensional scorer.
func NewMultiDimensionalScorer(weights Weights) *MultiDimensionalScorer {
	return &MultiDimensionalScorer{weights: weights}
}

// Score calculates a composite score for a hypothesis across multiple dimensions.
func (s *MultiDimensionalScorer) Score(ctx context.Context, hypothesis *models.Hypothesis, signals *models.NormalizedSignals, incident *models.Incident) ScoreResult {
	breakdown := models.ScoreBreakdown{
		TimeCorrelation: s.scoreTimeCorrelation(hypothesis, signals, incident),
		TopologyScore:   s.scoreTopology(hypothesis, signals, incident),
		ChangeProximity: s.scoreChangeProximity(hypothesis, signals, incident),
		MetricSeverity:  s.scoreMetricSeverity(hypothesis, signals, incident),
		ErrorFrequency:  s.scoreErrorFrequency(hypothesis, signals, incident),
		TraceEvidence:   s.scoreTraceEvidence(hypothesis, signals, incident),
	}

	// Calculate weighted sum
	total := breakdown.TimeCorrelation*s.weights.TimeCorrelation +
		breakdown.TopologyScore*s.weights.TopologyScore +
		breakdown.ChangeProximity*s.weights.ChangeProximity +
		breakdown.MetricSeverity*s.weights.MetricSeverity +
		breakdown.ErrorFrequency*s.weights.ErrorFrequency +
		breakdown.TraceEvidence*s.weights.TraceEvidence

	// Normalize to confidence 0-1
	confidence := s.normalizeConfidence(total)

	return ScoreResult{
		Total:      total,
		Confidence: confidence,
		Breakdown:  breakdown,
	}
}

// scoreTimeCorrelation evaluates if the hypothesis timing matches the incident.
func (s *MultiDimensionalScorer) scoreTimeCorrelation(hypothesis *models.Hypothesis, signals *models.NormalizedSignals, incident *models.Incident) float64 {
	score := 0.0

	// Check if logs spike during incident window.计数按假设的服务过滤:
	// 归因对象是该服务,用全局窗口计数会把别的服务的流量算进该假设的
	// 时间相关性(全局计数曾让噪声服务的尖峰抬高所有假设的得分)。
	if hypothesis.Service != "" {
		for i, window := range signals.TimeWindows {
			isIncidentWindow := window.End.After(incident.StartTime) && window.Start.Before(incident.EndTime)
			logCount := countLogsForService(signals.LogByWindow[i], hypothesis.Service)

			if isIncidentWindow && logCount > 0 {
				baselineAvg := s.getServiceBaselineLogCount(signals, i, hypothesis.Service)
				if baselineAvg > 0 {
					ratio := float64(logCount) / baselineAvg
					if ratio > 2.0 {
						score = max(score, 1.0)
					} else if ratio > 1.5 {
						score = max(score, 0.7)
					} else if ratio > 1.2 {
						score = max(score, 0.4)
					}
				} else if i > 0 {
					// 基线窗口存在(非窗口 0 构造性无前置)但该服务零采样,
					// 而事故窗有日志:日志源为"最近优先"采样截断(联邦/云侧
					// limit),高流量舰队下基线窗口恒采不到 —— 能走到这里的
					// error-spike 假设已通过生成侧 incident > 2×baseline &&
					// > 10 的尖峰判定,采样形状本身即尖峰证据,按满值计时间
					// 相关,不把"零基线采样"当"无时间证据"(那会系统性抹掉
					// 日志维度假设)。窗口 0 无前置窗口,无从判定,不得分。
					score = max(score, 1.0)
				}
			}
		}
	}

	// Check metric trends during incident
	if hypothesis.Metric != "" {
		if trends, ok := signals.MetricTrends[hypothesis.Metric]; ok {
			for i, window := range signals.TimeWindows {
				isIncidentWindow := window.End.After(incident.StartTime) && window.Start.Before(incident.EndTime)
				if isIncidentWindow {
					if trend, ok := trends[i]; ok {
						if trend == models.TrendUp || trend == models.TrendDown {
							score = max(score, 0.8)
						}
					}
				}
			}
		}
	}

	return score
}

// scoreTopology evaluates if the hypothesis service is topologically related.
func (s *MultiDimensionalScorer) scoreTopology(hypothesis *models.Hypothesis, signals *models.NormalizedSignals, incident *models.Incident) float64 {
	if signals.Topology == nil || hypothesis.Service == "" {
		return 0.0
	}

	// Check if hypothesis service is in the affected services list
	for _, affected := range incident.AffectedServices {
		if hypothesis.Service == affected {
			return 1.0
		}
	}

	// Check if hypothesis service is a dependency of affected service
	for _, affected := range incident.AffectedServices {
		deps := signals.Topology.GetDependencies(affected)
		for _, dep := range deps {
			if dep == hypothesis.Service {
				return 0.9
			}
		}

		// Check second-level dependencies
		for _, dep := range deps {
			deps2 := signals.Topology.GetDependencies(dep)
			for _, dep2 := range deps2 {
				if dep2 == hypothesis.Service {
					return 0.5
				}
			}
		}
	}

	return 0.0
}

// scoreChangeProximity evaluates if a recent change is related to the hypothesis.
func (s *MultiDimensionalScorer) scoreChangeProximity(hypothesis *models.Hypothesis, signals *models.NormalizedSignals, incident *models.Incident) float64 {
	if hypothesis.Type != models.HypothesisTypeChange {
		return 0.0
	}

	for _, change := range signals.Changes {
		if change.ID == hypothesis.ChangeID {
			timeDiff := change.Timestamp.Sub(incident.StartTime)
			if timeDiff < 0 {
				timeDiff = -timeDiff
			}

			if timeDiff < 5*time.Minute {
				return 1.0
			} else if timeDiff < 15*time.Minute {
				return 0.7
			} else if timeDiff < 30*time.Minute {
				return 0.4
			}
			return 0.2
		}
	}

	return 0.0
}

// scoreMetricSeverity evaluates the severity of metric anomalies.
func (s *MultiDimensionalScorer) scoreMetricSeverity(hypothesis *models.Hypothesis, signals *models.NormalizedSignals, incident *models.Incident) float64 {
	if hypothesis.Type != models.HypothesisTypeMetricAnomaly || hypothesis.Metric == "" {
		return 0.0
	}

	trends, ok := signals.MetricTrends[hypothesis.Metric]
	if !ok {
		return 0.0
	}

	incidentTrendCount := 0
	for i, window := range signals.TimeWindows {
		isIncidentWindow := window.End.After(incident.StartTime) && window.Start.Before(incident.EndTime)
		if isIncidentWindow {
			if trend, ok := trends[i]; ok && trend != models.TrendStable {
				incidentTrendCount++
			}
		}
	}

	if incidentTrendCount >= 3 {
		return 1.0
	} else if incidentTrendCount >= 2 {
		return 0.7
	} else if incidentTrendCount >= 1 {
		return 0.4
	}

	return 0.0
}

// scoreErrorFrequency evaluates the frequency of error logs.
func (s *MultiDimensionalScorer) scoreErrorFrequency(hypothesis *models.Hypothesis, signals *models.NormalizedSignals, incident *models.Incident) float64 {
	if hypothesis.Type != models.HypothesisTypeErrorSpike || hypothesis.Service == "" {
		return 0.0
	}

	totalErrors := 0
	incidentErrors := 0

	for i, window := range signals.TimeWindows {
		logs := signals.LogByWindow[i]
		isIncidentWindow := window.End.After(incident.StartTime) && window.Start.Before(incident.EndTime)

		for _, log := range logs {
			if log.Service == hypothesis.Service && log.IsError {
				totalErrors++
				if isIncidentWindow {
					incidentErrors++
				}
			}
		}
	}

	if totalErrors == 0 {
		return 0.0
	}

	ratio := float64(incidentErrors) / float64(totalErrors)
	if ratio > 0.7 {
		return 1.0
	} else if ratio > 0.5 {
		return 0.7
	} else if ratio > 0.3 {
		return 0.4
	}

	return ratio
}

// scoreTraceEvidence evaluates if traces support the hypothesis.
func (s *MultiDimensionalScorer) scoreTraceEvidence(hypothesis *models.Hypothesis, signals *models.NormalizedSignals, incident *models.Incident) float64 {
	if hypothesis.Service == "" {
		return 0.0
	}

	errorTraceCount := 0
	totalTraceCount := 0

	for _, traces := range signals.TraceByWindow {
		for _, trace := range traces {
			if trace.ServiceName == hypothesis.Service {
				totalTraceCount++
				if trace.HasError {
					errorTraceCount++
				}
			}
		}
	}

	if totalTraceCount == 0 {
		return 0.0
	}

	errorRate := float64(errorTraceCount) / float64(totalTraceCount)
	if errorRate > 0.5 {
		return 1.0
	} else if errorRate > 0.3 {
		return 0.7
	} else if errorRate > 0.1 {
		return 0.4
	}

	return errorRate
}

// Helper methods

func (s *MultiDimensionalScorer) getBaselineLogCount(signals *models.NormalizedSignals, incidentWindowIndex int) float64 {
	if incidentWindowIndex <= 0 {
		return 0
	}

	total := 0
	count := 0
	for i := 0; i < incidentWindowIndex && i < len(signals.TimeWindows); i++ {
		total += len(signals.LogByWindow[i])
		count++
	}

	if count == 0 {
		return 0
	}
	return float64(total) / float64(count)
}

// getServiceBaselineLogCount 单服务版基线均值(事故窗之前窗口里该服务的
// 平均采样条数),与 scoreTimeCorrelation 的按服务计数同口径。
func (s *MultiDimensionalScorer) getServiceBaselineLogCount(signals *models.NormalizedSignals, incidentWindowIndex int, service string) float64 {
	if incidentWindowIndex <= 0 {
		return 0
	}

	total := 0
	count := 0
	for i := 0; i < incidentWindowIndex && i < len(signals.TimeWindows); i++ {
		total += countLogsForService(signals.LogByWindow[i], service)
		count++
	}

	if count == 0 {
		return 0
	}
	return float64(total) / float64(count)
}

// countLogsForService 统计一个窗口内属于指定服务的日志条数(时间相关性
// 与基线共用的单一口径;IsError 不在此过滤 —— 与生成侧尖峰判定同口径)。
func countLogsForService(logs []models.LogEntry, service string) int {
	n := 0
	for _, l := range logs {
		if l.Service == service {
			n++
		}
	}
	return n
}

func (s *MultiDimensionalScorer) normalizeConfidence(score float64) float64 {
	return 1.0 / (1.0 + math.Exp(-5*(score-0.5)))
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
