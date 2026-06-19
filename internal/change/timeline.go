// Package change provides change-fault time window analysis for OpsAgent.
// This module analyzes the temporal relationship between changes and faults
// to determine causality.
package change

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// TimelineAnalyzer analyzes the temporal relationship between changes and incidents.
type TimelineAnalyzer struct {
	changeAnalyzer   *ChangeAnalyzer
	configAnalyzer   *ConfigAnalyzer
	correlationThreshold float64
}

// NewTimelineAnalyzer creates a new timeline analyzer.
func NewTimelineAnalyzer(
	changeAnalyzer *ChangeAnalyzer,
	configAnalyzer *ConfigAnalyzer,
) *TimelineAnalyzer {
	return &TimelineAnalyzer{
		changeAnalyzer:   changeAnalyzer,
		configAnalyzer:   configAnalyzer,
		correlationThreshold: 0.3,
	}
}

// SetCorrelationThreshold sets the minimum correlation score for considering a change related.
func (a *TimelineAnalyzer) SetCorrelationThreshold(threshold float64) {
	a.correlationThreshold = threshold
}

// TimeWindowAnalysis represents the analysis of a time window.
type TimeWindowAnalysis struct {
	IncidentID        string               `json:"incident_id"`
	AnalysisWindow    TimeWindow          `json:"analysis_window"`
	Changes           []TimelineChange    `json:"changes"`
	CausalityChain    []CausalityLink      `json:"causality_chain"`
	RootCauseHypothesis *RootCauseHypothesis `json:"root_cause_hypothesis"`
	Confidence        float64              `json:"confidence"`
}

// TimelineChange represents a change with its temporal context.
type TimelineChange struct {
	ChangeID        string        `json:"change_id"`
	ChangeType      ChangeType    `json:"change_type"`
	Timestamp       time.Time     `json:"timestamp"`
	TimeToIncident  time.Duration `json:"time_to_incident"` // Time from change to incident
	CorrelationScore float64      `json:"correlation_score"`
	Severity        ChangeSeverity `json:"severity"`
	Description     string        `json:"description"`
}

// CausalityLink represents a potential causal link in the timeline.
type CausalityLink struct {
	FromChangeID   string        `json:"from_change_id"`
	ToChangeID     string        `json:"to_change_id,omitempty"`
	ToIncidentID   string        `json:"to_incident_id,omitempty"`
	LinkStrength   float64       `json:"link_strength"`
	Reasoning      string        `json:"reasoning"`
	TimeDelta      time.Duration `json:"time_delta"`
}

// RootCauseHypothesis represents the most likely root cause.
type RootCauseHypothesis struct {
	ChangeID        string   `json:"change_id"`
	ChangeType      ChangeType `json:"change_type"`
	Confidence      float64  `json:"confidence"`
	Reasoning       string   `json:"reasoning"`
	Alternatives    []AlternativeRootCause `json:"alternatives,omitempty"`
}

