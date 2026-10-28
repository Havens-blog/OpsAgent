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
		ID          string            `json:"id"`
		Kind        string            `json:"kind"`
		System      string            `json:"system"`
		Service     string            `json:"service"`
		Message     string            `json:"message"`
		Time        string            `json:"time"`
		Namespace   string            `json:"namespace"`
		Reason      string            `json:"reason"`
		Level       string            `json:"level"`
		Object      string            `json:"object"`
		ObjectKind  string            `json:"object_kind"`
		Cluster     string            `json:"cluster"`
		Occurrences int               `json:"occurrences"`
		FirstSeen   string            `json:"first_seen"`
		LastSeen    string            `json:"last_seen"`
		Details     map[string]string `json:"details"`
	} `json:"changes"`
	Count     int  `json:"count"`
	Total     int  `json:"total"`
	Offset    int  `json:"offset"`
	Limit     int  `json:"limit"`
	HasMore   bool `json:"has_more"`
	Truncated bool `json:"truncated"`
	Stats     *struct {
		Total       int            `json:"total"`
		ByKind      map[string]int `json:"by_kind"`
		Services    int            `json:"services"`
		TopServices []string       `json:"top_services"`
		Systems     []string       `json:"systems"`
		Namespaces  []string       `json:"namespaces"`
		LevelCounts map[string]int `json:"level_counts"`
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

// callChanges issues one /api/v1/changes request and decodes the envelope. It
// also decodes the error envelope so 4xx/5xx responses can be asserted.
func callChanges(t *testing.T, server *APIServer, query string) (int, changesEnvelope) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/changes"+query, nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	var resp changesEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("GET /api/v1/changes%s: unmarshal failed: %v (body: %s)", query, err, w.Body.String())
	}
	return w.Code, resp
}

// changeFixture builds one real models.Change for the change-source tests.
func changeFixture(id string, at time.Time, kind, service, message string, details map[string]string) models.Change {
	return models.Change{
		ID:          id,
		Timestamp:   at,
		Service:     service,
		Type:        kind,
		Description: message,
		Details:     details,
	}
}

