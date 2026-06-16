// Package models defines RCA hypothesis and result structures.
package models

import (
	"fmt"
	"time"
)

// HypothesisType represents the type of root cause hypothesis.
type HypothesisType string

const (
	HypothesisTypeErrorSpike     HypothesisType = "error_spike"
	HypothesisTypeMetricAnomaly HypothesisType = "metric_anomaly"
	HypothesisTypeChange         HypothesisType = "change"
	HypothesisTypeDependency     HypothesisType = "dependency"
	HypothesisTypeLatency        HypothesisType = "latency"
	HypothesisTypeResource       HypothesisType = "resource"
)

// Hypothesis represents a potential root cause candidate.
type Hypothesis struct {
	Type        HypothesisType
	Service     string
	Description string
	Metric      string // For metric anomalies
	ChangeID    string // For change-related hypotheses
	Evidence    []string
	Confidence  float64
}

// ScoreBreakdown represents the score components for a hypothesis.
type ScoreBreakdown struct {
	TimeCorrelation  float64 // How well the timing matches the incident
	TopologyScore    float64 // Topology proximity to affected services
	ChangeProximity  float64 // Proximity to recent changes
	MetricSeverity   float64 // Severity of metric anomalies
	ErrorFrequency   float64 // Frequency of error logs
	TraceEvidence    float64 // Evidence from traces
}

// ScoredHypothesis is a hypothesis with its computed score.
type ScoredHypothesis struct {
	Hypothesis  Hypothesis
	Score       float64
	Confidence  float64 // Normalized 0-1
	Breakdown   ScoreBreakdown
}

// RCAReport represents the complete root cause analysis report.
type RCAReport struct {
	Incident       *Incident
	Signals        *Signals
	Candidates     []ScoredHypothesis
	TopCandidate   *ScoredHypothesis
	Confidence     float64
	AnalysisWindow TimeWindow
	Duration       time.Duration
	GeneratedAt    time.Time
}

// GetSummary returns a human-readable summary of the RCA report.
func (r *RCAReport) GetSummary() string {
	if r == nil || r.TopCandidate == nil {
		return "Unable to determine root cause"
	}

	return fmt.Sprintf("Root cause: %s (confidence: %.0f%%)\n\nEvidence:\n%s",
		r.TopCandidate.Hypothesis.Description,
		r.TopCandidate.Confidence*100,
		formatEvidence(r.TopCandidate.Hypothesis.Evidence))
}

func formatEvidence(evidence []string) string {
	result := ""
	for _, e := range evidence {
		result += fmt.Sprintf("- %s\n", e)
	}
	return result
}

// HasHighConfidence returns true if the top candidate has high confidence.
func (r *RCAReport) HasHighConfidence() bool {
	return r != nil && r.TopCandidate != nil && r.TopCandidate.Confidence >= 0.7
}

// HasMultipleCandidates returns true if there are multiple high-confidence candidates.
func (r *RCAReport) HasMultipleCandidates() bool {
	if r == nil || len(r.Candidates) < 2 {
		return false
	}
	count := 0
	for _, c := range r.Candidates {
		if c.Confidence >= 0.5 {
			count++
		}
	}
	return count >= 2
}
