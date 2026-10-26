// Package topology provides service topology integration for OpsAgent.
// This module integrates with Aliyun ARMS Service Graph to enable
// topology-aware root cause analysis.
package topology

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// ServiceNode represents a node in the service topology.
type ServiceNode struct {
	// Node identification
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"` // service, database, cache, queue, etc.

	// Service metadata
	Cluster      string `json:"cluster,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	Environment  string `json:"environment,omitempty"`
	Language     string `json:"language,omitempty"`
	Runtime      string `json:"runtime,omitempty"`

	// Health and metrics
	Health      string   `json:"health"` // healthy, degraded, unhealthy
	ErrorRate   float64  `json:"error_rate"`
	Latency     float64  `json:"latency"`     // in milliseconds
	Throughput  float64  `json:"throughput"`  // requests per second
	Tags        map[string]string `json:"tags,omitempty"`

	// Timestamp
	LastUpdated time.Time `json:"last_updated"`
}

// ServiceEdge represents a relationship between services.
type ServiceEdge struct {
	// Edge identification
	ID         string `json:"id"`
	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`

	// Edge metadata
	Type       string `json:"type"` // http, rpc, database, cache, message_queue
	Protocol   string `json:"protocol,omitempty"`

	// Traffic metrics
	RequestCount    int64   `json:"request_count"`
	ErrorCount      int64   `json:"error_count"`
	ErrorRate       float64 `json:"error_rate"`
	Latency         float64 `json:"latency"`    // in milliseconds
	P95Latency      float64 `json:"p95_latency"` // in milliseconds
	P99Latency      float64 `json:"p99_latency"` // in milliseconds
	Throughput      float64 `json:"throughput"`  // requests per second

	// Timestamp
	LastUpdated time.Time `json:"last_updated"`
}

// ServiceGraph represents the complete service topology.
type ServiceGraph struct {
	Nodes    []ServiceNode `json:"nodes"`
	Edges    []ServiceEdge `json:"edges"`
	Metadata GraphMetadata  `json:"metadata"`
}

// GraphMetadata contains metadata about the service graph.
type GraphMetadata struct {
	Timestamp     time.Time `json:"timestamp"`
	TimeWindow    TimeWindow `json:"time_window"`
	Cluster       string    `json:"cluster,omitempty"`
	Environment   string    `json:"environment,omitempty"`
	TotalServices int       `json:"total_services"`
	TotalEdges    int       `json:"total_edges"`
}

// TimeWindow represents a time range for data collection.
type TimeWindow struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

// ARMSClient implements the Aliyun ARMS Service Graph client.
type ARMSClient struct {
	baseURL     string
	accessKeyID     string
	accessKeySecret string
	region      string
	httpClient  *http.Client
	timeout     time.Duration
	mu          sync.RWMutex
	cache       *graphCache
}

// graphCache caches service graph data.
type graphCache struct {
	graph      *ServiceGraph
	expiration time.Time
	mu         sync.RWMutex
}

// ARMSClientOption configures an ARMSClient.
type ARMSClientOption func(*ARMSClient)

// WithARMSBaseURL sets the ARMS base URL.
func WithARMSBaseURL(url string) ARMSClientOption {
	return func(c *ARMSClient) {
		c.baseURL = url
	}
}

// WithARMSRegion sets the Aliyun region.
func WithARMSRegion(region string) ARMSClientOption {
	return func(c *ARMSClient) {
		c.region = region
	}
}

// WithARMSCredentials sets the Aliyun credentials.
func WithARMSCredentials(accessKeyID, accessKeySecret string) ARMSClientOption {
	return func(c *ARMSClient) {
		c.accessKeyID = accessKeyID
		c.accessKeySecret = accessKeySecret
	}
}

// WithARMSTimeout sets the HTTP timeout.
func WithARMSTimeout(timeout time.Duration) ARMSClientOption {
	return func(c *ARMSClient) {
		c.timeout = timeout
	}
}

// NewARMSClient creates a new ARMS Service Graph client.
func NewARMSClient(opts ...ARMSClientOption) *ARMSClient {
	c := &ARMSClient{
		baseURL:    "https://arms.cn-beijing.aliyuncs.com",
		region:     "cn-beijing",
		httpClient: &http.Client{},
		timeout:    30 * time.Second,
		cache:      &graphCache{},
	}

	for _, opt := range opts {
		opt(c)
	}

	c.httpClient.Timeout = c.timeout

	return c
}

