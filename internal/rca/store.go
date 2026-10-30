// Package rca provides root cause analysis storage.
package rca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/tenant"
)

// RCAReportStore defines the interface for storing and retrieving RCA reports.
//
// Every method is tenant-scoped: the tenant id is read from ctx (see
// internal/tenant) and reports are isolated per tenant. A report created
// under tenant A is invisible to tenant B — cross-tenant Get/UpdateStatus
// return a not-found error (surfaced as 404) rather than leaking existence.
type RCAReportStore interface {
	// Store saves an RCA report under the ctx tenant.
	Store(ctx context.Context, report *models.RCAReport) error

	// Get retrieves an RCA report by incident ID within the ctx tenant.
	Get(ctx context.Context, incidentID string) (*models.RCAReport, error)

	// List lists RCA reports belonging to the ctx tenant.
	List(ctx context.Context) ([]*models.RCAReport, error)

	// UpdateStatus updates the status of an RCA report within the ctx tenant.
	UpdateStatus(ctx context.Context, incidentID string, status string) error
}

// MemoryRCAReportStore provides an in-memory, tenant-scoped implementation of
// RCAReportStore. Reports are bucketed by tenant id; tenant 0 is the default
// bucket used when EIAM enforcement is disabled.
type MemoryRCAReportStore struct {
	mu   sync.RWMutex
	path string
	// byTenant maps tenant id -> (incident id -> report).
	byTenant map[int64]map[string]*models.RCAReport
}

// NewMemoryRCAReportStore creates a new in-memory RCA report store.
func NewMemoryRCAReportStore() *MemoryRCAReportStore {
	return &MemoryRCAReportStore{
		byTenant: make(map[int64]map[string]*models.RCAReport),
	}
}

// persistedRCAState is the on-disk snapshot shape. Tenant ids are integer map
// keys, which encoding/json serializes as their decimal string forms.
type persistedRCAState struct {
	ByTenant map[int64]map[string]*models.RCAReport `json:"by_tenant"`
}

// NewFileBackedRCAReportStore loads the snapshot at path, or starts empty when
// it does not exist. A corrupt or unreadable file returns an error.
func NewFileBackedRCAReportStore(path string) (*MemoryRCAReportStore, error) {
	if path == "" {
		return nil, fmt.Errorf("RCA report store path is required")
	}

	s := &MemoryRCAReportStore{
		path:     path,
		byTenant: make(map[int64]map[string]*models.RCAReport),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads and decodes the snapshot. A missing file is the empty store.
// Reports are restored verbatim so running/failed states survive a restart.
func (s *MemoryRCAReportStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read RCA report store %s: %w", s.path, err)
	}

	var state persistedRCAState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse RCA report store %s: %w", s.path, err)
	}
	if state.ByTenant == nil {
		state.ByTenant = make(map[int64]map[string]*models.RCAReport)
	}
	s.byTenant = state.ByTenant
	return nil
}

// persistLocked writes the current state to a temp file in the same directory
// and atomically renames it over the target. Callers must hold the write lock.
func (s *MemoryRCAReportStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create RCA report store dir: %w", err)
	}

	data, err := json.MarshalIndent(persistedRCAState{ByTenant: s.byTenant}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode RCA report store: %w", err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create RCA report store temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write RCA report store temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close RCA report store temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace RCA report store: %w", err)
	}
	return nil
}

// bucket returns the report map for the ctx tenant, creating it if needed.
// Caller must hold s.mu for writing.
func (s *MemoryRCAReportStore) bucket(ctx context.Context) map[string]*models.RCAReport {
	tid := tenant.IDFrom(ctx)
	b := s.byTenant[tid]
	if b == nil {
		b = make(map[string]*models.RCAReport)
		s.byTenant[tid] = b
	}
	return b
}

// Store saves an RCA report under the ctx tenant.
func (s *MemoryRCAReportStore) Store(ctx context.Context, report *models.RCAReport) error {
	if report.Incident == nil {
		return fmt.Errorf("report incident is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.bucket(ctx)[report.Incident.ID] = report
	return s.persistLocked()
}

// Get retrieves an RCA report by incident ID within the ctx tenant. A report
// owned by another tenant is reported as not found (no existence leak).
func (s *MemoryRCAReportStore) Get(ctx context.Context, incidentID string) (*models.RCAReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tid := tenant.IDFrom(ctx)
	report, exists := s.byTenant[tid][incidentID]
	if !exists {
		return nil, fmt.Errorf("report not found: %s", incidentID)
	}

	return report, nil
}

// List lists RCA reports belonging to the ctx tenant only.
func (s *MemoryRCAReportStore) List(ctx context.Context) ([]*models.RCAReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tid := tenant.IDFrom(ctx)
	b := s.byTenant[tid]
	reports := make([]*models.RCAReport, 0, len(b))
	for _, report := range b {
		reports = append(reports, report)
	}

	return reports, nil
}

// UpdateStatus updates the status of an RCA report within the ctx tenant.
func (s *MemoryRCAReportStore) UpdateStatus(ctx context.Context, incidentID string, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tid := tenant.IDFrom(ctx)
	report, exists := s.byTenant[tid][incidentID]
	if !exists {
		return fmt.Errorf("report not found: %s", incidentID)
	}

	// Update the incident status field if we have labels support
	if report.Incident.Labels == nil {
		report.Incident.Labels = make(map[string]string)
	}
	report.Incident.Labels["status"] = status

	return s.persistLocked()
}
