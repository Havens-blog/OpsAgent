// Package synthetic provides RCA scoring and evaluation for synthetic tests.
package synthetic

import (
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// ScoreResult represents the evaluation score for a scenario run.
type ScoreResult struct {
	ScenarioName     string
	OverallScore     float64 // 0-100
	AccuracyScore    float64
	ConfidenceScore  float64
	SignalScore      float64
	TimelinessScore  float64
	Passed           bool
	Findings         []ValidationFinding
	Duration         time.Duration
}

// ValidationFinding represents a validation check result.
type ValidationFinding struct {
	Category string // accuracy, confidence, signal, timing
	Passed   bool
	Message  string
	Severity string // critical, warning, info
}

// Evaluator evaluates RCA analysis results against expected outcomes.
type Evaluator struct {
	minAccuracyThreshold    float64
	minConfidenceThreshold float64
	signalWeighting         map[string]float64
}

// NewEvaluator creates a new RCA evaluator.
func NewEvaluator() *Evaluator {
	return &Evaluator{
		minAccuracyThreshold:    0.7,
		minConfidenceThreshold: 0.5,
		signalWeighting: map[string]float64{
			"error_spike":       1.0,
			"metric_anomaly":   0.9,
			"change_proximity": 0.8,
			"topology_score":   0.7,
		},
	}
}

// Evaluate scores a scenario result against expected outcomes.
func (e *Evaluator) Evaluate(result *ScenarioResult) *ScoreResult {
	startTime := time.Now()

	findings := []ValidationFinding{}

	// 1. Accuracy Check - Did we identify the correct service?
	accuracyFinding := e.checkAccuracy(result.Report, result.Expected)
	findings = append(findings, accuracyFinding)

	// 2. Confidence Check - Is confidence above threshold?
	confidenceFinding := e.checkConfidence(result.Report, result.Expected)
	findings = append(findings, confidenceFinding)

	// 3. Signal Check - Did we detect expected signals?
	signalFinding := e.checkSignals(result.Report, result.Expected)
	findings = append(findings, signalFinding)

	// 4. Finding Check - Did we identify expected issues?
	findingCheck := e.checkFindings(result.Report, result.Expected)
	findings = append(findings, findingCheck)

	// Calculate scores
	accuracyScore := e.calculateAccuracyScore(accuracyFinding)
	confidenceScore := e.calculateConfidenceScore(confidenceFinding)
	signalScore := e.calculateSignalScore(signalFinding)
	timelinessScore := 100.0 // Assuming analysis completed in reasonable time

	// Calculate overall score
	overallScore := (accuracyScore*0.4 + confidenceScore*0.3 + signalScore*0.2 + timelinessScore*0.1)

	// Determine if passed
	allPassed := true
	criticalCount := 0
	for _, f := range findings {
		if !f.Passed && f.Severity == "critical" {
			allPassed = false
			criticalCount++
		}
	}

	return &ScoreResult{
		ScenarioName:     result.Scenario,
		OverallScore:     overallScore,
		AccuracyScore:    accuracyScore,
		ConfidenceScore:  confidenceScore,
		SignalScore:      signalScore,
		TimelinessScore:  timelinessScore,
		Passed:           allPassed,
		Findings:         findings,
		Duration:         time.Since(startTime),
	}
}

// checkAccuracy validates the top candidate service identification.
func (e *Evaluator) checkAccuracy(report *models.RCAReport, expected *ExpectedResult) ValidationFinding {
	if report.TopCandidate == nil {
		return ValidationFinding{
			Category: "accuracy",
			Passed:   false,
			Message:  "No top candidate identified",
			Severity: "critical",
		}
	}

	actualService := report.TopCandidate.Hypothesis.Service
	expectedService := expected.TopCandidateService

	if actualService == expectedService {
		return ValidationFinding{
			Category: "accuracy",
			Passed:   true,
			Message:  fmt.Sprintf("Correctly identified %s as root cause", actualService),
			Severity: "info",
		}
	}

	return ValidationFinding{
		Category: "accuracy",
		Passed:   false,
		Message:  fmt.Sprintf("Expected %s, got %s", expectedService, actualService),
		Severity: "critical",
	}
}

// checkConfidence validates the confidence score.
func (e *Evaluator) checkConfidence(report *models.RCAReport, expected *ExpectedResult) ValidationFinding {
	if report.Confidence < expected.MinConfidence {
		return ValidationFinding{
			Category: "confidence",
			Passed:   false,
			Message:  fmt.Sprintf("Confidence %.2f below minimum %.2f", report.Confidence, expected.MinConfidence),
			Severity: "critical",
		}
	}

	if report.Confidence < e.minConfidenceThreshold {
		return ValidationFinding{
			Category: "confidence",
			Passed:   false,
			Message:  fmt.Sprintf("Confidence %.2f below evaluator threshold %.2f", report.Confidence, e.minConfidenceThreshold),
			Severity: "warning",
		}
	}

	return ValidationFinding{
		Category: "confidence",
		Passed:   true,
		Message:  fmt.Sprintf("Confidence %.2f meets threshold", report.Confidence),
		Severity: "info",
	}
}

// checkSignals validates that expected signals were detected.
func (e *Evaluator) checkSignals(report *models.RCAReport, expected *ExpectedResult) ValidationFinding {
	if report.TopCandidate == nil {
		return ValidationFinding{
			Category: "signal",
			Passed:   false,
			Message:  "Cannot check signals - no top candidate",
			Severity: "critical",
		}
	}

	detectedSignals := make(map[string]bool)
	for _, signal := range expected.ExpectedSignals {
		detectedSignals[signal] = false
	}

	// Check if signals are present in the hypothesis
	desc := strings.ToLower(report.TopCandidate.Hypothesis.Description)
	for _, signal := range expected.ExpectedSignals {
		if strings.Contains(desc, strings.ToLower(signal)) {
			detectedSignals[signal] = true
		}
	}

	missingSignals := []string{}
	for signal, detected := range detectedSignals {
		if !detected {
			missingSignals = append(missingSignals, signal)
		}
	}

	if len(missingSignals) > 0 {
		return ValidationFinding{
			Category: "signal",
			Passed:   false,
			Message:  fmt.Sprintf("Missing signals: %v", missingSignals),
			Severity: "warning",
		}
	}

	return ValidationFinding{
		Category: "signal",
		Passed:   true,
		Message:  "All expected signals detected",
		Severity: "info",
	}
}

// checkFindings validates that expected findings are present.
func (e *Evaluator) checkFindings(report *models.RCAReport, expected *ExpectedResult) ValidationFinding {
	if report.TopCandidate == nil {
		return ValidationFinding{
			Category: "finding",
			Passed:   false,
			Message:  "Cannot check findings - no top candidate",
			Severity: "warning",
		}
	}

	desc := strings.ToLower(report.TopCandidate.Hypothesis.Description)
	evidence := strings.ToLower(strings.Join(report.TopCandidate.Hypothesis.Evidence, " "))
	combinedText := desc + " " + evidence

	missingFindings := []string{}
	for _, finding := range expected.ExpectedFindings {
		if !strings.Contains(combinedText, strings.ToLower(finding)) {
			missingFindings = append(missingFindings, finding)
		}
	}

	if len(missingFindings) > 0 {
		return ValidationFinding{
			Category: "finding",
			Passed:   false,
			Message:  fmt.Sprintf("Missing findings: %v", missingFindings),
			Severity: "info",
		}
	}

	return ValidationFinding{
		Category: "finding",
		Passed:   true,
		Message:  "Expected findings present",
		Severity: "info",
	}
}

// calculateAccuracyScore calculates accuracy score.
func (e *Evaluator) calculateAccuracyScore(finding ValidationFinding) float64 {
	if finding.Passed {
		return 100.0
	}
	if finding.Severity == "critical" {
		return 0.0
	}
	return 50.0
}

// calculateConfidenceScore calculates confidence score.
func (e *Evaluator) calculateConfidenceScore(finding ValidationFinding) float64 {
	if finding.Passed {
		return 100.0
	}
	if finding.Severity == "critical" {
		return 0.0
	}
	return 60.0
}

// calculateSignalScore calculates signal detection score.
func (e *Evaluator) calculateSignalScore(finding ValidationFinding) float64 {
	if finding.Passed {
		return 100.0
	}
	return 70.0
}

// GenerateReport creates a human-readable evaluation report.
func GenerateReport(results []ScoreResult) string {
	var output strings.Builder

	output.WriteString("# RCA Synthetic Test Report\n\n")
	output.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))

	// Summary
	totalScenarios := len(results)
	passedScenarios := 0
	totalScore := 0.0

	for _, r := range results {
		if r.Passed {
			passedScenarios++
		}
		totalScore += r.OverallScore
	}

	passRate := float64(passedScenarios) / float64(totalScenarios) * 100
	avgScore := totalScore / float64(totalScenarios)

	output.WriteString("## Summary\n\n")
	output.WriteString(fmt.Sprintf("| Metric | Value |\n"))
	output.WriteString(fmt.Sprintf("|--------|-------|\n"))
	output.WriteString(fmt.Sprintf("| Total Scenarios | %d |\n", totalScenarios))
	output.WriteString(fmt.Sprintf("| Passed | %d (%.1f%%) |\n", passedScenarios, passRate))
	output.WriteString(fmt.Sprintf("| Failed | %d (%.1f%%) |\n", totalScenarios-passedScenarios, 100-passRate))
	output.WriteString(fmt.Sprintf("| Average Score | %.1f/100 |\n\n", avgScore))

	// Detailed Results
	output.WriteString("## Detailed Results\n\n")

	for _, r := range results {
		statusIcon := "✅"
		if !r.Passed {
			statusIcon = "❌"
		}

		output.WriteString(fmt.Sprintf("### %s %s\n\n", statusIcon, r.ScenarioName))
		output.WriteString(fmt.Sprintf("**Score:** %.1f/100\n", r.OverallScore))
		output.WriteString(fmt.Sprintf("**Duration:** %s\n\n", r.Duration))

		// Component scores
		output.WriteString("| Component | Score |\n")
		output.WriteString("|-----------|-------|\n")
		output.WriteString(fmt.Sprintf("| Accuracy | %.1f/100 |\n", r.AccuracyScore))
		output.WriteString(fmt.Sprintf("| Confidence | %.1f/100 |\n", r.ConfidenceScore))
		output.WriteString(fmt.Sprintf("| Signals | %.1f/100 |\n\n", r.SignalScore))

		// Findings
		if len(r.Findings) > 0 {
			output.WriteString("**Findings:**\n\n")
			for _, f := range r.Findings {
				icon := "✓"
				if !f.Passed {
					icon = "✗"
				}
				output.WriteString(fmt.Sprintf("- %s **%s:** %s\n", icon, f.Category, f.Message))
			}
			output.WriteString("\n")
		}
	}

	return output.String()
}

// ScoredScenario represents a scored scenario with detailed metrics.
type ScoredScenario struct {
	Name              string
	Passed            bool
	Score             float64
	TopCandidate      string
	ExpectedCandidate string
	Confidence        float64
	SignalsDetected   []string
	SignalsExpected   []string
	Findings          []string
	Timestamp         time.Time
}

// GetScoredScenarios converts score results to scored scenarios.
func GetScoredScenarios(results []ScoreResult) []ScoredScenario {
	scenarios := make([]ScoredScenario, 0, len(results))

	for _, r := range results {
		scenario := ScoredScenario{
			Name:            r.ScenarioName,
			Passed:          r.Passed,
			Score:           r.OverallScore,
			Confidence:      0.0,
			SignalsDetected: []string{},
			SignalsExpected: []string{},
			Findings:        []string{},
			Timestamp:       time.Now(),
		}

		// Note: ScoreResult doesn't have Report field directly,
		// this function expects results to be generated from ScenarioResult

		for _, f := range r.Findings {
			scenario.Findings = append(scenario.Findings, f.Message)
		}

		scenarios = append(scenarios, scenario)
	}

	return scenarios
}
