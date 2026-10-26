// Package topology provides service dependency propagation analysis for OpsAgent.
// This module analyzes how issues propagate through the service dependency graph.
package topology

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// PropagationAnalyzer analyzes issue propagation through service dependencies.
type PropagationAnalyzer struct {
	armsClient *ARMSClient
}

// NewPropagationAnalyzer creates a new propagation analyzer.
func NewPropagationAnalyzer(armsClient *ARMSClient) *PropagationAnalyzer {
	return &PropagationAnalyzer{
		armsClient: armsClient,
	}
}

// PropagationPath represents a potential propagation path for an issue.
type PropagationPath struct {
	Path       []PropagationStep `json:"path"`
	Confidence float64           `json:"confidence"`
	Reasoning  string            `json:"reasoning"`
}

// PropagationStep represents a single step in the propagation path.
type PropagationStep struct {
	FromNode   string    `json:"from_node"`
	ToNode     string    `json:"to_node"`
	EdgeType   string    `json:"edge_type"`
	Impact     float64   `json:"impact"`
	Reasoning  string    `json:"reasoning"`
	Timestamp  time.Time `json:"timestamp"`
}

// PropagationAnalysis represents the complete propagation analysis.
type PropagationAnalysis struct {
	IncidentID       string            `json:"incident_id"`
	AffectedServices []string          `json:"affected_services"`
	PropagationPaths []PropagationPath `json:"propagation_paths"`
	RootCandidate   string            `json:"root_candidate"`    // Most likely root cause
	PropagationTree  *PropagationTree  `json:"propagation_tree"`
	Confidence       float64           `json:"confidence"`
}

// PropagationTree represents the propagation as a tree structure.
type PropagationTree struct {
	Root      *PropagationNode `json:"root"`
	Direction string           `json:"direction"` // upstream, downstream
}

// PropagationNode represents a node in the propagation tree.
type PropagationNode struct {
	ServiceID   string             `json:"service_id"`
	ServiceName string             `json:"service_name"`
	Children    []*PropagationNode `json:"children,omitempty"`
	Impact      float64            `json:"impact"`
	Health      string             `json:"health"`
	ErrorRate   float64            `json:"error_rate"`
	Latency     float64            `json:"latency"`
}

// AnalyzePropagation analyzes how an issue propagated through services.
func (a *PropagationAnalyzer) AnalyzePropagation(
	ctx context.Context,
	incidentID string,
	affectedServices []string,
	timeWindow TimeWindow,
) (*PropagationAnalysis, error) {
	// Get service topology
	graph, err := a.armsClient.GetServiceGraph(ctx, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get service graph: %w", err)
	}

	// Build propagation tree
	tree := a.buildPropagationTree(graph, affectedServices)

	// Find propagation paths
	paths := a.findPropagationPaths(graph, affectedServices)

	// Determine root candidate
	rootCandidate := a.determineRootCandidate(graph, affectedServices)

	// Calculate confidence
	confidence := a.calculatePropagationConfidence(paths, tree)

	return &PropagationAnalysis{
		IncidentID:       incidentID,
		AffectedServices: affectedServices,
		PropagationPaths: paths,
		RootCandidate:   rootCandidate,
		PropagationTree:  tree,
		Confidence:      confidence,
	}, nil
}

// buildPropagationTree builds a tree showing how the issue propagated.
func (a *PropagationAnalyzer) buildPropagationTree(
	graph *ServiceGraph,
	affectedServices []string,
) *PropagationTree {
	if len(affectedServices) == 0 {
		return nil
	}

	// Find affected nodes
	affectedNodes := make(map[string]*ServiceNode)
	for _, serviceID := range affectedServices {
		for i := range graph.Nodes {
			if graph.Nodes[i].ID == serviceID {
				affectedNodes[serviceID] = &graph.Nodes[i]
				break
			}
		}
	}

	// Find root (most likely cause) - service with highest error rate
	var root *ServiceNode
	maxErrorRate := 0.0

	for _, node := range affectedNodes {
		if node.ErrorRate > maxErrorRate {
			maxErrorRate = node.ErrorRate
			root = node
		}
	}

	if root == nil {
		return nil
	}

	// Build tree structure
	treeRoot := &PropagationNode{
		ServiceID:   root.ID,
		ServiceName: root.Name,
		Impact:      root.ErrorRate,
		Health:      root.Health,
		ErrorRate:   root.ErrorRate,
		Latency:     root.Latency,
	}

	// Add downstream affected services
	a.addDownstreamAffected(treeRoot, graph, affectedNodes, make(map[string]bool))

	return &PropagationTree{
		Root:      treeRoot,
		Direction: "downstream",
	}
}

