// Package topology provides topology path scoring for OpsAgent.
// This module scores topology paths to rank root cause hypotheses.
package topology

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// PathScorer scores topology paths for root cause analysis.
type PathScorer struct {
	armsClient *ARMSClient
	analyzer   *PropagationAnalyzer
}

// NewPathScorer creates a new path scorer.
func NewPathScorer(
	armsClient *ARMSClient,
	analyzer *PropagationAnalyzer,
) *PathScorer {
	return &PathScorer{
		armsClient: armsClient,
		analyzer:   analyzer,
	}
}

// PathScore represents the score for a topology path.
type PathScore struct {
	Path         []string    `json:"path"`
	Score        float64     `json:"score"`
	Confidence   float64     `json:"confidence"`
	Reasoning    string      `json:"reasoning"`
	Metrics      PathMetrics `json:"metrics"`
	Alternatives []PathScore `json:"alternatives,omitempty"`
}

// RootCauseHypothesis represents a root cause hypothesis for topology scoring.
type RootCauseHypothesis struct {
	ChangeID   string  `json:"change_id"`
	ChangeType string  `json:"change_type"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// PathMetrics contains metrics for a path.
type PathMetrics struct {
	TotalErrorRate float64 `json:"total_error_rate"`
	AvgLatency     float64 `json:"avg_latency"`
	PathLength     int     `json:"path_length"`
	FailedEdges    int     `json:"failed_edges"`
	HealthScore    float64 `json:"health_score"`
	TrafficImpact  float64 `json:"traffic_impact"`
}

// ScoreHypotheses scores multiple root cause hypotheses based on topology paths.
func (s *PathScorer) ScoreHypotheses(
	ctx context.Context,
	hypotheses []RootCauseHypothesis,
	affectedServices []string,
	timeWindow TimeWindow,
) ([]PathScore, error) {
	// Get service topology
	graph, err := s.armsClient.GetServiceGraph(ctx, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get service graph: %w", err)
	}

	scores := make([]PathScore, 0, len(hypotheses))

	for _, hypothesis := range hypotheses {
		// Find path from hypothesis to affected services
		pathScore := s.scorePath(ctx, graph, hypothesis.ChangeID, affectedServices, timeWindow)
		if pathScore != nil {
			scores = append(scores, *pathScore)
		}
	}

	// Sort by score (descending)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	// Add alternatives for top scores
	if len(scores) > 1 {
		for i := range scores {
			if i < len(scores)-1 {
				scores[i].Alternatives = scores[i+1:]
			}
		}
	}

	return scores, nil
}

// scorePath scores a single topology path.
func (s *PathScorer) scorePath(
	ctx context.Context,
	graph *ServiceGraph,
	rootService string,
	affectedServices []string,
	timeWindow TimeWindow,
) *PathScore {
	// Find all paths from root to affected services
	allPaths := make([][]string, 0)

	for _, service := range affectedServices {
		if service == rootService {
			continue
		}

		paths, err := s.armsClient.GetPath(ctx, rootService, service, timeWindow)
		if err != nil {
			continue
		}

		for _, path := range paths {
			allPaths = append(allPaths, path.Nodes)
		}
	}

	if len(allPaths) == 0 {
		return nil
	}

	// Score each path and take the best
	var bestPath []string
	var bestMetrics PathMetrics
	bestScore := -1.0

	for _, path := range allPaths {
		score, metrics := s.calculatePathScore(graph, path, affectedServices)
		if score > bestScore {
			bestScore = score
			bestPath = path
			bestMetrics = metrics
		}
	}

	if bestPath == nil {
		return nil
	}

	return &PathScore{
		Path:       bestPath,
		Score:      bestScore,
		Confidence: s.calculateConfidence(bestMetrics),
		Reasoning:  s.generateReasoning(bestMetrics, bestPath),
		Metrics:    bestMetrics,
	}
}

// calculatePathScore calculates the score for a path.
func (s *PathScorer) calculatePathScore(
	graph *ServiceGraph,
	path []string,
	affectedServices []string,
) (float64, PathMetrics) {
	if len(path) == 0 {
		return 0.0, PathMetrics{}
	}

	metrics := PathMetrics{
		PathLength: len(path),
	}

	totalErrorRate := 0.0
	totalLatency := 0.0
	healthScore := 0.0
	failedEdges := 0

	// Analyze each node and edge in path
	for i := 0; i < len(path); i++ {
		// Find node
		var node *ServiceNode
		for idx := range graph.Nodes {
			if graph.Nodes[idx].ID == path[i] {
				node = &graph.Nodes[idx]
				break
			}
		}

		if node != nil {
			totalErrorRate += node.ErrorRate
			totalLatency += node.Latency

			// Health score contribution
			switch node.Health {
			case "healthy":
				healthScore += 1.0
			case "degraded":
				healthScore += 0.5
			case "unhealthy":
				healthScore += 0.0
			}

			// Check if service is affected
			for _, affected := range affectedServices {
				if affected == node.ID {
					// Affected service contributes more to score
					totalErrorRate += node.ErrorRate * 0.5
				}
			}
		}

		// Analyze edge to next node
		if i < len(path)-1 {
			var edge *ServiceEdge
			for idx := range graph.Edges {
				if graph.Edges[idx].FromNodeID == path[i] && graph.Edges[idx].ToNodeID == path[i+1] {
					edge = &graph.Edges[idx]
					break
				}
			}

			if edge != nil {
				totalErrorRate += edge.ErrorRate
				totalLatency += edge.Latency

				// Count failed edges (>5% error rate)
				if edge.ErrorRate > 0.05 {
					failedEdges++
				}
			}
		}
	}

	// Calculate averages
	pathLen := float64(len(path))
	if pathLen > 0 {
		metrics.TotalErrorRate = totalErrorRate / pathLen
		metrics.AvgLatency = totalLatency / pathLen
		metrics.HealthScore = healthScore / pathLen
	}

	metrics.FailedEdges = failedEdges

	// Calculate traffic impact
	metrics.TrafficImpact = s.calculateTrafficImpact(graph, path)

	// Calculate overall score
	score := s.calculateOverallScore(metrics)

	return score, metrics
}

// calculateTrafficImpact calculates the traffic impact of a path.
func (s *PathScorer) calculateTrafficImpact(graph *ServiceGraph, path []string) float64 {
	if len(path) == 0 {
		return 0.0
	}

	totalThroughput := 0.0

	for _, serviceID := range path {
		for _, node := range graph.Nodes {
			if node.ID == serviceID {
				totalThroughput += node.Throughput
				break
			}
		}
	}

	// Normalize (assuming max throughput of 10000 RPS)
	return math.Min(totalThroughput/10000.0, 1.0)
}

// calculateOverallScore calculates the overall score from metrics.
func (s *PathScorer) calculateOverallScore(metrics PathMetrics) float64 {
	score := 0.0

	// Error rate is the most important factor (40% weight)
	score += metrics.TotalErrorRate * 10 * 0.4

	// Failed edges contribute heavily (30% weight)
	if metrics.FailedEdges > 0 {
		score += float64(metrics.FailedEdges) * 0.3
	}

	// Health score (20% weight)
	score += (1.0 - metrics.HealthScore) * 0.2

	// Traffic impact (10% weight)
	score += metrics.TrafficImpact * 0.1

	// Path length penalty (shorter paths are more likely)
	if metrics.PathLength > 1 {
		score *= 1.0 / float64(metrics.PathLength)
	}

	return math.Min(score, 1.0)
}

// calculateConfidence calculates confidence in the path score.
func (s *PathScorer) calculateConfidence(metrics PathMetrics) float64 {
	confidence := 0.5 // Base confidence

	// Higher error rate increases confidence
	if metrics.TotalErrorRate > 0.05 {
		confidence += 0.2
	}

	// Failed edges increase confidence
	if metrics.FailedEdges > 0 {
		confidence += 0.2
	}

	// Poor health increases confidence
	if metrics.HealthScore < 0.5 {
		confidence += 0.1
	}

	return math.Min(confidence, 1.0)
}

// generateReasoning generates human-readable reasoning for the score.
func (s *PathScorer) generateReasoning(metrics PathMetrics, path []string) string {
	reasoning := fmt.Sprintf("Path of %d services", metrics.PathLength)

	if metrics.TotalErrorRate > 0.05 {
		reasoning += fmt.Sprintf(" with high error rate (%.2f%%)", metrics.TotalErrorRate*100)
	}

	if metrics.FailedEdges > 0 {
		reasoning += fmt.Sprintf(" and %d failed edges", metrics.FailedEdges)
	}

	if metrics.HealthScore < 0.5 {
		reasoning += " and degraded health"
	}

	return reasoning
}

// ScoreTopologyPaths scores all possible paths in the topology.
func (s *PathScorer) ScoreTopologyPaths(
	ctx context.Context,
	affectedServices []string,
	timeWindow TimeWindow,
) (*TopologyScores, error) {
	// Get service topology
	graph, err := s.armsClient.GetServiceGraph(ctx, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get service graph: %w", err)
	}

	// Get all anomalous nodes
	anomalousNodes, err := s.armsClient.GetAnomalousNodes(ctx, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get anomalous nodes: %w", err)
	}

	scores := &TopologyScores{
		AffectedServices: affectedServices,
		AnomalousNodes:   getNodeIDs(anomalousNodes),
		PathScores:       make([]PathScore, 0),
	}

	// Score paths from each anomalous node to affected services
	for _, node := range anomalousNodes {
		pathScore := s.scorePath(ctx, graph, node.ID, affectedServices, timeWindow)
		if pathScore != nil {
			scores.PathScores = append(scores.PathScores, *pathScore)
		}
	}

	// Sort by score
	sort.Slice(scores.PathScores, func(i, j int) bool {
		return scores.PathScores[i].Score > scores.PathScores[j].Score
	})

	// Calculate overall statistics
	scores.calculateStatistics()

	return scores, nil
}

// TopologyScores represents scores for all topology paths.
type TopologyScores struct {
	AffectedServices []string    `json:"affected_services"`
	AnomalousNodes   []string    `json:"anomalous_nodes"`
	PathScores       []PathScore `json:"path_scores"`
	BestPath         *PathScore  `json:"best_path,omitempty"`
	AverageScore     float64     `json:"average_score"`
	Confidence       float64     `json:"confidence"`
}

// calculateStatistics calculates overall statistics.
func (s *TopologyScores) calculateStatistics() {
	if len(s.PathScores) == 0 {
		return
	}

	// Best path
	s.BestPath = &s.PathScores[0]

	// Average score
	totalScore := 0.0
	for _, score := range s.PathScores {
		totalScore += score.Score
	}
	s.AverageScore = totalScore / float64(len(s.PathScores))

	// Overall confidence (based on best path)
	s.Confidence = s.BestPath.Confidence
}

// getNodeIDs extracts node IDs from nodes.
func getNodeIDs(nodes []ServiceNode) []string {
	ids := make([]string, len(nodes))
	for i, node := range nodes {
		ids[i] = node.ID
	}
	return ids
}

// GetCriticalPaths returns the most critical paths in the topology.
func (s *PathScorer) GetCriticalPaths(
	ctx context.Context,
	timeWindow TimeWindow,
	topN int,
) ([]PathScore, error) {
	// Get all paths
	graph, err := s.armsClient.GetServiceGraph(ctx, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get service graph: %w", err)
	}

	// Get critical path from ARMS
	criticalPath, err := s.armsClient.GetCriticalPath(ctx, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get critical path: %w", err)
	}

	if criticalPath == nil {
		return []PathScore{}, nil
	}

	// Score the critical path
	_, metrics := s.calculatePathScore(graph, criticalPath.Nodes, []string{})

	score := PathScore{
		Path:       criticalPath.Nodes,
		Score:      s.calculateOverallScore(metrics),
		Confidence: s.calculateConfidence(metrics),
		Reasoning:  s.generateReasoning(metrics, criticalPath.Nodes),
		Metrics:    metrics,
	}

	// Collect high-error paths
	paths := make([]PathScore, 0)
	paths = append(paths, score)

	// Find other paths with high error rates
	for _, edge := range graph.Edges {
		if edge.ErrorRate > 0.01 {
			// Score this simple 2-node path
			path := []string{edge.FromNodeID, edge.ToNodeID}
			pathScore, pathMetrics := s.calculatePathScore(graph, path, []string{})
			paths = append(paths, PathScore{
				Path:       path,
				Score:      pathScore,
				Confidence: s.calculateConfidence(pathMetrics),
				Metrics:    pathMetrics,
				Reasoning:  fmt.Sprintf("Direct edge with %.2f%% error rate", edge.ErrorRate*100),
			})
		}
	}

	// Sort and return top N
	sort.Slice(paths, func(i, j int) bool {
		return paths[i].Score > paths[j].Score
	})

	if topN > 0 && len(paths) > topN {
		paths = paths[:topN]
	}

	return paths, nil
}

// ComparePaths compares two paths and returns the better one.
func (s *PathScorer) ComparePaths(
	ctx context.Context,
	path1, path2 []string,
	timeWindow TimeWindow,
) (*PathComparison, error) {
	graph, err := s.armsClient.GetServiceGraph(ctx, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get service graph: %w", err)
	}

	score1, metrics1 := s.calculatePathScore(graph, path1, []string{})
	score2, metrics2 := s.calculatePathScore(graph, path2, []string{})

	var betterPath []string
	if score1 >= score2 {
		betterPath = path1
	} else {
		betterPath = path2
	}

	return &PathComparison{
		Path1:          path1,
		Path2:          path2,
		Score1:         score1,
		Score2:         score2,
		Metrics1:       metrics1,
		Metrics2:       metrics2,
		BetterPath:     betterPath,
		Recommendation: s.generateComparisonRecommendation(score1, score2),
	}, nil
}

// PathComparison represents a comparison between two paths.
type PathComparison struct {
	Path1          []string    `json:"path1"`
	Path2          []string    `json:"path2"`
	Score1         float64     `json:"score1"`
	Score2         float64     `json:"score2"`
	Metrics1       PathMetrics `json:"metrics1"`
	Metrics2       PathMetrics `json:"metrics2"`
	BetterPath     []string    `json:"better_path"`
	Recommendation string      `json:"recommendation"`
}

// generateComparisonRecommendation generates a recommendation for path comparison.
func (s *PathScorer) generateComparisonRecommendation(score1, score2 float64) string {
	diff := math.Abs(score1 - score2)

	if diff < 0.1 {
		return "Both paths have similar likelihood; consider investigating both"
	}

	if score1 > score2 {
		return fmt.Sprintf("Path 1 is more likely (%.2f vs %.2f)", score1, score2)
	}

	return fmt.Sprintf("Path 2 is more likely (%.2f vs %.2f)", score2, score1)
}

// RankRootCauses ranks potential root causes by topology position.
func (s *PathScorer) RankRootCauses(
	ctx context.Context,
	potentialCauses []string,
	affectedServices []string,
	timeWindow TimeWindow,
) ([]RootCauseRank, error) {
	ranks := make([]RootCauseRank, 0, len(potentialCauses))

	for _, cause := range potentialCauses {
		// Get propagation impact
		impact, err := s.analyzer.GetDownstreamImpact(ctx, cause, timeWindow)
		if err != nil {
			continue
		}

		// Calculate rank score
		rankScore := 0.0

		// More affected downstream services = higher rank
		rankScore += float64(len(impact.DownstreamServices)) * 0.4

		// Higher total impact = higher rank
		rankScore += impact.TotalImpact * 0.3

		// Check if cause is in affected services
		for _, service := range affectedServices {
			if service == cause {
				rankScore += 0.3
				break
			}
		}

		ranks = append(ranks, RootCauseRank{
			CauseID: cause,
			Rank:    0, // Will be set after sorting
			Score:   rankScore,
			Impact:  impact,
			Reasoning: fmt.Sprintf("Affects %d services with %.2f total impact",
				len(impact.DownstreamServices), impact.TotalImpact),
		})
	}

	// Sort by score
	sort.Slice(ranks, func(i, j int) bool {
		return ranks[i].Score > ranks[j].Score
	})

	// Assign ranks
	for i := range ranks {
		ranks[i].Rank = i + 1
	}

	return ranks, nil
}

// RootCauseRank represents a ranked root cause hypothesis.
type RootCauseRank struct {
	CauseID    string             `json:"cause_id"`
	Rank       int                `json:"rank"`
	Score      float64            `json:"score"`
	Impact     *DownstreamImpact  `json:"impact"`
	Reasoning  string             `json:"reasoning"`
}
