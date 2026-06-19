// Package topology provides tests for ARMS Service Graph integration.
package topology

import (
	"context"
	"testing"
	"time"
)

func TestARMSClient(t *testing.T) {
	client := NewARMSClient(
		WithARMSRegion("cn-beijing"),
		WithARMSCredentials("test-key", "test-secret"),
	)

	t.Run("Get Service Graph", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		graph, err := client.GetServiceGraph(context.Background(), window)
		if err != nil {
			t.Fatalf("Failed to get service graph: %v", err)
		}

		if graph == nil {
			t.Fatal("Expected graph, got nil")
		}

		if len(graph.Nodes) == 0 {
			t.Error("Expected at least one node")
		}

		if len(graph.Edges) == 0 {
			t.Error("Expected at least one edge")
		}
	})

	t.Run("Get Node by ID", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		node, err := client.GetNode(context.Background(), "order-service", window)
		if err != nil {
			t.Fatalf("Failed to get node: %v", err)
		}

		if node == nil {
			t.Fatal("Expected node, got nil")
		}

		if node.ID != "order-service" {
			t.Errorf("Expected node ID order-service, got %s", node.ID)
		}
	})

	t.Run("Get Upstream Nodes", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		nodes, err := client.GetUpstreamNodes(context.Background(), "order-service", window)
		if err != nil {
			t.Fatalf("Failed to get upstream nodes: %v", err)
		}

		if len(nodes) == 0 {
			t.Error("Expected at least one upstream node")
		}

		// Check that api-gateway is upstream
		found := false
		for _, node := range nodes {
			if node.ID == "api-gateway" {
				found = true
				break
			}
		}

		if !found {
			t.Error("Expected api-gateway to be upstream of order-service")
		}
	})

	t.Run("Get Downstream Nodes", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		nodes, err := client.GetDownstreamNodes(context.Background(), "order-service", window)
		if err != nil {
			t.Fatalf("Failed to get downstream nodes: %v", err)
		}

		if len(nodes) == 0 {
			t.Error("Expected at least one downstream node")
		}

		// Check that postgres-order is downstream
		found := false
		for _, node := range nodes {
			if node.ID == "postgres-order" {
				found = true
				break
			}
		}

		if !found {
			t.Error("Expected postgres-order to be downstream of order-service")
		}
	})

	t.Run("Get Path Between Nodes", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		paths, err := client.GetPath(context.Background(), "api-gateway", "postgres-order", window)
		if err != nil {
			t.Fatalf("Failed to get path: %v", err)
		}

		if len(paths) == 0 {
			t.Error("Expected at least one path")
		}

		// Check that path includes expected nodes
		for _, path := range paths {
			if len(path.Nodes) == 0 {
				t.Error("Path should have nodes")
			}
		}
	})

	t.Run("Get Anomalous Nodes", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		nodes, err := client.GetAnomalousNodes(context.Background(), window)
		if err != nil {
			t.Fatalf("Failed to get anomalous nodes: %v", err)
		}

		// Should have at least order-service and postgres-order (both degraded)
		if len(nodes) < 2 {
			t.Errorf("Expected at least 2 anomalous nodes, got %d", len(nodes))
		}
	})

	t.Run("Get Critical Path", func(t *testing.T) {
		window := TimeWindow{
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		path, err := client.GetCriticalPath(context.Background(), window)
		if err != nil {
			t.Fatalf("Failed to get critical path: %v", err)
		}

		if path == nil {
			t.Error("Expected critical path, got nil")
		}

		if len(path.Nodes) == 0 {
			t.Error("Critical path should have nodes")
		}
	})
}

func TestServiceNode(t *testing.T) {
	t.Run("Service Node Fields", func(t *testing.T) {
		node := ServiceNode{
			ID:          "test-node",
			Name:        "Test Service",
			Type:        "service",
			Cluster:     "production",
			Namespace:   "default",
			Environment: "production",
			Health:      "healthy",
			ErrorRate:   0.001,
			Latency:     45.5,
			Throughput:  1000.0,
			LastUpdated: time.Now(),
		}

		if node.ID == "" {
			t.Error("Node ID should not be empty")
		}
		if node.Type == "" {
			t.Error("Node type should not be empty")
		}
	})
}

func TestServiceEdge(t *testing.T) {
	t.Run("Service Edge Fields", func(t *testing.T) {
		edge := ServiceEdge{
			ID:         "test-edge",
			FromNodeID: "node-a",
			ToNodeID:   "node-b",
			Type:       "http",
			Protocol:   "HTTP/1.1",
			RequestCount: 1000,
			ErrorCount: 10,
			ErrorRate:  0.01,
			Latency:    50.0,
			Throughput: 100.0,
			LastUpdated: time.Now(),
		}

		if edge.ID == "" {
			t.Error("Edge ID should not be empty")
		}
		if edge.FromNodeID == "" || edge.ToNodeID == "" {
			t.Error("FromNodeID and ToNodeID should not be empty")
		}
	})
}