// addDownstreamAffected recursively adds downstream affected services.
func (a *PropagationAnalyzer) addDownstreamAffected(
	parent *PropagationNode,
	graph *ServiceGraph,
	affectedNodes map[string]*ServiceNode,
	visited map[string]bool,
) {
	visited[parent.ServiceID] = true

	// Find downstream services
	for _, edge := range graph.Edges {
		if edge.FromNodeID == parent.ServiceID {
			// Check if downstream service is affected
			if node, ok := affectedNodes[edge.ToNodeID]; ok {
				// Don't revisit nodes
				if visited[edge.ToNodeID] {
					continue
				}

				child := &PropagationNode{
					ServiceID:   node.ID,
					ServiceName: node.Name,
					Impact:      edge.ErrorRate,
					Health:      node.Health,
					ErrorRate:   node.ErrorRate,
					Latency:     node.Latency,
				}

				parent.Children = append(parent.Children, child)

				// Recursively add children
				a.addDownstreamAffected(child, graph, affectedNodes, visited)
			}
		}
	}
}

// findPropagationPaths finds all possible propagation paths.
func (a *PropagationAnalyzer) findPropagationPaths(
	graph *ServiceGraph,
	affectedServices []string,
) []PropagationPath {
	if len(affectedServices) == 0 {
		return []PropagationPath{}
	}

	paths := make([]PropagationPath, 0)

	// For each affected service, find paths from other affected services
	for _, serviceID := range affectedServices {
		for _, otherServiceID := range affectedServices {
			if serviceID == otherServiceID {
				continue
			}

			// Find path from other service to this service
			servicePaths, err := a.armsClient.GetPath(context.Background(), otherServiceID, serviceID, TimeWindow{
				StartTime: time.Now().Add(-1 * time.Hour),
				EndTime:   time.Now(),
			})

			if err == nil && len(servicePaths) > 0 {
				// Convert to propagation path
				for _, servicePath := range servicePaths {
					propPath := a.convertToPropagationPath(servicePath, graph)
					if len(propPath.Path) > 0 {
						paths = append(paths, propPath)
					}
				}
			}
		}
	}

	// Sort by confidence
	sort.Slice(paths, func(i, j int) bool {
		return paths[i].Confidence > paths[j].Confidence
	})

	return paths
}

// convertToPropagationPath converts a service path to a propagation path.
func (a *PropagationAnalyzer) convertToPropagationPath(
	servicePath Path,
	graph *ServiceGraph,
) PropagationPath {
	if len(servicePath.Nodes) < 2 {
		return PropagationPath{}
	}

	steps := make([]PropagationStep, 0, len(servicePath.Nodes)-1)

	for i := 0; i < len(servicePath.Nodes)-1; i++ {
		fromNode := servicePath.Nodes[i]
		toNode := servicePath.Nodes[i+1]

		// Find edge
		var edge *ServiceEdge
		for _, e := range graph.Edges {
			if e.FromNodeID == fromNode && e.ToNodeID == toNode {
				edge = &e
				break
			}
		}

		step := PropagationStep{
			FromNode:  fromNode,
			ToNode:    toNode,
			EdgeType:  "unknown",
			Impact:    0.5,
			Reasoning: fmt.Sprintf("Issue propagates from %s to %s", fromNode, toNode),
			Timestamp: time.Now(),
		}

		if edge != nil {
			step.EdgeType = edge.Type
			step.Impact = edge.ErrorRate
			step.Reasoning = fmt.Sprintf("Edge with %0.2f%% error rate (%s)",
				edge.ErrorRate*100, edge.Type)
		}

		steps = append(steps, step)
	}

	// Calculate confidence
	confidence := a.calculatePathConfidence(steps, graph)

	return PropagationPath{
		Path:      steps,
		Confidence: confidence,
		Reasoning: fmt.Sprintf("Propagation path with %d steps", len(steps)),
	}
}

