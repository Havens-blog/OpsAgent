// Package synthetic provides tests for RCA scoring and evaluation.
package synthetic

import (
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// TestNewEvaluator tests evaluator creation.
func TestNewEvaluator(t *testing.T) {
	eval := NewEvaluator()

	if eval == nil {
		t.Fatal("Expected non-nil evaluator")
	}

	if eval.minAccuracyThreshold != 0.7 {
		t.Errorf("Expected min accuracy threshold 0.7, got %f", eval.minAccuracyThreshold)
	}
}

// TestEvaluate tests scenario evaluation.
func TestEvaluate(t *testing.T) {
	eval := NewEvaluator()

	// Create a successful result
	successfulResult := &ScenarioResult{
		Scenario: "test_scenario",
		Report: &models.RCAReport{
			Incident: &models.Incident{ID: "INC-001"},
			Confidence: 0.8,
			TopCandidate: &models.ScoredHypothesis{
				Hypothesis: models.Hypothesis{
					Service:     "test-service",
					Description: "Database connection pool exhaustion detected",
					Evidence:    []string{"High connection usage", "Timeout errors"},
				},
				Score: 0.85,
			},
		},
		Expected: &ExpectedResult{
			TopCandidateService: "test-service",
			MinConfidence:       0.5,
			ExpectedSignals:     []string{"error_spike"},
			ExpectedFindings:    []string{"connection"},
		},
		Passed:    true,
		StartTime: time.Now(),
	}

	t.Run("successful_scenario", func(t *testing.T) {
		score := eval.Evaluate(successfulResult)

		if score == nil {
			t.Fatal("Expected non-nil score")
		}

		if score.ScenarioName != "test_scenario" {
			t.Errorf("Expected scenario name 'test_scenario', got '%s'", score.ScenarioName)
		}

		if !score.Passed {
			t.Error("Expected scenario to pass")
		}

		if score.OverallScore == 0 {
			t.Error("Expected non-zero overall score")
		}
	})

	// Create a failed result
	failedResult := &ScenarioResult{
		Scenario: "failed_scenario",
		Report: &models.RCAReport{
			Incident: &models.Incident{ID: "INC-002"},
			Confidence: 0.3,
			TopCandidate: &models.ScoredHypothesis{
				Hypothesis: models.Hypothesis{
					Service:     "wrong-service",
					Description: "Incorrect analysis",
				},
			},
		},
		Expected: &ExpectedResult{
			TopCandidateService: "correct-service",
			MinConfidence:       0.7,
			ExpectedSignals:     []string{"error_spike", "metric_anomaly"},
		},
		Passed:    false,
		StartTime: time.Now(),
	}

	t.Run("failed_scenario", func(t *testing.T) {
		score := eval.Evaluate(failedResult)

		if score.Passed {
			t.Error("Expected scenario to fail")
		}

		if score.AccuracyScore >= 50 {
			t.Error("Expected low accuracy score for incorrect service")
		}

		if score.ConfidenceScore >= 50 {
			t.Error("Expected low confidence score for low confidence")
		}
	})

	// Result with no top candidate
	noCandidateResult := &ScenarioResult{
		Scenario: "no_candidate",
		Report: &models.RCAReport{
			Incident:    &models.Incident{ID: "INC-003"},
			Confidence: 0.0,
			Candidates:  []models.ScoredHypothesis{},
		},
		Expected: &ExpectedResult{
			TopCandidateService: "expected-service",
		},
		StartTime: time.Now(),
	}

	t.Run("no_top_candidate", func(t *testing.T) {
		score := eval.Evaluate(noCandidateResult)

		if score.Passed {
			t.Error("Expected scenario to fail without top candidate")
		}

		if score.AccuracyScore > 0 {
			t.Error("Expected zero accuracy score without top candidate")
		}
	})
}

// TestCheckAccuracy tests accuracy checking.
func TestCheckAccuracy(t *testing.T) {
	eval := NewEvaluator()

	t.Run("correct_service", func(t *testing.T) {
		report := &models.RCAReport{
			TopCandidate: &models.ScoredHypothesis{
				Hypothesis: models.Hypothesis{Service: "correct-service"},
			},
		}
		expected := &ExpectedResult{TopCandidateService: "correct-service"}

		finding := eval.checkAccuracy(report, expected)

		if !finding.Passed {
			t.Error("Expected accuracy check to pass")
		}
		if finding.Severity != "info" {
			t.Errorf("Expected severity 'info', got '%s'", finding.Severity)
		}
	})

	t.Run("incorrect_service", func(t *testing.T) {
		report := &models.RCAReport{
			TopCandidate: &models.ScoredHypothesis{
				Hypothesis: models.Hypothesis{Service: "wrong-service"},
			},
		}
		expected := &ExpectedResult{TopCandidateService: "correct-service"}

		finding := eval.checkAccuracy(report, expected)

		if finding.Passed {
			t.Error("Expected accuracy check to fail")
		}
		if finding.Severity != "critical" {
			t.Errorf("Expected severity 'critical', got '%s'", finding.Severity)
		}
	})

	t.Run("no_candidate", func(t *testing.T) {
		report := &models.RCAReport{TopCandidate: nil}
		expected := &ExpectedResult{TopCandidateService: "expected-service"}

		finding := eval.checkAccuracy(report, expected)

		if finding.Passed {
			t.Error("Expected accuracy check to fail without candidate")
		}
		if finding.Severity != "critical" {
			t.Errorf("Expected severity 'critical', got '%s'", finding.Severity)
		}
	})
}

// TestCheckConfidence tests confidence checking.
func TestCheckConfidence(t *testing.T) {
	eval := NewEvaluator()

	t.Run("high_confidence", func(t *testing.T) {
		report := &models.RCAReport{Confidence: 0.8}
		expected := &ExpectedResult{MinConfidence: 0.5}

		finding := eval.checkConfidence(report, expected)

		if !finding.Passed {
			t.Error("Expected confidence check to pass")
		}
	})

	t.Run("below_minimum", func(t *testing.T) {
		report := &models.RCAReport{Confidence: 0.3}
		expected := &ExpectedResult{MinConfidence: 0.5}

		finding := eval.checkConfidence(report, expected)

		if finding.Passed {
			t.Error("Expected confidence check to fail")
		}
		if finding.Severity != "critical" {
			t.Errorf("Expected severity 'critical', got '%s'", finding.Severity)
		}
	})

	t.Run("below_threshold", func(t *testing.T) {
		report := &models.RCAReport{Confidence: 0.4}
		expected := &ExpectedResult{MinConfidence: 0.3}

		finding := eval.checkConfidence(report, expected)

		if finding.Passed {
			t.Error("Expected confidence check to fail below threshold")
		}
		if finding.Severity != "warning" {
			t.Errorf("Expected severity 'warning', got '%s'", finding.Severity)
		}
	})
}

// TestCheckSignals tests signal detection checking.
func TestCheckSignals(t *testing.T) {
	eval := NewEvaluator()

	t.Run("all_signals_detected", func(t *testing.T) {
		report := &models.RCAReport{
			TopCandidate: &models.ScoredHypothesis{
				Hypothesis: models.Hypothesis{
					Description: "error spike and metric anomaly detected",
				},
			},
		}
		expected := &ExpectedResult{ExpectedSignals: []string{"error spike", "metric anomaly"}}

		finding := eval.checkSignals(report, expected)

		if !finding.Passed {
			t.Error("Expected signal check to pass")
		}
	})

	t.Run("missing_signals", func(t *testing.T) {
		report := &models.RCAReport{
			TopCandidate: &models.ScoredHypothesis{
				Hypothesis: models.Hypothesis{
					Description: "Only error spike detected",
				},
			},
		}
		expected := &ExpectedResult{ExpectedSignals: []string{"error_spike", "metric_anomaly", "change_proximity"}}

		finding := eval.checkSignals(report, expected)

		if finding.Passed {
			t.Error("Expected signal check to fail with missing signals")
		}
		if finding.Severity != "warning" {
			t.Errorf("Expected severity 'warning', got '%s'", finding.Severity)
		}
	})
}

// TestCheckFindings tests findings validation.
func TestCheckFindings(t *testing.T) {
	eval := NewEvaluator()

	t.Run("all_findings_present", func(t *testing.T) {
		report := &models.RCAReport{
			TopCandidate: &models.ScoredHypothesis{
				Hypothesis: models.Hypothesis{
					Description: "Connection pool exhaustion causing timeouts",
					Evidence:    []string{"High connection usage", "Pool exhausted"},
				},
			},
		}
		expected := &ExpectedResult{ExpectedFindings: []string{"connection", "timeout"}}

		finding := eval.checkFindings(report, expected)

		if !finding.Passed {
			t.Error("Expected findings check to pass")
		}
	})

	t.Run("missing_findings", func(t *testing.T) {
		report := &models.RCAReport{
			TopCandidate: &models.ScoredHypothesis{
				Hypothesis: models.Hypothesis{
					Description: "Generic error",
				},
			},
		}
		expected := &ExpectedResult{ExpectedFindings: []string{"connection", "pool", "timeout"}}

		finding := eval.checkFindings(report, expected)

		if finding.Passed {
			t.Error("Expected findings check to fail with missing findings")
		}
	})
}

// TestGenerateReport tests report generation.
func TestGenerateReport(t *testing.T) {
	results := []ScoreResult{
		{
			ScenarioName:     "scenario_1",
			OverallScore:     85.0,
			AccuracyScore:    100.0,
			ConfidenceScore:  90.0,
			SignalScore:      75.0,
			TimelinessScore:  100.0,
			Passed:           true,
			Findings:         []ValidationFinding{},
			Duration:         100 * time.Millisecond,
		},
		{
			ScenarioName:     "scenario_2",
			OverallScore:     45.0,
			AccuracyScore:    0.0,
			ConfidenceScore:  60.0,
			SignalScore:      50.0,
			TimelinessScore:  100.0,
			Passed:           false,
			Findings:         []ValidationFinding{},
			Duration:         150 * time.Millisecond,
		},
	}

	report := GenerateReport(results)

	if report == "" {
		t.Error("Expected non-empty report")
	}

	// Check for key sections
	if !contains(report, "# RCA Synthetic Test Report") {
		t.Error("Report missing title")
	}
	if !contains(report, "## Summary") {
		t.Error("Report missing summary section")
	}
	if !contains(report, "## Detailed Results") {
		t.Error("Report missing detailed results section")
	}
	if !contains(report, "scenario_1") {
		t.Error("Report missing scenario_1")
	}
	if !contains(report, "scenario_2") {
		t.Error("Report missing scenario_2")
	}
}

// TestGetScoredScenarios tests conversion to scored scenarios.
func TestGetScoredScenarios(t *testing.T) {
	results := []ScoreResult{
		{
			ScenarioName: "test_scenario",
			OverallScore: 85.0,
			Passed:       true,
			Findings:     []ValidationFinding{{Message: "Test passed"}},
		},
	}

	scenarios := GetScoredScenarios(results)

	if len(scenarios) != 1 {
		t.Fatalf("Expected 1 scenario, got %d", len(scenarios))
	}

	scenario := scenarios[0]
	if scenario.Name != "test_scenario" {
		t.Errorf("Expected name 'test_scenario', got '%s'", scenario.Name)
	}
	if scenario.Score != 85.0 {
		t.Errorf("Expected score 85.0, got %f", scenario.Score)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsInString(s, substr))
}

func containsInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