func TestServiceGraph(t *testing.T) {
	t.Run("Service Graph Structure", func(t *testing.T) {
		graph := &ServiceGraph{
			Nodes: []ServiceNode{
				{ID: "node-1", Name: "Node 1"},
				{ID: "node-2", Name: "Node 2"},
			},
			Edges: []ServiceEdge{
				{ID: "edge-1", FromNodeID: "node-1", ToNodeID: "node-2"},
			},
			Metadata: GraphMetadata{
				Timestamp:     time.Now(),
				TotalServices: 2,
				TotalEdges:    1,
			},
		}

		if len(graph.Nodes) != 2 {
			t.Errorf("Expected 2 nodes, got %d", len(graph.Nodes))
		}
		if len(graph.Edges) != 1 {
			t.Errorf("Expected 1 edge, got %d", len(graph.Edges))
		}
	})
}

func TestPath(t *testing.T) {
	t.Run("Path Structure", func(t *testing.T) {
		path := Path{
			Nodes: []string{"node-1", "node-2", "node-3"},
			Edges: []string{"edge-1", "edge-2"},
		}

		if len(path.Nodes) != 3 {
			t.Errorf("Expected 3 nodes, got %d", len(path.Nodes))
		}
		if len(path.Edges) != 2 {
			t.Errorf("Expected 2 edges, got %d", len(path.Edges))
		}
	})
}

func TestARMSClientOptions(t *testing.T) {
	t.Run("New ARMS Client with Options", func(t *testing.T) {
		client := NewARMSClient(
			WithARMSRegion("cn-hangzhou"),
			WithARMSCredentials("my-key", "my-secret"),
			WithARMSTimeout(60*time.Second),
		)

		if client.region != "cn-hangzhou" {
			t.Errorf("Expected region cn-hangzhou, got %s", client.region)
		}

		if client.accessKeyID != "my-key" {
			t.Errorf("Expected access key ID my-key, got %s", client.accessKeyID)
		}

		if client.timeout != 60*time.Second {
			t.Errorf("Expected timeout 60s, got %v", client.timeout)
		}
	})
}

func TestCache(t *testing.T) {
	t.Run("Cache Expiration", func(t *testing.T) {
		client := NewARMSClient()

		// Initially cache should be invalid
		if client.isCacheValid() {
			t.Error("Expected cache to be invalid initially")
		}

		// Update cache with short TTL
		graph := &ServiceGraph{Nodes: []ServiceNode{{ID: "test"}}}
		client.updateCachedGraph(graph, 1*time.Millisecond)

		// Cache should be valid now
		if !client.isCacheValid() {
			t.Error("Expected cache to be valid after update")
		}

		// Wait for cache to expire
		time.Sleep(2 * time.Millisecond)

		// Cache should be invalid now
		if client.isCacheValid() {
			t.Error("Expected cache to be expired")
		}
	})

	t.Run("Cache Retrieval", func(t *testing.T) {
		client := NewARMSClient()

		graph := &ServiceGraph{
			Nodes: []ServiceNode{{ID: "cached-node"}},
		}

		client.updateCachedGraph(graph, 5*time.Minute)

		cached := client.getCachedGraph()
		if cached == nil {
			t.Fatal("Expected cached graph, got nil")
		}

		if len(cached.Nodes) != 1 {
			t.Errorf("Expected 1 cached node, got %d", len(cached.Nodes))
		}
	})
}

func TestConvertFromARMS(t *testing.T) {
	t.Run("Convert ARMS Response to ServiceGraph", func(t *testing.T) {
		response := armsServiceTopologyResponse{
			Data: struct {
				Nodes []armsNode `json:"nodes"`
				Edges []armsEdge `json:"edges"`
			}{
				Nodes: []armsNode{
					{
						ID:    "node-1",
						Name:  "Node 1",
						Type:  "service",
						Health: "healthy",
					},
				},
				Edges: []armsEdge{
					{
						FromNodeID: "node-1",
						ToNodeID:   "node-2",
						Type:       "http",
					},
				},
			},
		}

		graph := ConvertFromARMS(response)

		if len(graph.Nodes) != 1 {
			t.Errorf("Expected 1 node, got %d", len(graph.Nodes))
		}

		if len(graph.Edges) != 1 {
			t.Errorf("Expected 1 edge, got %d", len(graph.Edges))
		}
	})
}