// calculatePathConfidence calculates confidence in a propagation path.
func (a *PropagationAnalyzer) calculatePathConfidence(
	steps []PropagationStep,
	graph *ServiceGraph,
) float64 {
	if len(steps) == 0 {
		return 0.0
	}

	confidence := 1.0

	// Shorter paths are more likely
	pathLength := len(steps)
	confidence *= math.Pow(0.9, float64(pathLength-1))

	// Paths with high impact edges are more likely
	totalImpact := 0.0
	for _, step := range steps {
		totalImpact += step.Impact
	}
	avgImpact := totalImpact / float64(pathLength)
	confidence *= (0.5 + avgImpact)

	return math.Min(confidence, 1.0)
}

// determineRootCandidate determines the most likely root cause service.
func (a *PropagationAnalyzer) determineRootCandidate(
	graph *ServiceGraph,
	affectedServices []string,
) string {
	if len(affectedServices) == 0 {
		return ""
	}

	// Find service with highest error rate among affected services
	var rootCandidate string
	maxErrorRate := 0.0

	for _, serviceID := range affectedServices {
		for _, node := range graph.Nodes {
			if node.ID == serviceID {
				if node.ErrorRate > maxErrorRate {
					maxErrorRate = node.ErrorRate
					rootCandidate = serviceID
				}
				break
			}
		}
	}

	// If no clear winner by error rate, use latency
	if rootCandidate == "" {
		maxLatency := 0.0
		for _, serviceID := range affectedServices {
			for _, node := range graph.Nodes {
				if node.ID == serviceID {
					if node.Latency > maxLatency {
						maxLatency = node.Latency
						rootCandidate = serviceID
					}
					break
				}
			}
		}
	}

	return rootCandidate
}

// calculatePropagationConfidence calculates overall confidence in propagation analysis.
func (a *PropagationAnalyzer) calculatePropagationConfidence(
	paths []PropagationPath,
	tree *PropagationTree,
) float64 {
	if len(paths) == 0 || tree == nil {
		return 0.0
	}

	confidence := 0.0

	// Confidence from paths
	if len(paths) > 0 {
		// Use best path's confidence
		confidence += paths[0].Confidence * 0.6
	}

	// Confidence from tree structure
	if tree.Root != nil {
		confidence += tree.Root.Impact * 0.4
	}

	return math.Min(confidence, 1.0)
}

// GetUpstreamImpact analyzes the upstream impact of a service issue.
func (a *PropagationAnalyzer) GetUpstreamImpact(
	ctx context.Context,
	serviceID string,
	timeWindow TimeWindow,
) (*UpstreamImpact, error) {
	// Get upstream services
	upstreamNodes, err := a.armsClient.GetUpstreamNodes(ctx, serviceID, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get upstream nodes: %w", err)
	}

	// Get service graph for metrics
	graph, err := a.armsClient.GetServiceGraph(ctx, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get service graph: %w", err)
	}

	impact := &UpstreamImpact{
		ServiceID:        serviceID,
		UpstreamServices: make([]ServiceImpact, 0),
		TotalImpact:      0.0,
	}

	for _, node := range upstreamNodes {
		// Find edge metrics
		var edgeErrorRate float64
		for _, edge := range graph.Edges {
			if edge.FromNodeID == node.ID && edge.ToNodeID == serviceID {
				edgeErrorRate = edge.ErrorRate
				break
			}
		}

		serviceImpact := ServiceImpact{
			ServiceID:    node.ID,
			ServiceName:  node.Name,
			ErrorRate:    node.ErrorRate,
			Latency:      node.Latency,
			EdgeErrorRate: edgeErrorRate,
			ImpactScore:  calculateServiceImpact(node, edgeErrorRate),
		}

		impact.UpstreamServices = append(impact.UpstreamServices, serviceImpact)
		impact.TotalImpact += serviceImpact.ImpactScore
	}

	// Sort by impact score
	sort.Slice(impact.UpstreamServices, func(i, j int) bool {
		return impact.UpstreamServices[i].ImpactScore > impact.UpstreamServices[j].ImpactScore
	})

	return impact, nil
}

// UpstreamImpact represents the upstream impact analysis.
type UpstreamImpact struct {
	ServiceID        string          `json:"service_id"`
	UpstreamServices []ServiceImpact `json:"upstream_services"`
	TotalImpact      float64         `json:"total_impact"`
}

// ServiceImpact represents the impact of a specific service.
type ServiceImpact struct {
	ServiceID     string  `json:"service_id"`
	ServiceName   string  `json:"service_name"`
	ErrorRate     float64 `json:"error_rate"`
	Latency       float64 `json:"latency"`
	EdgeErrorRate float64 `json:"edge_error_rate"`
	ImpactScore   float64 `json:"impact_score"`
}

