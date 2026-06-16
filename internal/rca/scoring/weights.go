// Package scoring provides multi-dimensional scoring for RCA hypotheses.
package scoring

import "fmt"

// Weights defines the relative importance of each scoring dimension.
// Based on OpenSRE's approach with adjustments for multi-cloud operations.
type Weights struct {
	// TimeCorrelation: Does the hypothesis timing match the incident window? (50%)
	TimeCorrelation float64

	// TopologyScore: Is the hypothesis service topologically related? (25%)
	TopologyScore float64

	// ChangeProximity: Is there a recent change related to the hypothesis? (15%)
	ChangeProximity float64

	// MetricSeverity: How severe are the metric anomalies? (5%)
	MetricSeverity float64

	// ErrorFrequency: How frequent are the errors? (3%)
	ErrorFrequency float64

	// TraceEvidence: Do traces support this hypothesis? (2%)
	TraceEvidence float64
}

// DefaultWeights returns the default scoring weights.
// These values are based on OpenSRE's multi-dimensional scoring,
// adapted for multi-cloud operations where topology may be incomplete.
func DefaultWeights() Weights {
	return Weights{
		TimeCorrelation: 0.50, // Time alignment is most important
		TopologyScore:    0.25, // Topology provides strong evidence
		ChangeProximity:  0.15, // Changes are common root causes (70% of incidents)
		MetricSeverity:   0.05, // Metrics provide supporting evidence
		ErrorFrequency:   0.03, // Error frequency is context
		TraceEvidence:    0.02, // Traces provide granular evidence
	}
}

// Validate checks if weights sum to approximately 1.0.
func (w Weights) Validate() error {
	sum := w.TimeCorrelation + w.TopologyScore + w.ChangeProximity +
		w.MetricSeverity + w.ErrorFrequency + w.TraceEvidence

	if sum < 0.95 || sum > 1.05 {
		return fmt.Errorf("weights sum to %.2f, expected ~1.0", sum)
	}
	return nil
}

// Clone creates a copy of the weights.
func (w Weights) Clone() Weights {
	return w
}