// GetServiceGraph retrieves the service topology graph.
func (c *ARMSClient) GetServiceGraph(ctx context.Context, window TimeWindow) (*ServiceGraph, error) {
	// Check cache first
	if c.isCacheValid() {
		if cached := c.getCachedGraph(); cached != nil {
			return cached, nil
		}
	}

	// In a real implementation, this would:
	// 1. Call ARMS API to get service graph
	// 2. Parse response
	// 3. Cache the result

	// For now, return mock data
	mockGraph := &ServiceGraph{
		Nodes: []ServiceNode{
			{
				ID:          "api-gateway",
				Name:        "api-gateway",
				Type:        "service",
				Cluster:     "production",
				Namespace:   "default",
				Environment: "production",
				Health:      "healthy",
				ErrorRate:   0.001,
				Latency:     45.5,
				Throughput:  1250.0,
			},
			{
				ID:          "order-service",
				Name:        "order-service",
				Type:        "service",
				Cluster:     "production",
				Namespace:   "default",
				Environment: "production",
				Health:      "degraded",
				ErrorRate:   0.025,
				Latency:     120.5,
				Throughput:  850.0,
			},
			{
				ID:          "payment-service",
				Name:        "payment-service",
				Type:        "service",
				Cluster:     "production",
				Namespace:   "default",
				Environment: "production",
				Health:      "healthy",
				ErrorRate:   0.002,
				Latency:     85.0,
				Throughput:  320.0,
			},
			{
				ID:          "user-service",
				Name:        "user-service",
				Type:        "service",
				Cluster:     "production",
				Namespace:   "default",
				Environment: "production",
				Health:      "healthy",
				ErrorRate:   0.001,
				Latency:     35.0,
				Throughput:  1500.0,
			},
			{
				ID:          "postgres-order",
				Name:        "postgres-order",
				Type:        "database",
				Cluster:     "production",
				Namespace:   "default",
				Environment: "production",
				Health:      "degraded",
				ErrorRate:   0.015,
				Latency:     25.5,
				Throughput:  850.0,
			},
			{
				ID:          "redis-cache",
				Name:        "redis-cache",
				Type:        "cache",
				Cluster:     "production",
				Namespace:   "default",
				Environment: "production",
				Health:      "healthy",
				ErrorRate:   0.000,
				Latency:     2.5,
				Throughput:  2500.0,
			},
		},
		Edges: []ServiceEdge{
			{
				ID:         "edge-1",
				FromNodeID: "api-gateway",
				ToNodeID:   "order-service",
				Type:       "http",
				Protocol:   "HTTP/1.1",
				RequestCount: 85000,
				ErrorCount: 2125,
				ErrorRate:  0.025,
				Latency:    120.5,
				Throughput: 850.0,
			},
			{
				ID:         "edge-2",
				FromNodeID: "api-gateway",
				ToNodeID:   "payment-service",
				Type:       "http",
				Protocol:   "HTTP/1.1",
				RequestCount: 32000,
				ErrorCount: 64,
				ErrorRate:  0.002,
				Latency:    85.0,
				Throughput: 320.0,
			},
			{
				ID:         "edge-3",
				FromNodeID: "api-gateway",
				ToNodeID:   "user-service",
				Type:       "http",
				Protocol:   "HTTP/1.1",
				RequestCount: 150000,
				ErrorCount: 150,
				ErrorRate:  0.001,
				Latency:    35.0,
				Throughput: 1500.0,
			},
			{
				ID:         "edge-4",
				FromNodeID: "order-service",
				ToNodeID:   "postgres-order",
				Type:       "database",
				Protocol:   "postgresql",
				RequestCount: 120000,
				ErrorCount: 1800,
				ErrorRate:  0.015,
				Latency:    25.5,
				Throughput: 1200.0,
			},
			{
				ID:         "edge-5",
				FromNodeID: "order-service",
				ToNodeID:   "redis-cache",
				Type:       "cache",
				Protocol:   "redis",
				RequestCount: 450000,
				ErrorCount: 0,
				ErrorRate:  0.0,
				Latency:    2.5,
				Throughput: 5000.0,
			},
			{
				ID:         "edge-6",
				FromNodeID: "user-service",
				ToNodeID:   "redis-cache",
				Type:       "cache",
				Protocol:   "redis",
				RequestCount: 2000000,
				ErrorCount: 0,
				ErrorRate:  0.0,
				Latency:    2.0,
				Throughput: 20000.0,
			},
		},
		Metadata: GraphMetadata{
			Timestamp:     time.Now(),
			TimeWindow:    window,
			Cluster:       "production",
			Environment:   "production",
			TotalServices: 4,
			TotalEdges:    6,
		},
	}

	// Update cache
	c.updateCachedGraph(mockGraph, 5*time.Minute)

	return mockGraph, nil
}

