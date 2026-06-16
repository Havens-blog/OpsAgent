// Package scoring provides tests for RCA scoring weights.
package scoring

import (
	"testing"
)

func TestDefaultWeights(t *testing.T) {
	weights := DefaultWeights()

	tests := []struct {
		name  string
		field float64
		want  float64
	}{
		{"TimeCorrelation", weights.TimeCorrelation, 0.50},
		{"TopologyScore", weights.TopologyScore, 0.25},
		{"ChangeProximity", weights.ChangeProximity, 0.15},
		{"MetricSeverity", weights.MetricSeverity, 0.05},
		{"ErrorFrequency", weights.ErrorFrequency, 0.03},
		{"TraceEvidence", weights.TraceEvidence, 0.02},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.field != tt.want {
				t.Errorf("weight %s = %v, want %v", tt.name, tt.field, tt.want)
			}
		})
	}

	// Verify sum is approximately 1.0
	sum := weights.TimeCorrelation + weights.TopologyScore + weights.ChangeProximity +
		weights.MetricSeverity + weights.ErrorFrequency + weights.TraceEvidence

	if sum < 0.99 || sum > 1.01 {
		t.Errorf("weights sum = %v, want ~1.0", sum)
	}
}

func TestWeightsValidate(t *testing.T) {
	tests := []struct {
		name    string
		weights Weights
		wantErr bool
	}{
		{
			name:    "valid default weights",
			weights: DefaultWeights(),
			wantErr: false,
		},
		{
			name: "valid custom weights",
			weights: Weights{
				TimeCorrelation: 0.40,
				TopologyScore:   0.30,
				ChangeProximity: 0.20,
				MetricSeverity:  0.05,
				ErrorFrequency:  0.03,
				TraceEvidence:   0.02,
			},
			wantErr: false,
		},
		{
			name: "sum too low",
			weights: Weights{
				TimeCorrelation: 0.30,
				TopologyScore:   0.20,
				ChangeProximity: 0.10,
				MetricSeverity:  0.05,
				ErrorFrequency:  0.03,
				TraceEvidence:   0.02,
			},
			wantErr: true,
		},
		{
			name: "sum too high",
			weights: Weights{
				TimeCorrelation: 0.60,
				TopologyScore:   0.30,
				ChangeProximity: 0.15,
				MetricSeverity:  0.05,
				ErrorFrequency:  0.03,
				TraceEvidence:   0.02,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.weights.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Weights.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWeightsClone(t *testing.T) {
	original := DefaultWeights()
	cloned := original.Clone()

	// Modify clone
	cloned.TimeCorrelation = 0.99

	// Original should be unchanged
	if original.TimeCorrelation != 0.50 {
		t.Errorf("Clone modified original: TimeCorrelation = %v, want 0.50", original.TimeCorrelation)
	}

	// Clone should have new value
	if cloned.TimeCorrelation != 0.99 {
		t.Errorf("Clone has wrong value: TimeCorrelation = %v, want 0.99", cloned.TimeCorrelation)
	}
}
