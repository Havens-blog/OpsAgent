// Package models defines RCA hypothesis and result structures.
package models

import (
	"fmt"
	"time"
)

// HypothesisType represents the type of root cause hypothesis.
type HypothesisType string

const (
	HypothesisTypeErrorSpike    HypothesisType = "error_spike"
	HypothesisTypeMetricAnomaly HypothesisType = "metric_anomaly"
	HypothesisTypeChange        HypothesisType = "change"
	HypothesisTypeDependency    HypothesisType = "dependency"
	HypothesisTypeLatency       HypothesisType = "latency"
	HypothesisTypeResource      HypothesisType = "resource"
	HypothesisTypeWAFAttack     HypothesisType = "waf_attack"
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
	TimeCorrelation float64 // How well the timing matches the incident
	TopologyScore   float64 // Topology proximity to affected services
	ChangeProximity float64 // Proximity to recent changes
	MetricSeverity  float64 // Severity of metric anomalies
	ErrorFrequency  float64 // Frequency of error logs
	TraceEvidence   float64 // Evidence from traces
	AlarmEvidence   float64 // Hypothesis directly explains the alarm itself (WAF facts)
}

// ScoredHypothesis is a hypothesis with its computed score.
type ScoredHypothesis struct {
	Hypothesis Hypothesis
	Score      float64
	Confidence float64 // Normalized 0-1
	Breakdown  ScoreBreakdown
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

	// Warnings are human-readable, stable-ordered source errors for the HTTP DTO.
	Warnings []string
	// Note is an honest caveat that overrides the candidate ranking — e.g. a
	// dial-test host that cannot be mapped to a backend service. When set, the
	// summary leads with it instead of presenting a window-noise candidate as
	// the root cause.
	Note string
	// EdgeFacts carries window-scoped SLB facts for a domain alarm. It is
	// reference material only (never scored), so a dial-test "unavailable rate"
	// report can show whether the SLB forwarded 5xx in the window without
	// crowning the edge as the root cause.
	EdgeFacts *EdgeFacts
}

// GetSummary returns a human-readable summary of the RCA report. The headline
// is confidence-tiered: a weak correlation must not masquerade as a determined
// root cause — on-call trusts the headline wording.
func (r *RCAReport) GetSummary() string {
	if r == nil {
		return "Unable to determine root cause"
	}
	if r.Note != "" {
		return r.Note
	}
	if r.TopCandidate == nil {
		return "Unable to determine root cause"
	}

	conf := r.TopCandidate.Confidence
	label := "弱信号(置信度低,仅供排查参考)"
	switch {
	case conf >= 0.7:
		label = "Root cause"
	case conf >= 0.5:
		label = "疑似根因"
	}

	return fmt.Sprintf("%s: %s (confidence: %.0f%%)\n\nEvidence:\n%s",
		label,
		r.TopCandidate.Hypothesis.Description,
		conf*100,
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