// GetNode retrieves a specific service node by ID.
func (c *ARMSClient) GetNode(ctx context.Context, nodeID string, window TimeWindow) (*ServiceNode, error) {
	graph, err := c.GetServiceGraph(ctx, window)
	if err != nil {
		return nil, err
	}

	for _, node := range graph.Nodes {
		if node.ID == nodeID {
			return &node, nil
		}
	}

	return nil, fmt.Errorf("node not found: %s", nodeID)
}

// GetUpstreamNodes returns all upstream (calling) nodes for a given node.
func (c *ARMSClient) GetUpstreamNodes(ctx context.Context, nodeID string, window TimeWindow) ([]ServiceNode, error) {
	graph, err := c.GetServiceGraph(ctx, window)
	if err != nil {
		return nil, err
	}

	var upstreamNodes []ServiceNode

	for _, edge := range graph.Edges {
		if edge.ToNodeID == nodeID {
			// Find the from node
			for _, node := range graph.Nodes {
				if node.ID == edge.FromNodeID {
					upstreamNodes = append(upstreamNodes, node)
					break
				}
			}
		}
	}

	return upstreamNodes, nil
}

// GetDownstreamNodes returns all downstream (callee) nodes for a given node.
func (c *ARMSClient) GetDownstreamNodes(ctx context.Context, nodeID string, window TimeWindow) ([]ServiceNode, error) {
	graph, err := c.GetServiceGraph(ctx, window)
	if err != nil {
		return nil, err
	}

	var downstreamNodes []ServiceNode

	for _, edge := range graph.Edges {
		if edge.FromNodeID == nodeID {
			// Find the to node
			for _, node := range graph.Nodes {
				if node.ID == edge.ToNodeID {
					downstreamNodes = append(downstreamNodes, node)
					break
				}
			}
		}
	}

	return downstreamNodes, nil
}

// GetPath returns all paths between two nodes.
func (c *ARMSClient) GetPath(ctx context.Context, fromNodeID, toNodeID string, window TimeWindow) ([]Path, error) {
	graph, err := c.GetServiceGraph(ctx, window)
	if err != nil {
		return nil, err
	}

	// Build adjacency list
	adjacency := make(map[string][]string)
	for _, edge := range graph.Edges {
		adjacency[edge.FromNodeID] = append(adjacency[edge.FromNodeID], edge.ToNodeID)
	}

	// Find all paths using BFS
	paths := c.findAllPaths(adjacency, fromNodeID, toNodeID, []string{})

	return paths, nil
}

// Path represents a path between services.
type Path struct {
	Nodes []string `json:"nodes"`
	Edges []string `json:"edges"`
}

// findAllPaths recursively finds all paths between nodes.
func (c *ARMSClient) findAllPaths(adjacency map[string][]string, current, target string, visited []string) []Path {
	// Avoid cycles
	for _, v := range visited {
		if v == current {
			return []Path{}
		}
	}

	visited = append(visited, current)

	if current == target {
		return []Path{{Nodes: append([]string{}, visited...)}}
	}

	paths := make([]Path, 0)

	for _, next := range adjacency[current] {
		subPaths := c.findAllPaths(adjacency, next, target, visited)
		for _, subPath := range subPaths {
			paths = append(paths, subPath)
		}
	}

	return paths
}

// Health checks if the ARMS API is accessible.
func (c *ARMSClient) Health(ctx context.Context) error {
	// In a real implementation, ping the ARMS API
	return fmt.Errorf("not implemented")
}

// Cache methods

func (c *ARMSClient) isCacheValid() bool {
	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()
	return time.Now().Before(c.cache.expiration)
}

func (c *ARMSClient) getCachedGraph() *ServiceGraph {
	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()
	return c.cache.graph
}

func (c *ARMSClient) updateCachedGraph(graph *ServiceGraph, ttl time.Duration) {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()
	c.cache.graph = graph
	c.cache.expiration = time.Now().Add(ttl)
}

// HTTP helpers

func (c *ARMSClient) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	u := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add ARMS authentication headers
	c.addAuthHeaders(req)

	// Add common headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	return c.httpClient.Do(req)
}

func (c *ARMSClient) addAuthHeaders(req *http.Request) {
	// In a real implementation, add Aliyun signature authentication
	// For now, just set access key ID
	if c.accessKeyID != "" {
		req.Header.Set("X-Acs-AccessKeyId", c.accessKeyID)
	}
}