// TestChangesEndpoint verifies the changes endpoint serves ONLY the real change
// source: an unconfigured source yields an honest empty list (never mock
// records), a configured source is mapped to DTOs newest-first, and an upstream
// failure surfaces as 502 rather than falling back to fabricated data. It also
// covers the paging / filter / order / dedupe contract of the same endpoint.
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

		code, resp := callChanges(t, server, "")
		if code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", code)
		}

		if resp.Source.Real {
			t.Error("Expected source.real=false when no real change source is configured")
		}
		if resp.Source.Hint == "" {
			t.Error("Expected a hint explaining that no real change source is configured")
		}
		if resp.Count != 0 || resp.Total != 0 {
			t.Errorf("Expected count/total 0, got count=%d total=%d", resp.Count, resp.Total)
		}
		if len(resp.Changes) != 0 {
			t.Errorf("Expected empty changes, got %d entries", len(resp.Changes))
		}
		if resp.Changes == nil {
			t.Error("changes must serialize as [] not null")
		}
		if resp.HasMore {
			t.Error("Expected has_more=false on an empty response")
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
			changeFixture("deploy-old", older, "deployment", "order-service",
				"Deployment updated: registry/order-service:v2.3.1",
				map[string]string{
					"namespace": "prod",
					"reason":    "ScalingReplicaSet",
					"level":     "info",
					"object":    "order-service-7d9f",
					"kind":      "Pod",
					"cluster":   "prod-ecam",
					"image":     "registry/order-service:v2.3.1",
				}),
			changeFixture("config-new", newer, "config", "payment-service",
				"ConfigMap payment-config updated",
				map[string]string{"namespace": "prod", "system": "kubernetes"}),
		}}
		server.changeSource = fake
		server.changeSourceReal = true

		code, resp := callChanges(t, server, "?hours=24")
		if code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", code)
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
		if resp.Total != 2 || resp.Offset != 0 || resp.Limit != 20 {
			t.Errorf("Expected total=2 offset=0 limit=20, got total=%d offset=%d limit=%d",
				resp.Total, resp.Offset, resp.Limit)
		}
		if resp.HasMore || resp.Truncated {
			t.Errorf("Expected has_more/truncated false, got %v/%v", resp.HasMore, resp.Truncated)
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
		if first.Namespace != "prod" {
			t.Errorf("Expected namespace lifted from details, got %q", first.Namespace)
		}
		if first.Occurrences != 1 {
			t.Errorf("Expected occurrences 1 for an un-grouped record, got %d", first.Occurrences)
		}
		if first.FirstSeen != first.Time || first.LastSeen != first.Time {
			t.Errorf("Expected first_seen/last_seen to equal time %s, got %s/%s",
				first.Time, first.FirstSeen, first.LastSeen)
		}

		second := resp.Changes[1]
		if second.Kind != "deployment" {
			t.Errorf("Expected kind deployment, got %q", second.Kind)
		}
		if second.Details["image"] != "registry/order-service:v2.3.1" {
			t.Errorf("Expected details passthrough, got %v", second.Details)
		}
		if second.Namespace != "prod" || second.Reason != "ScalingReplicaSet" ||
			second.Level != "info" || second.Object != "order-service-7d9f" ||
			second.ObjectKind != "Pod" || second.Cluster != "prod-ecam" {
			t.Errorf("Expected details lifted into DTO fields, got %+v", second)
		}

		if resp.Stats == nil {
			t.Fatal("Expected window-wide stats")
		}
		if resp.Stats.Total != 2 || resp.Stats.ByKind["deployment"] != 1 || resp.Stats.ByKind["config"] != 1 {
			t.Errorf("Expected stats over the full window, got %+v", resp.Stats)
		}
		if len(resp.Stats.Systems) != 1 || resp.Stats.Systems[0] != "kubernetes" {
			t.Errorf("Expected systems [kubernetes], got %v", resp.Stats.Systems)
		}
		if len(resp.Stats.Namespaces) != 1 || resp.Stats.Namespaces[0] != "prod" {
			t.Errorf("Expected namespaces [prod], got %v", resp.Stats.Namespaces)
		}
		if resp.Stats.LevelCounts["info"] != 1 || resp.Stats.LevelCounts["unknown"] != 1 {
			t.Errorf("Expected level_counts info=1 unknown=1, got %v", resp.Stats.LevelCounts)
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

		code, _ := callChanges(t, server, "?hours=9999")
		if code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", code)
		}
		if got := fake.gotEnd.Sub(fake.gotStart); got != 168*time.Hour {
			t.Errorf("Expected the window capped at 168h, got %s", got)
		}
	})

	t.Run("hours_below_range_is_clamped_to_one_hour", func(t *testing.T) {
		fake := &fakeChangeSource{}
		server.changeSource = fake
		server.changeSourceReal = true

		for _, query := range []string{"?hours=0", "?hours=-5"} {
			code, resp := callChanges(t, server, query)
			if code != http.StatusOK {
				t.Fatalf("query %q: expected 200 (clamped), got %d", query, code)
			}
			if got := fake.gotEnd.Sub(fake.gotStart); got != time.Hour {
				t.Errorf("query %q: expected the window clamped to 1h, got %s", query, got)
			}
			if resp.Limit != 20 {
				t.Errorf("query %q: expected the default limit 20, got %d", query, resp.Limit)
			}
		}
	})

	t.Run("explicit_start_end_take_precedence", func(t *testing.T) {
		fake := &fakeChangeSource{}
		server.changeSource = fake
		server.changeSourceReal = true

		start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
		url := "?hours=1&start=" + start.Format(time.RFC3339) + "&end=" + end.Format(time.RFC3339)

		code, _ := callChanges(t, server, url)
		if code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", code)
		}
		if !fake.gotStart.Equal(start) || !fake.gotEnd.Equal(end) {
			t.Errorf("Expected explicit window %s..%s, got %s..%s", start, end, fake.gotStart, fake.gotEnd)
		}
	})

	t.Run("invalid_params_are_rejected", func(t *testing.T) {
		server.changeSource = &fakeChangeSource{}
		server.changeSourceReal = true

		for _, query := range []string{
			"?hours=abc",
			"?start=not-a-time",
			"?end=not-a-time",
			"?start=2026-10-27T10:00:00Z&end=2026-13-45T99:00:00Z",
		} {
			code, resp := callChanges(t, server, query)
			if code != http.StatusBadRequest {
				t.Errorf("query %q: expected 400, got %d", query, code)
			}
			if code == http.StatusBadRequest && (resp.Error == nil || resp.Error.Code == "") {
				t.Errorf("query %q: expected an error code in the 400 body", query)
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
			changes = append(changes, changeFixture(
				fmt.Sprintf("c-%d", i),
				now.Add(-time.Duration(i)*time.Minute),
				kind, service, "", nil))
		}
		fake := &fakeChangeSource{changes: changes}
		server.changeSource = fake
		server.changeSourceReal = true

		var resp changesEnvelope
		for round := 0; round < 2; round++ {
			code, got := callChanges(t, server, "?limit=2")
			if code != http.StatusOK {
				t.Fatalf("round %d: expected 200, got %d", round, code)
			}
			resp = got
		}

		if fake.calls != 1 {
			t.Errorf("Expected the second request to be served from cache (1 call), got %d", fake.calls)
		}
		if resp.Count != 2 || len(resp.Changes) != 2 {
			t.Fatalf("Expected payload truncated to 2, got count=%d len=%d", resp.Count, len(resp.Changes))
		}
		if resp.Total != 5 || !resp.Truncated || !resp.HasMore {
			t.Errorf("Expected total=5 truncated=has_more=true, got total=%d truncated=%v has_more=%v",
				resp.Total, resp.Truncated, resp.HasMore)
		}
		if resp.Limit != 2 || resp.Offset != 0 {
			t.Errorf("Expected limit=2 offset=0, got limit=%d offset=%d", resp.Limit, resp.Offset)
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

		for _, query := range []string{
			"?limit=abc",
			"?limit=2.5",
			"?offset=abc",
			"?offset=-1",
			"?order=sideways",
			"?dedupe=maybe",
		} {
			code, _ := callChanges(t, server, query)
			if code != http.StatusBadRequest {
				t.Errorf("query %q: expected 400, got %d", query, code)
			}
		}
	})

	t.Run("limit_is_clamped_into_range", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		fake := &fakeChangeSource{changes: []models.Change{
			changeFixture("c-0", now, "k8s_event", "svc-a", "first", nil),
			changeFixture("c-1", now.Add(-time.Minute), "k8s_event", "svc-b", "second", nil),
		}}
		server.changeSource = fake
		server.changeSourceReal = true

		code, resp := callChanges(t, server, "?limit=0")
		if code != http.StatusOK {
			t.Fatalf("limit=0: expected 200 (clamped), got %d", code)
		}
		if resp.Limit != 1 || resp.Count != 1 || resp.Total != 2 || !resp.HasMore {
			t.Errorf("limit=0: expected limit=1 count=1 total=2 has_more=true, got limit=%d count=%d total=%d has_more=%v",
				resp.Limit, resp.Count, resp.Total, resp.HasMore)
		}

		code, resp = callChanges(t, server, "?limit=-3")
		if code != http.StatusOK || resp.Limit != 1 {
			t.Errorf("limit=-3: expected 200 with limit=1, got %d limit=%d", code, resp.Limit)
		}

		code, resp = callChanges(t, server, "?limit=999")
		if code != http.StatusOK || resp.Limit != 200 || resp.Count != 2 {
			t.Errorf("limit=999: expected 200 with limit=200 count=2, got %d limit=%d count=%d",
				code, resp.Limit, resp.Count)
		}
	})

	t.Run("offset_and_limit_page_the_filtered_list", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		changes := make([]models.Change, 0, 5)
		for i := 0; i < 5; i++ {
			changes = append(changes, changeFixture(
				fmt.Sprintf("c-%d", i),
				now.Add(-time.Duration(i)*time.Minute),
				"k8s_event", "svc-a", "", nil))
		}
		fake := &fakeChangeSource{changes: changes}
		server.changeSource = fake
		server.changeSourceReal = true

		code, page2 := callChanges(t, server, "?limit=2&offset=2")
		if code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", code)
		}
		if page2.Count != 2 || len(page2.Changes) != 2 || page2.Total != 5 {
			t.Fatalf("Expected count=2 total=5, got count=%d len=%d total=%d",
				page2.Count, len(page2.Changes), page2.Total)
		}
		if page2.Offset != 2 || page2.Limit != 2 || !page2.HasMore {
			t.Errorf("Expected offset=2 limit=2 has_more=true, got offset=%d limit=%d has_more=%v",
				page2.Offset, page2.Limit, page2.HasMore)
		}
		if page2.Changes[0].ID != "c-2" || page2.Changes[1].ID != "c-3" {
			t.Errorf("Expected page 2 = [c-2 c-3], got [%s %s]",
				page2.Changes[0].ID, page2.Changes[1].ID)
		}

		code, lastPage := callChanges(t, server, "?limit=2&offset=4")
		if code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", code)
		}
		if lastPage.Count != 1 || lastPage.Changes[0].ID != "c-4" || lastPage.HasMore {
			t.Errorf("Expected last page count=1 id=c-4 has_more=false, got count=%d id=%s has_more=%v",
				lastPage.Count, lastPage.Changes[0].ID, lastPage.HasMore)
		}

		// An offset past the end is an empty page, not an error.
		code, past := callChanges(t, server, "?limit=2&offset=50")
		if code != http.StatusOK {
			t.Fatalf("Expected 200 for an offset past the end, got %d", code)
		}
		if past.Count != 0 || len(past.Changes) != 0 || past.Total != 5 || past.HasMore {
			t.Errorf("Expected an empty page with total=5 has_more=false, got count=%d total=%d has_more=%v",
				past.Count, past.Total, past.HasMore)
		}

		if fake.calls != 1 {
			t.Errorf("Expected every page to reuse the cached window list (1 call), got %d", fake.calls)
		}
	})

	t.Run("kind_namespace_and_query_filters", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		fake := &fakeChangeSource{changes: []models.Change{
			changeFixture("pod-order", now.Add(-5*time.Minute), "k8s_event", "order-service",
				"Back-off restarting failed container",
				map[string]string{
					"namespace": "prod", "reason": "BackOff", "level": "warning",
					"object": "order-abc", "kind": "Pod",
				}),
			changeFixture("deploy-payment", now.Add(-15*time.Minute), "deployment", "payment-service",
				"Deployment updated: registry/payment:v1.2.3",
				map[string]string{
					"namespace": "staging", "reason": "ScalingReplicaSet", "level": "info",
					"object": "payment-deploy", "kind": "Deployment",
				}),
			changeFixture("pod-cart", now.Add(-25*time.Minute), "k8s_event", "cart-service",
				"Created container cart",
				map[string]string{
					"namespace": "prod", "reason": "SuccessfulCreate", "level": "info",
					"object": "cart-xyz", "kind": "Pod",
				}),
		}}
		server.changeSource = fake
		server.changeSourceReal = true

		type want struct {
			query string
			total int
			ids   []string
		}
		cases := []want{
			{query: "?kind=k8s_event", total: 2, ids: []string{"pod-order", "pod-cart"}},
			{query: "?namespace=staging", total: 1, ids: []string{"deploy-payment"}},
			{query: "?system=kubernetes", total: 3, ids: []string{"pod-order", "deploy-payment", "pod-cart"}},
			{query: "?q=PAYMENT", total: 1, ids: []string{"deploy-payment"}},
			{query: "?q=backoff", total: 1, ids: []string{"pod-order"}},
			{query: "?q=cart-xyz", total: 1, ids: []string{"pod-cart"}},
			{query: "?q=container&kind=k8s_event&namespace=prod", total: 2, ids: []string{"pod-order", "pod-cart"}},
			{query: "?kind=deployment&namespace=prod", total: 0, ids: nil},
		}
		for _, tc := range cases {
			code, resp := callChanges(t, server, tc.query)
			if code != http.StatusOK {
				t.Fatalf("query %q: expected 200, got %d", tc.query, code)
			}
			if resp.Total != tc.total || resp.Count != len(tc.ids) {
				t.Errorf("query %q: expected total=%d count=%d, got total=%d count=%d",
					tc.query, tc.total, len(tc.ids), resp.Total, resp.Count)
			}
			for i, id := range tc.ids {
				if i < len(resp.Changes) && resp.Changes[i].ID != id {
					t.Errorf("query %q: expected id %s at %d, got %s", tc.query, id, i, resp.Changes[i].ID)
				}
			}
			// Stats stay window-wide and are NEVER narrowed by the filters.
			if resp.Stats == nil || resp.Stats.Total != 3 {
				t.Errorf("query %q: expected unfiltered stats total 3, got %+v", tc.query, resp.Stats)
			}
		}

		if fake.calls != 1 {
			t.Errorf("Expected every filtered request to reuse the cached list (1 call), got %d", fake.calls)
		}
	})

	t.Run("order_asc_returns_oldest_first", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		fake := &fakeChangeSource{changes: []models.Change{
			changeFixture("middle", now.Add(-2*time.Hour), "k8s_event", "svc-a", "", nil),
			changeFixture("oldest", now.Add(-6*time.Hour), "k8s_event", "svc-a", "", nil),
			changeFixture("newest", now.Add(-10*time.Minute), "k8s_event", "svc-a", "", nil),
		}}
		server.changeSource = fake
		server.changeSourceReal = true

		code, desc := callChanges(t, server, "?order=desc")
		if code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", code)
		}
		if desc.Changes[0].ID != "newest" || desc.Changes[2].ID != "oldest" {
			t.Errorf("Expected desc [newest middle oldest], got [%s %s %s]",
				desc.Changes[0].ID, desc.Changes[1].ID, desc.Changes[2].ID)
		}

		code, asc := callChanges(t, server, "?order=asc")
		if code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", code)
		}
		if asc.Changes[0].ID != "oldest" || asc.Changes[2].ID != "newest" {
			t.Errorf("Expected asc [oldest middle newest], got [%s %s %s]",
				asc.Changes[0].ID, asc.Changes[1].ID, asc.Changes[2].ID)
		}
	})

	t.Run("dedupe_merges_duplicate_events", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		oldest := now.Add(-3 * time.Hour)
		middle := now.Add(-2 * time.Hour)
		newest := now.Add(-1 * time.Hour)
		fake := &fakeChangeSource{changes: []models.Change{
			changeFixture("dup-old", oldest, "k8s_event", "order-service",
				"Back-off restarting failed container",
				map[string]string{"namespace": "prod", "reason": "BackOff", "object": "order-abc", "kind": "Pod"}),
			changeFixture("dup-mid", middle, "k8s_event", "order-service",
				"Back-off restarting failed container (again)",
				map[string]string{"namespace": "prod", "reason": "BackOff", "object": "order-abc", "kind": "Pod"}),
			changeFixture("dup-new", newest, "k8s_event", "order-service",
				"Back-off restarting failed container (latest)",
				map[string]string{"namespace": "prod", "reason": "BackOff", "object": "order-abc", "kind": "Pod"}),
			changeFixture("other", now.Add(-30*time.Minute), "k8s_event", "cart-service",
				"Created container cart",
				map[string]string{"namespace": "prod", "reason": "BackOff", "object": "cart-xyz", "kind": "Pod"}),
		}}
		server.changeSource = fake
		server.changeSourceReal = true

		code, raw := callChanges(t, server, "")
		if code != http.StatusOK || raw.Total != 4 {
			t.Fatalf("Expected 4 un-grouped records, got code=%d total=%d", code, raw.Total)
		}

		code, resp := callChanges(t, server, "?dedupe=true")
		if code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", code)
		}
		if resp.Total != 2 || resp.Count != 2 {
			t.Fatalf("Expected the 4 records grouped into 2 (total=2 count=2), got total=%d count=%d",
				resp.Total, resp.Count)
		}
		// Newest first: the newer cart event, then the collapsed order group.
		if resp.Changes[0].ID != "other" || resp.Changes[1].ID != "dup-new" {
			t.Errorf("Expected grouped order [other dup-new], got [%s %s]",
				resp.Changes[0].ID, resp.Changes[1].ID)
		}
		group := resp.Changes[1]
		if group.Occurrences != 3 {
			t.Errorf("Expected occurrences 3 for the collapsed group, got %d", group.Occurrences)
		}
		if !newest.Equal(mustParseTime(t, group.Time)) {
			t.Errorf("Expected the retained record to be the newest of the group (%s), got %s",
				newest.Format(time.RFC3339), group.Time)
		}
		if group.Message != "Back-off restarting failed container (latest)" {
			t.Errorf("Expected the newest message to be retained, got %q", group.Message)
		}
		if !oldest.Equal(mustParseTime(t, group.FirstSeen)) {
			t.Errorf("Expected first_seen %s, got %s", oldest.Format(time.RFC3339), group.FirstSeen)
		}
		if !newest.Equal(mustParseTime(t, group.LastSeen)) {
			t.Errorf("Expected last_seen %s, got %s", newest.Format(time.RFC3339), group.LastSeen)
		}
		// Stats stay over the UN-grouped window list.
		if resp.Stats == nil || resp.Stats.Total != 4 {
			t.Errorf("Expected stats over the full ungrouped window (4), got %+v", resp.Stats)
		}

		// Dedupe composes with the kind/namespace/text filters.
		code, filtered := callChanges(t, server, "?dedupe=true&q=order-abc")
		if code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", code)
		}
		if filtered.Total != 1 || filtered.Count != 1 || filtered.Changes[0].ID != "dup-new" {
			t.Errorf("Expected the filtered group to collapse to dup-new, got total=%d count=%d first=%s",
				filtered.Total, filtered.Count, filtered.Changes[0].ID)
		}
		if filtered.Changes[0].Occurrences != 3 {
			t.Errorf("Expected occurrences 3 after filtering the same group, got %d",
				filtered.Changes[0].Occurrences)
		}
		if fake.calls != 1 {
			t.Errorf("Expected dedupe/filter variations to reuse the cached desc list (1 call), got %d", fake.calls)
		}

		// Flipping the order re-queries the source once (the cached list is
		// stored already ordered) and keeps the group's true extremes.
		code, asc := callChanges(t, server, "?dedupe=true&order=asc")
		if code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", code)
		}
		if asc.Changes[0].ID != "dup-new" || asc.Changes[1].ID != "other" {
			t.Errorf("Expected asc grouped order [dup-new other], got [%s %s]",
				asc.Changes[0].ID, asc.Changes[1].ID)
		}
		if !oldest.Equal(mustParseTime(t, asc.Changes[0].FirstSeen)) {
			t.Errorf("Expected first_seen %s in asc order, got %s",
				oldest.Format(time.RFC3339), asc.Changes[0].FirstSeen)
		}
		if fake.calls != 2 {
			t.Errorf("Expected one source query per order (2), got %d calls", fake.calls)
		}
	})

	t.Run("source_error_returns_502_without_fallback", func(t *testing.T) {
		fake := &fakeChangeSource{err: errors.New("K8s deployments request failed: connection refused")}
		server.changeSource = fake
		server.changeSourceReal = true

		code, resp := callChanges(t, server, "")
		if code != http.StatusBadGateway {
			t.Fatalf("Expected 502, got %d", code)
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
		if fake.calls != 1 {
			t.Errorf("Expected exactly 1 failing source call, got %d", fake.calls)
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