// AlternativeRootCause represents an alternative root cause hypothesis.
type AlternativeRootCause struct {
	ChangeID   string  `json:"change_id"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// AnalyzeIncidentTimeline analyzes the timeline of an incident to identify potential root causes.
func (a *TimelineAnalyzer) AnalyzeIncidentTimeline(
	ctx context.Context,
	incidentID string,
	incidentWindow TimeWindow,
) (*TimeWindowAnalysis, error) {
	// Look back 60 minutes before incident start
	analysisWindow := TimeWindow{
		StartTime: incidentWindow.StartTime.Add(-60 * time.Minute),
		EndTime:   incidentWindow.EndTime,
	}

	// Collect all changes in the analysis window
	deploymentCorrelations, err := a.changeAnalyzer.CorrelateWithIncident(ctx, incidentID, incidentWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to correlate deployments: %w", err)
	}

	configCorrelations, err := a.configAnalyzer.CorrelateWithIncident(ctx, incidentID, incidentWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to correlate config changes: %w", err)
	}

	// Combine and sort by time
	allCorrelations := append(deploymentCorrelations, configCorrelations...)

	// Build timeline changes
	timelineChanges := make([]TimelineChange, 0, len(allCorrelations))
	for _, correlation := range allCorrelations {
		if correlation.CorrelationScore >= a.correlationThreshold {
			change := TimelineChange{
				ChangeID:         correlation.ChangeID,
				ChangeType:       correlation.ChangeType,
				TimeToIncident:   correlation.TimeDelta,
				CorrelationScore: correlation.CorrelationScore,
				Description:      correlation.Reason,
			}
			timelineChanges = append(timelineChanges, change)
		}
	}

	// Sort by time to incident (soonest first)
	sort.Slice(timelineChanges, func(i, j int) bool {
		return timelineChanges[i].TimeToIncident < timelineChanges[j].TimeToIncident
	})

	// Build causality chain
	causalityChain := a.buildCausalityChain(timelineChanges, incidentID)

	// Determine root cause hypothesis
	hypothesis := a.determineRootCause(timelineChanges, causalityChain)

	// Calculate overall confidence
	confidence := a.calculateOverallConfidence(timelineChanges, hypothesis)

	return &TimeWindowAnalysis{
		IncidentID:         incidentID,
		AnalysisWindow:     analysisWindow,
		Changes:            timelineChanges,
		CausalityChain:     causalityChain,
		RootCauseHypothesis: hypothesis,
		Confidence:         confidence,
	}, nil
}

// buildCausalityChain builds a chain of causal links between changes.
func (a *TimelineAnalyzer) buildCausalityChain(
	changes []TimelineChange,
	incidentID string,
) []CausalityLink {
	if len(changes) == 0 {
		return []CausalityLink{}
	}

	chain := make([]CausalityLink, 0, len(changes))

	// Each change potentially leads to the next or to the incident
	for i, change := range changes {
		if i == 0 {
			// First change potentially leads to incident
			link := CausalityLink{
				FromChangeID:  change.ChangeID,
				ToIncidentID:  incidentID,
				LinkStrength:  change.CorrelationScore,
				Reasoning:     change.Description,
				TimeDelta:     change.TimeToIncident,
			}
			chain = append(chain, link)
		} else {
			// Change potentially leads to next change
			prevChange := changes[i-1]
			link := CausalityLink{
				FromChangeID:  prevChange.ChangeID,
				ToChangeID:    change.ChangeID,
				LinkStrength:  a.calculateLinkStrength(prevChange, change),
				Reasoning:     fmt.Sprintf("Sequential changes: %s then %s",
					prevChange.ChangeID, change.ChangeID),
				TimeDelta:     change.TimeToIncident - prevChange.TimeToIncident,
			}
			chain = append(chain, link)

			// Current change also potentially leads to incident
			linkToIncident := CausalityLink{
				FromChangeID:  change.ChangeID,
				ToIncidentID:  incidentID,
				LinkStrength:  change.CorrelationScore,
				Reasoning:     change.Description,
				TimeDelta:     change.TimeToIncident,
			}
			chain = append(chain, linkToIncident)
		}
	}

	return chain
}

// calculateLinkStrength calculates the strength of a causal link between two changes.
func (a *TimelineAnalyzer) calculateLinkStrength(from, to TimelineChange) float64 {
	// Factors affecting link strength:
	// 1. Time proximity (closer = stronger)
	// 2. Change type compatibility (config -> deployment is common)
	// 3. Correlation scores

	strength := 0.0

	// Time proximity: if changes are within 5 minutes, stronger link
	timeBetween := to.TimeToIncident - from.TimeToIncident
	if timeBetween < 5*time.Minute {
		strength += 0.4
	} else if timeBetween < 15*time.Minute {
		strength += 0.2
	}

	// Change type compatibility
	if isCompatibleChangeSequence(from.ChangeType, to.ChangeType) {
		strength += 0.3
	}

	// Average correlation scores
	avgCorrelation := (from.CorrelationScore + to.CorrelationScore) / 2
	strength += avgCorrelation * 0.3

	return math.Min(strength, 1.0)
}

// isCompatibleChangeSequence checks if one change type commonly follows another.
func isCompatibleChangeSequence(from, to ChangeType) bool {
	// Config changes often precede deployments
	if from == ChangeTypeConfig && to == ChangeTypeDeployment {
		return true
	}
	if from == ChangeTypeSecret && to == ChangeTypeDeployment {
		return true
	}

	// Infrastructure changes can trigger config changes
	if from == ChangeTypeInfrastructure && to == ChangeTypeConfig {
		return true
	}

	return false
}

// determineRootCause determines the most likely root cause from the timeline.
func (a *TimelineAnalyzer) determineRootCause(
	changes []TimelineChange,
	chain []CausalityLink,
) *RootCauseHypothesis {
	if len(changes) == 0 {
		return &RootCauseHypothesis{
			Confidence: 0.0,
			Reasoning:  "No changes found in analysis window",
		}
	}

	// Find the change with highest correlation score
	var rootCause *TimelineChange
	maxScore := 0.0

	for i, change := range changes {
		// Weight score by time proximity (soonest changes weighted higher)
		timeWeight := 1.0 - float64(i)/float64(len(changes))
		weightedScore := change.CorrelationScore * (0.7 + 0.3*timeWeight)

		if weightedScore > maxScore {
			maxScore = weightedScore
			rootCause = &changes[i]
		}
	}

	if rootCause == nil {
		return nil
	}

	// Build alternatives
	alternatives := make([]AlternativeRootCause, 0)
	for _, change := range changes {
		if change.ChangeID != rootCause.ChangeID {
			alternatives = append(alternatives, AlternativeRootCause{
				ChangeID:   change.ChangeID,
				Confidence: change.CorrelationScore * 0.8, // Lower confidence for alternatives
				Reasoning:  change.Description,
			})
		}
	}

	return &RootCauseHypothesis{
		ChangeID:   rootCause.ChangeID,
		ChangeType: rootCause.ChangeType,
		Confidence: maxScore,
		Reasoning:  rootCause.Description,
		Alternatives: alternatives,
	}
}

// calculateOverallConfidence calculates the overall confidence in the analysis.
func (a *TimelineAnalyzer) calculateOverallConfidence(
	changes []TimelineChange,
	hypothesis *RootCauseHypothesis,
) float64 {
	if hypothesis == nil || hypothesis.Confidence == 0 {
		return 0.0
	}

	confidence := hypothesis.Confidence

	// Increase confidence if there are multiple related changes
	if len(changes) > 1 {
		confidence *= 1.1
	}

	// Decrease confidence if time window is very large (uncertainty)
	if len(changes) > 0 {
		timeSpread := changes[len(changes)-1].TimeToIncident - changes[0].TimeToIncident
		if timeSpread > 30*time.Minute {
			confidence *= 0.9
		}
	}

	return math.Min(confidence, 1.0)
}

// TimeSeriesPoint represents a point in time series data.
type TimeSeriesPoint struct {
	Timestamp time.Time
	Value     float64
}

// ChangePattern represents a detected pattern in change data.
type ChangePattern struct {
	PatternType    string        `json:"pattern_type"`
	Description    string        `json:"description"`
	Confidence     float64       `json:"confidence"`
	FirstOccurrence time.Time    `json:"first_occurrence"`
	LastOccurrence  time.Time    `json:"last_occurrence"`
	Occurrences    int           `json:"occurrences"`
}

// DetectChangePatterns detects patterns in change data over time.
func (a *TimelineAnalyzer) DetectChangePatterns(
	ctx context.Context,
	window TimeWindow,
) ([]ChangePattern, error) {
	// This would analyze historical change data to detect patterns like:
	// - Recurring changes at specific times
	// - Sequences of changes that often lead to incidents
	// - Seasonal patterns

	return []ChangePattern{}, fmt.Errorf("not implemented")
}

// PredictIncidentRisk predicts the risk of incidents based on recent changes.
func (a *TimelineAnalyzer) PredictIncidentRisk(
	ctx context.Context,
	service string,
	since time.Time,
) (RiskScore, error) {
	// Collect recent changes
	// Analyze patterns
	// Calculate risk score

	return RiskScore{}, fmt.Errorf("not implemented")
}

// RiskScore represents the calculated risk of an incident.
type RiskScore struct {
	Service      string  `json:"service"`
	Score        float64 `json:"score"`        // 0-1
	Factors      []RiskFactor `json:"factors"`
	PredictedWindow *TimeWindow `json:"predicted_window,omitempty"`
	Reasoning    string  `json:"reasoning"`
}

// RiskFactor represents a factor contributing to risk.
type RiskFactor struct {
	Name     string  `json:"name"`
	Impact   float64 `json:"impact"`
	Description string `json:"description"`
}

// GetChangeStatistics returns statistics about changes in a time window.
func (a *TimelineAnalyzer) GetChangeStatistics(
	ctx context.Context,
	window TimeWindow,
) (*ChangeStatistics, error) {
	// This would return statistics like:
	// - Total changes
	// - Changes by type
	// - Changes by service
	// - Failure rate

	return &ChangeStatistics{}, fmt.Errorf("not implemented")
}

// ChangeStatistics represents statistics about changes.
type ChangeStatistics struct {
	TotalChanges      int                    `json:"total_changes"`
	ChangesByType     map[ChangeType]int     `json:"changes_by_type"`
	ChangesByService  map[string]int         `json:"changes_by_service"`
	FailureRate       float64                `json:"failure_rate"`
	MostChangedServices []string             `json:"most_changed_services"`
	HighestRiskServices []string            `json:"highest_risk_services"`
}

// ClusterChanges clusters changes by temporal proximity and type.
func (a *TimelineAnalyzer) ClusterChanges(
	changes []TimelineChange,
	maxTimeGap time.Duration,
) [][]TimelineChange {
	if len(changes) == 0 {
		return [][]TimelineChange{}
	}

	// Sort by timestamp
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].TimeToIncident < changes[j].TimeToIncident
	})

	clusters := [][]TimelineChange{{changes[0]}}
	currentCluster := 0

	for i := 1; i < len(changes); i++ {
		timeGap := changes[i].TimeToIncident - changes[i-1].TimeToIncident

		if timeGap <= maxTimeGap {
			// Add to current cluster
			clusters[currentCluster] = append(clusters[currentCluster], changes[i])
		} else {
			// Start new cluster
			clusters = append(clusters, []TimelineChange{changes[i]})
			currentCluster++
		}
	}

	return clusters
}

// GetCriticalPath finds the critical path of changes leading to an incident.
func (a *TimelineAnalyzer) GetCriticalPath(
	analysis *TimeWindowAnalysis,
) []CausalityLink {
	if analysis == nil || len(analysis.CausalityChain) == 0 {
		return []CausalityLink{}
	}

	// Find the path with highest cumulative strength
	// This is a simplified version - a full implementation would use graph algorithms

	criticalPath := make([]CausalityLink, 0)
	maxCumulativeStrength := 0.0

	// Try each possible starting point
	for i := range analysis.CausalityChain {
		currentStrength := 0.0
		path := make([]CausalityLink, 0)

		// Follow the chain
		for j := i; j < len(analysis.CausalityChain); j++ {
			link := analysis.CausalityChain[j]
			currentStrength += link.LinkStrength
			path = append(path, link)

			// Only continue if this link leads to incident
			if link.ToIncidentID != "" {
				break
			}
		}

		if currentStrength > maxCumulativeStrength {
			maxCumulativeStrength = currentStrength
			criticalPath = path
		}
	}

	return criticalPath
}
