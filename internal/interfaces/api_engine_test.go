// Package interfaces provides tests for the engine-wired endpoints
// (topology, changes) added to the API server.
package interfaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
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

// fakeChangeSource is a test double for sources.ChangeSource. It records the
// window it was asked for so tests can assert the handler honours query params.
type fakeChangeSource struct {
	changes   []models.Change
	err       error
	calls     int
	gotStart  time.Time
	gotEnd    time.Time
	byService map[string][]models.Change
}

var _ sources.ChangeSource = (*fakeChangeSource)(nil)

func (f *fakeChangeSource) QueryChanges(_ context.Context, start, end time.Time) ([]models.Change, error) {
	f.calls++
	f.gotStart, f.gotEnd = start, end
	if f.err != nil {
		return nil, f.err
	}
	return f.changes, nil
}

func (f *fakeChangeSource) QueryChangesByService(_ context.Context, service string, _, _ time.Time) ([]models.Change, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byService[service], nil
}

func (f *fakeChangeSource) GetLastChange(_ context.Context, service string) (*models.Change, error) {
	if len(f.byService[service]) == 0 {
		return nil, nil
	}
	last := f.byService[service][0]
	return &last, nil
}

// changesEnvelope mirrors the /api/v1/changes response contract.
type changesEnvelope struct {
	Changes []struct {
		ID      string            `json:"id"`
		Kind    string            `json:"kind"`
		System  string            `json:"system"`
		Service string            `json:"service"`
		Message string            `json:"message"`
		Time    string            `json:"time"`
		Details map[string]string `json:"details"`
	} `json:"changes"`
	Count     int  `json:"count"`
	Total     int  `json:"total"`
	Truncated bool `json:"truncated"`
	Stats     *struct {
		Total       int            `json:"total"`
		ByKind      map[string]int `json:"by_kind"`
		Services    int            `json:"services"`
		TopServices []string       `json:"top_services"`
	} `json:"stats"`
	Source struct {
		Real bool   `json:"real"`
		Type string `json:"type"`
		Hint string `json:"hint"`
	} `json:"source"`
	Window *struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"window"`
	// Error shape (present only on failure responses).
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details string `json:"details"`
	} `json:"error"`
}

// TestChangesEndpoint verifies the changes endpoint serves ONLY the real change
// source: an unconfigured source yields an honest empty list (never mock
// records), a configured source is mapped to DTOs newest-first, and an upstream
// failure surfaces as 502 rather than falling back to fabricated data.
func TestChangesEndpoint(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	// Each subtest pins the change-source fields explicitly so the assertions
	// hold regardless of whether the machine running the tests has K8S_*/e-cam
	// credentials in its environment.
	t.Run("unconfigured_source_returns_honest_empty", func(t *testing.T) {
		// A source that would fail loudly if the handler ever touched it while
		// changeSourceReal is false — proving the mock/fallback is never queried.
		server.changeSource = &fakeChangeSource{err: errors.New("must not be queried")}
		server.changeSourceReal = false

		req := httptest.NewRequest("GET", "/api/v1/changes", nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d (body: %s)", w.Code, w.Body.String())
		}

		var resp changesEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if resp.Source.Real {
			t.Error("Expected source.real=false when no real change source is configured")
		}
		if resp.Source.Hint == "" {
			t.Error("Expected a hint explaining that no real change source is configured")
		}
		if resp.Count != 0 {
			t.Errorf("Expected count 0, got %d", resp.Count)
		}
		if len(resp.Changes) != 0 {
			t.Errorf("Expected empty changes, got %d entries", len(resp.Changes))
		}
		if resp.Changes == nil {
			t.Error("changes must serialize as [] not null")
		}
		if fake, ok := server.changeSource.(*fakeChangeSource); ok && fake.calls != 0 {
			t.Errorf("Unreal source must not be queried, got %d calls", fake.calls)
		}
	})

	t.Run("real_source_maps_to_dtos_newest_first", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		older := now.Add(-3 * time.Hour)
		newer := now.Add(-30 * time.Minute)

		fake := &fakeChangeSource{changes: []models.Change{
			{
				ID:          "deploy-old",
				Timestamp:   older,
				Service:     "order-service",
				Type:        "deployment",
				Description: "Deployment updated: registry/order-service:v2.3.1",
				Details:     map[string]string{"namespace": "prod", "image": "registry/order-service:v2.3.1"},
			},
			{
				ID:          "config-new",
				Timestamp:   newer,
				Service:     "payment-service",
				Type:        "config",
				Description: "ConfigMap payment-config updated",
				Details:     map[string]string{"namespace": "prod", "system": "kubernetes"},
			},
		}}
		server.changeSource = fake
		server.changeSourceReal = true

		req := httptest.NewRequest("GET", "/api/v1/changes?hours=24", nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d (body: %s)", w.Code, w.Body.String())
		}

		var resp changesEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if !resp.Source.Real {
			t.Error("Expected source.real=true for a configured real source")
		}
		if resp.Source.Type != "kubernetes" {
			t.Errorf("Expected source.type kubernetes, got %q", resp.Source.Type)
		}
		if resp.Count != 2 || len(resp.Changes) != 2 {
			t.Fatalf("Expected 2 changes (count=%d, len=%d)", resp.Count, len(resp.Changes))
		}
		if fake.calls != 1 {
			t.Errorf("Expected exactly 1 QueryChanges call, got %d", fake.calls)
		}

		// Newest first.
		if resp.Changes[0].ID != "config-new" || resp.Changes[1].ID != "deploy-old" {
			t.Errorf("Expected newest-first order [config-new deploy-old], got [%s %s]",
				resp.Changes[0].ID, resp.Changes[1].ID)
		}

		first := resp.Changes[0]
		if first.Kind != "config" {
			t.Errorf("Expected kind config, got %q", first.Kind)
		}
		if first.System != "kubernetes" {
			t.Errorf("Expected system kubernetes, got %q", first.System)
		}
		if first.Service != "payment-service" {
			t.Errorf("Expected service payment-service, got %q", first.Service)
		}
		if first.Message != "ConfigMap payment-config updated" {
			t.Errorf("Expected message mapped from Description, got %q", first.Message)
		}
		if !newer.Equal(mustParseTime(t, first.Time)) {
			t.Errorf("Expected time %s, got %s", newer.Format(time.RFC3339), first.Time)
		}

		second := resp.Changes[1]
		if second.Kind != "deployment" {
			t.Errorf("Expected kind deployment, got %q", second.Kind)
		}
		if second.Details["image"] != "registry/order-service:v2.3.1" {
			t.Errorf("Expected details passthrough, got %v", second.Details)
		}

		// The queried window must match hours=24 (bounded, not the mock's window).
		if fake.gotEnd.Sub(fake.gotStart) != 24*time.Hour {
			t.Errorf("Expected a 24h window, got %s", fake.gotEnd.Sub(fake.gotStart))
		}
		if resp.Window == nil {
			t.Fatal("Expected a window object for a real source response")
		}
	})

	t.Run("hours_is_capped_at_168", func(t *testing.T) {
		fake := &fakeChangeSource{}
		server.changeSource = fake
		server.changeSourceReal = true

		req := httptest.NewRequest("GET", "/api/v1/changes?hours=9999", nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d (body: %s)", w.Code, w.Body.String())
		}
		if got := fake.gotEnd.Sub(fake.gotStart); got != 168*time.Hour {
			t.Errorf("Expected the window capped at 168h, got %s", got)
		}
	})

	t.Run("explicit_start_end_take_precedence", func(t *testing.T) {
		fake := &fakeChangeSource{}
		server.changeSource = fake
		server.changeSourceReal = true

		start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
		url := "/api/v1/changes?hours=1&start=" + start.Format(time.RFC3339) + "&end=" + end.Format(time.RFC3339)

		req := httptest.NewRequest("GET", url, nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d (body: %s)", w.Code, w.Body.String())
		}
		if !fake.gotStart.Equal(start) || !fake.gotEnd.Equal(end) {
			t.Errorf("Expected explicit window %s..%s, got %s..%s", start, end, fake.gotStart, fake.gotEnd)
		}
	})

	t.Run("invalid_params_are_rejected", func(t *testing.T) {
		server.changeSource = &fakeChangeSource{}
		server.changeSourceReal = true

		for _, query := range []string{"hours=0", "hours=-5", "hours=abc", "start=not-a-time"} {
			req := httptest.NewRequest("GET", "/api/v1/changes?"+query, nil)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("query %q: expected 400, got %d (body: %s)", query, w.Code, w.Body.String())
			}
		}
	})

	t.Run("limit_truncates_payload_but_stats_cover_full_window", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		changes := make([]models.Change, 0, 5)
		for i := 0; i < 5; i++ {
			service := "svc-a"
			kind := "k8s_event"
			if i >= 3 {
				service = "svc-b"
				kind = "deployment"
			}
			changes = append(changes, models.Change{
				ID:        fmt.Sprintf("c-%d", i),
				Timestamp: now.Add(-time.Duration(i) * time.Minute),
				Service:   service,
				Type:      kind,
			})
		}
		fake := &fakeChangeSource{changes: changes}
		server.changeSource = fake
		server.changeSourceReal = true

		var resp changesEnvelope
		for round := 0; round < 2; round++ {
			req := httptest.NewRequest("GET", "/api/v1/changes?limit=2", nil)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("round %d: expected 200, got %d (body: %s)", round, w.Code, w.Body.String())
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}
		}

		if fake.calls != 1 {
			t.Errorf("Expected the second request to be served from cache (1 call), got %d", fake.calls)
		}
		if resp.Count != 2 || len(resp.Changes) != 2 {
			t.Fatalf("Expected payload truncated to 2, got count=%d len=%d", resp.Count, len(resp.Changes))
		}
		if resp.Total != 5 || !resp.Truncated {
			t.Errorf("Expected total=5 truncated=true, got total=%d truncated=%v", resp.Total, resp.Truncated)
		}
		if resp.Changes[0].ID != "c-0" {
			t.Errorf("Expected newest change first after truncation, got %s", resp.Changes[0].ID)
		}
		if resp.Stats == nil {
			t.Fatal("Expected stats in the response")
		}
		if resp.Stats.Total != 5 || resp.Stats.Services != 2 {
			t.Errorf("Expected stats over the full window (total=5 services=2), got %+v", resp.Stats)
		}
		if resp.Stats.ByKind["k8s_event"] != 3 || resp.Stats.ByKind["deployment"] != 2 {
			t.Errorf("Expected by_kind k8s_event=3 deployment=2, got %v", resp.Stats.ByKind)
		}
		if len(resp.Stats.TopServices) == 0 || resp.Stats.TopServices[0] != "svc-a" {
			t.Errorf("Expected svc-a as top service, got %v", resp.Stats.TopServices)
		}
	})

	t.Run("invalid_limit_is_rejected", func(t *testing.T) {
		server.changeSource = &fakeChangeSource{}
		server.changeSourceReal = true

		for _, query := range []string{"limit=0", "limit=-1", "limit=abc"} {
			req := httptest.NewRequest("GET", "/api/v1/changes?"+query, nil)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("query %q: expected 400, got %d", query, w.Code)
			}
		}
	})

	t.Run("source_error_returns_502_without_fallback", func(t *testing.T) {
		server.changeSource = &fakeChangeSource{err: errors.New("K8s deployments request failed: connection refused")}
		server.changeSourceReal = true

		req := httptest.NewRequest("GET", "/api/v1/changes", nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)

		if w.Code != http.StatusBadGateway {
			t.Fatalf("Expected 502, got %d (body: %s)", w.Code, w.Body.String())
		}

		var resp changesEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}
		if resp.Error == nil || resp.Error.Code != "change_source_error" {
			t.Fatalf("Expected error code change_source_error, got %+v", resp.Error)
		}
		if resp.Error.Details == "" {
			t.Error("Expected the upstream error message in details")
		}
		if len(resp.Changes) != 0 {
			t.Errorf("Expected no fabricated changes on error, got %d", len(resp.Changes))
		}
	})

	// Restore the honest default so later tests in the package see no real source.
	server.changeSource = nil
	server.changeSourceReal = false
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("Expected an RFC3339 timestamp, got %q (%v)", value, err)
	}
	return parsed
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
