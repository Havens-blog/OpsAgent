// Package rca provides root cause analysis storage.
package rca

import (
	"context"
	"fmt"
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
	mu sync.RWMutex
	// byTenant maps tenant id -> (incident id -> report).
	byTenant map[int64]map[string]*models.RCAReport
}

// NewMemoryRCAReportStore creates a new in-memory RCA report store.
func NewMemoryRCAReportStore() *MemoryRCAReportStore {
	return &MemoryRCAReportStore{
		byTenant: make(map[int64]map[string]*models.RCAReport),
	}
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
	return nil
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

	return nil
}