func (c *ARMSClient) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u := path
	if params != nil && len(params) > 0 {
		u += "?" + params.Encode()
	}

	resp, err := c.doRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// ARMS API types (for future implementation)

type armsServiceTopologyResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Nodes []armsNode `json:"nodes"`
		Edges []armsEdge `json:"edges"`
	} `json:"data"`
}

type armsNode struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Cluster    string `json:"cluster"`
	Namespace  string `json:"namespace"`
	Health     string `json:"health"`
	Metrics    struct {
		ErrorRate  float64 `json:"errorRate"`
		Latency    float64 `json:"latency"`
		Throughput float64 `json:"throughput"`
	} `json:"metrics"`
}

type armsEdge struct {
	FromNodeID string `json:"fromNodeId"`
	ToNodeID   string `json:"toNodeId"`
	Type       string `json:"type"`
	Protocol   string `json:"protocol"`
	Metrics    struct {
		RequestCount int64   `json:"requestCount"`
		ErrorCount   int64   `json:"errorCount"`
		ErrorRate    float64 `json:"errorRate"`
		Latency      float64 `json:"latency"`
		Throughput   float64 `json:"throughput"`
	} `json:"metrics"`
}

// ConvertFromARMS converts ARMS response to ServiceGraph.
func ConvertFromARMS(response armsServiceTopologyResponse) *ServiceGraph {
	graph := &ServiceGraph{
		Nodes: make([]ServiceNode, 0, len(response.Data.Nodes)),
		Edges: make([]ServiceEdge, 0, len(response.Data.Edges)),
	}

	for _, node := range response.Data.Nodes {
		graph.Nodes = append(graph.Nodes, ServiceNode{
			ID:         node.ID,
			Name:       node.Name,
			Type:       node.Type,
			Cluster:    node.Cluster,
			Namespace:  node.Namespace,
			Health:     node.Health,
			ErrorRate:  node.Metrics.ErrorRate,
			Latency:    node.Metrics.Latency,
			Throughput: node.Metrics.Throughput,
			LastUpdated: time.Now(),
		})
	}

	for _, edge := range response.Data.Edges {
		graph.Edges = append(graph.Edges, ServiceEdge{
			ID:         fmt.Sprintf("%s-%s", edge.FromNodeID, edge.ToNodeID),
			FromNodeID: edge.FromNodeID,
			ToNodeID:   edge.ToNodeID,
			Type:       edge.Type,
			Protocol:   edge.Protocol,
			RequestCount: edge.Metrics.RequestCount,
			ErrorCount:   edge.Metrics.ErrorCount,
			ErrorRate:    edge.Metrics.ErrorRate,
			Latency:      edge.Metrics.Latency,
			Throughput:   edge.Metrics.Throughput,
			LastUpdated:  time.Now(),
		})
	}

	return graph
}

// GetAnomalousNodes returns nodes with anomalous metrics.
func (c *ARMSClient) GetAnomalousNodes(ctx context.Context, window TimeWindow) ([]ServiceNode, error) {
	graph, err := c.GetServiceGraph(ctx, window)
	if err != nil {
		return nil, err
	}

	var anomalous []ServiceNode

	for _, node := range graph.Nodes {
		// Check if node is unhealthy
		if node.Health == "unhealthy" || node.Health == "degraded" {
			anomalous = append(anomalous, node)
			continue
		}

		// Check error rate threshold (>1%)
		if node.ErrorRate > 0.01 {
			anomalous = append(anomalous, node)
			continue
		}

		// Check latency threshold (>500ms)
		if node.Latency > 500 {
			anomalous = append(anomalous, node)
			continue
		}
	}

	return anomalous, nil
}

// GetCriticalPath returns the most critical path in the topology.
func (c *ARMSClient) GetCriticalPath(ctx context.Context, window TimeWindow) (*Path, error) {
	graph, err := c.GetServiceGraph(ctx, window)
	if err != nil {
		return nil, err
	}

	// Find path with highest error rate
	var criticalPath *Path
	maxErrorRate := 0.0

	for _, edge := range graph.Edges {
		if edge.ErrorRate > maxErrorRate {
			maxErrorRate = edge.ErrorRate
			criticalPath = &Path{
				Nodes: []string{edge.FromNodeID, edge.ToNodeID},
				Edges: []string{edge.ID},
			}
		}
	}

	if criticalPath == nil {
		return nil, fmt.Errorf("no critical path found")
	}

	return criticalPath, nil
}
