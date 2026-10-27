// Package interfaces provides tests for the engine-wired endpoints
// (topology, changes) added to the API server.
package interfaces

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/config"
)

func testSettings() *config.Settings {
	return &config.Settings{
		Environment: config.EnvDevelopment,
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}
}

// TestChangesEndpoint verifies the changes endpoint returns the seeded
// deployment data after server creation.
func TestChangesEndpoint(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	t.Run("returns_seeded_deployments", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/changes", nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d (body: %s)", w.Code, w.Body.String())
		}

		var resp struct {
			Count       int `json:"count"`
			Deployments []struct {
				ID      string `json:"id"`
				Service string `json:"service"`
				Status  string `json:"status"`
			} `json:"deployments"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if resp.Count == 0 {
			t.Error("Expected seeded deployments, got count 0")
		}
		if len(resp.Deployments) != resp.Count {
			t.Errorf("count (%d) != len(deployments) (%d)", resp.Count, len(resp.Deployments))
		}

		// Verify a known seeded deployment is present.
		found := false
		for _, d := range resp.Deployments {
			if d.Service == "order-service" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected order-service in seeded deployments")
		}
	})
}

// TestTopologyEndpoint verifies the topology endpoint returns 200 with an
// honest (empty) graph when no real ARMS source is configured — never
// fabricated demo nodes.
func TestTopologyEndpoint(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	t.Run("returns_service_graph", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/topology", nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d (body: %s)", w.Code, w.Body.String())
		}

		var resp struct {
			Status   string `json:"status"`
			Topology struct {
				Nodes []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"nodes"`
				Edges []struct {
					FromNodeID string `json:"from_node_id"`
					ToNodeID   string `json:"to_node_id"`
				} `json:"edges"`
			} `json:"topology"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if resp.Status != "ok" {
			t.Errorf("Expected status ok, got %s", resp.Status)
		}
		// No real ARMS source under test settings → the honest empty source
		// serves an empty graph. Fabricated demo nodes must NOT leak through.
		if len(resp.Topology.Nodes) != 0 {
			t.Errorf("Expected empty topology nodes (no real source), got %d", len(resp.Topology.Nodes))
		}
		if len(resp.Topology.Edges) != 0 {
			t.Errorf("Expected empty topology edges (no real source), got %d", len(resp.Topology.Edges))
		}
	})
}

// TestHealthVersionedPath verifies the /api/v1/health route used by the
// web console exists alongside the legacy /health route.
func TestHealthVersionedPath(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	for _, path := range []string{"/health", "/api/v1/health"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET %s: expected 200, got %d", path, w.Code)
		}
	}
}
