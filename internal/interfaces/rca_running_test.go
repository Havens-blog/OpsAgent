package interfaces

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca"
	"github.com/Havens-blog/OpsAgent/internal/rca/engine"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

// blockingLogSource blocks QueryErrors until release is closed. It keeps the
// engine's signal-gathering step parked so an analysis stays in running state.
type blockingLogSource struct {
	release chan struct{}
}

func (b *blockingLogSource) QueryErrors(ctx context.Context, _, _ time.Time) ([]models.LogEntry, error) {
	select {
	case <-b.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *blockingLogSource) QueryErrorsByService(ctx context.Context, _ string, _, _ time.Time) ([]models.LogEntry, error) {
	select {
	case <-b.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *blockingLogSource) QueryLogsByService(_ context.Context, _ string, _, _ time.Time) ([]models.LogEntry, error) {
	return nil, nil
}

func (b *blockingLogSource) GetLogCount(_ context.Context, _ string, _, _ time.Time) (int64, error) {
	return 0, nil
}

// failOnSecondStore lets the initial running report persist, then fails the
// full-report overwrite so the handler's store-failure branch is exercised.
type failOnSecondStore struct {
	*rca.MemoryRCAReportStore
	calls int
}

func (f *failOnSecondStore) Store(ctx context.Context, report *models.RCAReport) error {
	f.calls++
	if f.calls > 1 {
		return errors.New("forced store failure")
	}
	return f.MemoryRCAReportStore.Store(ctx, report)
}

type rcaReportStatusDTO struct {
	IncidentID string   `json:"incident_id"`
	Status     string   `json:"status"`
	Warnings   []string `json:"warnings"`
}

type rcaListResponseDTO struct {
	Reports []rcaReportStatusDTO `json:"reports"`
	Count   int                  `json:"count"`
}

func postRCAAnalysis(t *testing.T, server *APIServer, incidentID string) {
	t.Helper()
	request := RCAAnalysisRequest{
		IncidentID: incidentID,
		StartTime:  time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		EndTime:    time.Now().Format(time.RFC3339),
		Severity:   "P1",
		AffectedServices: []string{
			"order-service",
		},
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rca/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("POST /api/v1/rca/analyze status = %d, want %d (body: %s)", w.Code, http.StatusAccepted, w.Body.String())
	}
}

func getRCAReport(t *testing.T, server *APIServer, incidentID string) (int, rcaReportStatusDTO) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rca/"+incidentID, nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	var got rcaReportStatusDTO
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("GET /api/v1/rca/%s: unmarshal failed: %v (body: %s)", incidentID, err, w.Body.String())
	}
	return w.Code, got
}

func listRCAReports(t *testing.T, server *APIServer) (int, rcaListResponseDTO) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rca", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	var got rcaListResponseDTO
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("GET /api/v1/rca: unmarshal failed: %v (body: %s)", err, w.Body.String())
	}
	return w.Code, got
}

// TestRCARunningReportVisibleInGetAndList proves the running report is
// persisted synchronously before the background analysis completes.
func TestRCARunningReportVisibleInGetAndList(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}

	release := make(chan struct{})
	blocking := &blockingLogSource{release: release}
	eng, err := engine.NewEngine(
		engine.WithLogSource(blocking),
		engine.WithMetricSource(sources.NewEmptyMetricSource()),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	server.rcaEngine = eng

	const incidentID = "INC-RUNNING-1"
	postRCAAnalysis(t, server, incidentID)

	code, got := getRCAReport(t, server, incidentID)
	if code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d (body: %s)", code, http.StatusOK, "")
	}
	if got.Status != "running" {
		t.Fatalf("GET status = %q, want %q", got.Status, "running")
	}

	listCode, list := listRCAReports(t, server)
	if listCode != http.StatusOK {
		t.Fatalf("LIST status = %d, want %d", listCode, http.StatusOK)
	}
	found := false
	for _, report := range list.Reports {
		if report.IncidentID == incidentID {
			found = true
			if report.Status != "running" {
				t.Fatalf("LIST report status = %q, want %q", report.Status, "running")
			}
		}
	}
	if !found {
		t.Fatalf("LIST did not contain %s", incidentID)
	}

	// Unblock the source and wait for the background analysis to finish.
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		code, got = getRCAReport(t, server, incidentID)
		if code == http.StatusOK && got.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code != http.StatusOK || got.Status != "completed" {
		t.Fatalf("after release, GET status = %d / %q, want %d / %q", code, got.Status, http.StatusOK, "completed")
	}
}

// TestRCAAnalysisResponseIncludesWarnings proves both the report detail and
// list responses expose the warnings field as an empty JSON array for a
// successful analysis. Non-empty warnings are covered at the engine layer.
func TestRCAAnalysisResponseIncludesWarnings(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}

	const incidentID = "INC-WARNINGS-1"
	postRCAAnalysis(t, server, incidentID)

	deadline := time.Now().Add(5 * time.Second)
	var code int
	var got rcaReportStatusDTO
	for time.Now().Before(deadline) {
		code, got = getRCAReport(t, server, incidentID)
		if code == http.StatusOK && got.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code != http.StatusOK || got.Status != "completed" {
		t.Fatalf("GET status = %d / %q, want %d / %q", code, got.Status, http.StatusOK, "completed")
	}
	if got.Warnings == nil {
		t.Fatalf("GET warnings field missing; got nil, want empty array")
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("GET warnings = %v, want empty array", got.Warnings)
	}

	listCode, list := listRCAReports(t, server)
	if listCode != http.StatusOK {
		t.Fatalf("LIST status = %d, want %d", listCode, http.StatusOK)
	}
	found := false
	for _, report := range list.Reports {
		if report.IncidentID == incidentID {
			found = true
			if report.Warnings == nil {
				t.Fatalf("LIST warnings field missing; got nil, want empty array")
			}
			if len(report.Warnings) != 0 {
				t.Fatalf("LIST warnings = %v, want empty array", report.Warnings)
			}
		}
	}
	if !found {
		t.Fatalf("LIST did not contain %s", incidentID)
	}
}

// TestRCAStoreFailureFallsBackToFailed proves a store failure after the
// running report is persisted flips the record to failed instead of leaving it
// stuck in running.
func TestRCAStoreFailureFallsBackToFailed(t *testing.T) {
	server, err := NewAPIServer(testSettings())
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}

	failStore := &failOnSecondStore{MemoryRCAReportStore: rca.NewMemoryRCAReportStore()}
	server.rcaStore = failStore

	const incidentID = "INC-STORE-FAIL-1"
	postRCAAnalysis(t, server, incidentID)

	deadline := time.Now().Add(5 * time.Second)
	var code int
	var got rcaReportStatusDTO
	for time.Now().Before(deadline) {
		code, got = getRCAReport(t, server, incidentID)
		if code == http.StatusOK && got.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", code, http.StatusOK)
	}
	if got.Status != "failed" {
		t.Fatalf("GET status = %q, want %q", got.Status, "failed")
	}
}