// calculateServiceImpact calculates the impact score of a service.
func calculateServiceImpact(node ServiceNode, edgeErrorRate float64) float64 {
	impact := 0.0

	// Health status impact
	switch node.Health {
	case "unhealthy":
		impact += 0.5
	case "degraded":
		impact += 0.3
	}

	// Error rate impact
	impact += math.Min(node.ErrorRate*10, 0.3)

	// Edge error rate impact
	impact += math.Min(edgeErrorRate*10, 0.2)

	return math.Min(impact, 1.0)
}

// GetDownstreamImpact analyzes the downstream impact of a service issue.
func (a *PropagationAnalyzer) GetDownstreamImpact(
	ctx context.Context,
	serviceID string,
	timeWindow TimeWindow,
) (*DownstreamImpact, error) {
	// Get downstream services
	downstreamNodes, err := a.armsClient.GetDownstreamNodes(ctx, serviceID, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get downstream nodes: %w", err)
	}

	// Get service graph for metrics
	graph, err := a.armsClient.GetServiceGraph(ctx, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get service graph: %w", err)
	}

	impact := &DownstreamImpact{
		ServiceID:         serviceID,
		DownstreamServices: make([]ServiceImpact, 0),
		TotalImpact:       0.0,
	}

	for _, node := range downstreamNodes {
		// Find edge metrics
		var edgeErrorRate float64
		for _, edge := range graph.Edges {
			if edge.FromNodeID == serviceID && edge.ToNodeID == node.ID {
				edgeErrorRate = edge.ErrorRate
				break
			}
		}

		serviceImpact := ServiceImpact{
			ServiceID:     node.ID,
			ServiceName:   node.Name,
			ErrorRate:     node.ErrorRate,
			Latency:       node.Latency,
			EdgeErrorRate: edgeErrorRate,
			ImpactScore:   calculateServiceImpact(node, edgeErrorRate),
		}

		impact.DownstreamServices = append(impact.DownstreamServices, serviceImpact)
		impact.TotalImpact += serviceImpact.ImpactScore
	}

	// Sort by impact score
	sort.Slice(impact.DownstreamServices, func(i, j int) bool {
		return impact.DownstreamServices[i].ImpactScore > impact.DownstreamServices[j].ImpactScore
	})

	return impact, nil
}

// DownstreamImpact represents the downstream impact analysis.
type DownstreamImpact struct {
	ServiceID          string          `json:"service_id"`
	DownstreamServices []ServiceImpact `json:"downstream_services"`
	TotalImpact        float64         `json:"total_impact"`
}

// GetBlastRadius calculates the potential blast radius of a service failure.
func (a *PropagationAnalyzer) GetBlastRadius(
	ctx context.Context,
	serviceID string,
	timeWindow TimeWindow,
) (*BlastRadius, error) {
	// Get both upstream and downstream
	downstreamImpact, err := a.GetDownstreamImpact(ctx, serviceID, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get downstream impact: %w", err)
	}

	upstreamImpact, err := a.GetUpstreamImpact(ctx, serviceID, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to get upstream impact: %w", err)
	}

	return &BlastRadius{
		ServiceID:         serviceID,
		DownstreamImpact: downstreamImpact,
		UpstreamImpact:   upstreamImpact,
		TotalServices:    len(downstreamImpact.DownstreamServices) + len(upstreamImpact.UpstreamServices),
		RiskLevel:        calculateRiskLevel(downstreamImpact.TotalImpact + upstreamImpact.TotalImpact),
	}, nil
}

// BlastRadius represents the blast radius analysis.
type BlastRadius struct {
	ServiceID         string            `json:"service_id"`
	DownstreamImpact  *DownstreamImpact `json:"downstream_impact"`
	UpstreamImpact    *UpstreamImpact   `json:"upstream_impact"`
	TotalServices     int               `json:"total_services"`
	RiskLevel         string            `json:"risk_level"`
}

// calculateRiskLevel determines the risk level based on impact.
func calculateRiskLevel(totalImpact float64) string {
	if totalImpact > 3.0 {
		return "critical"
	}
	if totalImpact > 2.0 {
		return "high"
	}
	if totalImpact > 1.0 {
		return "medium"
	}
	return "low"
}
