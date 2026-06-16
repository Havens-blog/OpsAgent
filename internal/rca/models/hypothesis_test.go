// Package models provides tests for RCA hypothesis structures.
package models

import (
	"testing"
)

func TestRCAReportGetSummary(t *testing.T) {
	tests := []struct {
		name     string
		report   *RCAReport
		wantStr  string
		wantEmpty bool
	}{
		{
			name:     "nil report",
			report:   nil,
			wantEmpty: true,
		},
		{
			name: "report with no top candidate",
			report: &RCAReport{
				Candidates: []ScoredHypothesis{},
			},
			wantStr: "Unable to determine root cause",
		},
		{
			name: "report with top candidate",
			report: &RCAReport{
				TopCandidate: &ScoredHypothesis{
					Hypothesis: Hypothesis{
						Description: "Database connection pool exhausted",
						Evidence:    []string{"Error logs show timeout", "Metric shows 0 available connections"},
					},
					Confidence: 0.85,
				},
			},
			wantStr: "Root cause: Database connection pool exhausted (confidence: 85%)",
		},
		{
			name: "report with low confidence candidate",
			report: &RCAReport{
				TopCandidate: &ScoredHypothesis{
					Hypothesis: Hypothesis{
						Description: "Possible memory leak",
						Evidence:    []string{"Memory usage trending up"},
					},
					Confidence: 0.35,
				},
			},
			wantStr: "Root cause: Possible memory leak (confidence: 35%)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := tt.report.GetSummary()
			if tt.wantEmpty && summary != "Unable to determine root cause" {
				t.Errorf("GetSummary() = %q, want empty message", summary)
			}
			if !tt.wantEmpty && summary == "" {
				t.Errorf("GetSummary() returned empty string")
			}
			if tt.wantStr != "" && summary[:len(tt.wantStr)] != tt.wantStr {
				t.Errorf("GetSummary() prefix = %q, want %q", summary[:len(tt.wantStr)], tt.wantStr)
			}
		})
	}
}

func TestRCAReportHasHighConfidence(t *testing.T) {
	tests := []struct {
		name    string
		report  *RCAReport
		want    bool
	}{
		{"nil report", nil, false},
		{"no top candidate", &RCAReport{}, false},
		{"low confidence", &RCAReport{TopCandidate: &ScoredHypothesis{Confidence: 0.3}}, false},
		{"medium confidence", &RCAReport{TopCandidate: &ScoredHypothesis{Confidence: 0.6}}, false},
		{"high confidence", &RCAReport{TopCandidate: &ScoredHypothesis{Confidence: 0.7}}, true},
		{"very high confidence", &RCAReport{TopCandidate: &ScoredHypothesis{Confidence: 0.95}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.report.HasHighConfidence(); got != tt.want {
				t.Errorf("HasHighConfidence() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRCAReportHasMultipleCandidates(t *testing.T) {
	tests := []struct {
		name    string
		report  *RCAReport
		want    bool
	}{
		{"nil report", nil, false},
		{"single candidate", &RCAReport{Candidates: []ScoredHypothesis{{Confidence: 0.7}}}, false},
		{"two low confidence", &RCAReport{Candidates: []ScoredHypothesis{{Confidence: 0.3}, {Confidence: 0.4}}}, false},
		{"two high confidence", &RCAReport{Candidates: []ScoredHypothesis{{Confidence: 0.7}, {Confidence: 0.6}}}, true},
		{"three candidates mixed", &RCAReport{Candidates: []ScoredHypothesis{{Confidence: 0.8}, {Confidence: 0.3}, {Confidence: 0.5}}}, true},
		{"empty candidates", &RCAReport{Candidates: []ScoredHypothesis{}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.report.HasMultipleCandidates(); got != tt.want {
				t.Errorf("HasMultipleCandidates() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHypothesisTypeString(t *testing.T) {
	tests := []struct {
		name  string
		typ   HypothesisType
		want  string
	}{
		{"error spike", HypothesisTypeErrorSpike, "error_spike"},
		{"metric anomaly", HypothesisTypeMetricAnomaly, "metric_anomaly"},
		{"change", HypothesisTypeChange, "change"},
		{"dependency", HypothesisTypeDependency, "dependency"},
		{"latency", HypothesisTypeLatency, "latency"},
		{"resource", HypothesisTypeResource, "resource"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.typ) != tt.want {
				t.Errorf("HypothesisType %q = %q, want %q", tt.name, string(tt.typ), tt.want)
			}
		})
	}
}

func TestFormatEvidence(t *testing.T) {
	tests := []struct {
		name     string
		evidence []string
		want     string
	}{
		{"empty evidence", []string{}, ""},
		{"single evidence", []string{"Error detected"}, "- Error detected\n"},
		{"multiple evidence", []string{"Error detected", "Metric spike"}, "- Error detected\n- Metric spike\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatEvidence(tt.evidence)
			if got != tt.want {
				t.Errorf("formatEvidence() = %q, want %q", got, tt.want)
			}
		})
	}
}
