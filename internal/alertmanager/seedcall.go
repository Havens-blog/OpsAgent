// Package alertmanager provides Seed Call functionality for automatic evidence collection.
// Seed Call is triggered immediately when an incident is detected to collect initial evidence.
package alertmanager

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SeedCallConfig configures the Seed Call behavior.
type SeedCallConfig struct {
	// Time window before incident start to look for patterns
	LookbackDuration time.Duration
	// Maximum duration of evidence to collect
	CollectionDuration time.Duration
	// Timeout for individual data source queries
	QueryTimeout time.Duration
	// Whether to collect data in parallel
	ParallelCollection bool
}

// DefaultSeedCallConfig returns the default Seed Call configuration.
func DefaultSeedCallConfig() *SeedCallConfig {
	return &SeedCallConfig{
		LookbackDuration:    15 * time.Minute,
		CollectionDuration: 30 * time.Minute,
		QueryTimeout:        30 * time.Second,
		ParallelCollection:  true,
	}
}

// SeedCall performs automatic evidence collection when an incident is detected.
type SeedCall struct {
	config    *SeedCallConfig
	collectors []EvidenceCollector
}

// EvidenceCollector defines the interface for collecting evidence from data sources.
type EvidenceCollector interface {
	// Collect gathers evidence from the data source
	Collect(ctx context.Context, req CollectionRequest) (*CollectionResult, error)
	// Name returns the collector name
	Name() string
	// Type returns the type of evidence collected
	Type() string
}

// CollectionRequest defines the parameters for evidence collection.
type CollectionRequest struct {
	StartTime    time.Time
	EndTime      time.Time
	Services     []string
	IncidentID   string
	Labels       map[string]string
	MaxResults   int
	QueryOptions map[string]interface{}
}

// CollectionResult represents the result of evidence collection.
type CollectionResult struct {
	Collector   string
	Type        string
	Evidence    []Evidence
	StartTime   time.Time
	EndTime     time.Time
	Duration    time.Duration
	Error       error
	Metadata    map[string]interface{}
}

// SeedCallResult represents the complete Seed Call results.
type SeedCallResult struct {
	IncidentID    string
	StartTime     time.Time
	EndTime       time.Time
	Duration      time.Duration
	TotalEvidence  int
	Results       []*CollectionResult
	Errors        []error
}

// NewSeedCall creates a new Seed Call with the given collectors.
func NewSeedCall(config *SeedCallConfig, collectors []EvidenceCollector) *SeedCall {
	if config == nil {
		config = DefaultSeedCallConfig()
	}

	return &SeedCall{
		config:    config,
		collectors: collectors,
	}
}

// Execute performs the Seed Call evidence collection.
func (s *SeedCall) Execute(ctx context.Context, incident Incident) (*SeedCallResult, error) {
	startTime := time.Now()

	// Calculate time window
	evidenceStartTime := incident.StartTime.Add(-s.config.LookbackDuration)
	evidenceEndTime := incident.EndTime
	if evidenceEndTime.IsZero() {
		evidenceEndTime = time.Now()
	}

	// Limit collection duration
	if evidenceEndTime.Sub(evidenceStartTime) > s.config.CollectionDuration {
		evidenceStartTime = evidenceEndTime.Add(-s.config.CollectionDuration)
	}

	result := &SeedCallResult{
		IncidentID: incident.ID,
		StartTime:  startTime,
		Results:    make([]*CollectionResult, 0, len(s.collectors)),
	}

	// Build collection request
	req := CollectionRequest{
		StartTime:  evidenceStartTime,
		EndTime:    evidenceEndTime,
		Services:   incident.AffectedServices,
		IncidentID: incident.ID,
		Labels:     incident.Labels,
		MaxResults: 1000,
	}

	// collectOne runs a single collector and merges its result into the
	// aggregate. It is shared by both parallel and sequential paths so that
	// error accounting stays consistent (no double-counting) and failed
	// collectors remain discoverable via GetFailedCollectors.
	var wg sync.WaitGroup
	var mu sync.Mutex

	collectOne := func(c EvidenceCollector) {
		collectorCtx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
		defer cancel()

		cr, err := c.Collect(collectorCtx, req)

		// Always record a result entry so failed collectors are visible.
		if cr == nil {
			cr = &CollectionResult{}
		}
		// Backfill metadata from the collector interface so downstream
		// grouping (GetEvidenceByType, GetFailedCollectors) works even
		// when a collector forgets to populate these fields.
		if cr.Collector == "" {
			cr.Collector = c.Name()
		}
		if cr.Type == "" {
			cr.Type = c.Type()
		}
		cr.Error = err

		mu.Lock()
		result.Results = append(result.Results, cr)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("%s collector error: %w", c.Name(), err))
		} else {
			result.TotalEvidence += len(cr.Evidence)
		}
		mu.Unlock()
	}

	if s.config.ParallelCollection {
		for _, collector := range s.collectors {
			wg.Add(1)
			go func(c EvidenceCollector) {
				defer wg.Done()
				collectOne(c)
			}(collector)
		}
		wg.Wait()
	} else {
		// Sequential collection
		for _, collector := range s.collectors {
			collectOne(collector)
		}
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	// If the overall context was cancelled (e.g. caller-imposed timeout),
	// surface it to the caller while still returning the partial results
	// collected so far. A single collector failing does NOT reach here
	// because ctx itself is still alive.
	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	return result, nil
}

// Summary returns a summary of the Seed Call results.
func (r *SeedCallResult) Summary() string {
	var successful int
	var failed int

	for _, res := range r.Results {
		if res.Error != nil {
			failed++
		} else {
			successful++
		}
	}

	return fmt.Sprintf("Seed Call completed in %v: %d/%d collectors succeeded, %d evidence items collected",
		r.Duration, successful, len(r.Results), r.TotalEvidence)
}

// GetEvidenceByType returns evidence grouped by type.
func (r *SeedCallResult) GetEvidenceByType() map[string][]Evidence {
	evidenceByType := make(map[string][]Evidence)

	for _, res := range r.Results {
		if res.Error == nil && res != nil {
			evidenceByType[res.Type] = append(evidenceByType[res.Type], res.Evidence...)
		}
	}

	return evidenceByType
}

// GetFailedCollectors returns the names of collectors that failed.
func (r *SeedCallResult) GetFailedCollectors() []string {
	var failed []string

	for _, res := range r.Results {
		if res.Error != nil {
			failed = append(failed, res.Collector)
		}
	}

	return failed
}

// Mock collectors for testing

// MockLogCollector is a mock log collector for testing.
type MockLogCollector struct {
	name         string
	evidenceType string
	delay        time.Duration
	fail         bool
	count        int
}

func (m *MockLogCollector) Collect(ctx context.Context, req CollectionRequest) (*CollectionResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(m.delay):
	}

	if m.fail {
		return nil, fmt.Errorf("mock collector failed")
	}

	// Services may be empty for some incidents; avoid an index panic.
	source := "unknown"
	if len(req.Services) > 0 {
		source = req.Services[0]
	}

	evidence := make([]Evidence, m.count)
	for i := 0; i < m.count; i++ {
		evidence[i] = Evidence{
			Type:        m.evidenceType,
			Source:      source,
			StartTime:   req.StartTime,
			EndTime:     req.EndTime,
			Description: fmt.Sprintf("Mock %s entry %d", m.evidenceType, i),
		}
	}

	return &CollectionResult{
		Collector: m.name,
		Type:      m.evidenceType,
		Evidence:  evidence,
	}, nil
}

func (m *MockLogCollector) Name() string {
	return m.name
}

func (m *MockLogCollector) Type() string {
	return m.evidenceType
}

// NewMockLogCollector creates a new mock collector. evidenceType is optional
// (variadic) and defaults to "logs", preserving backward compatibility with
// existing 4-argument callers.
func NewMockLogCollector(name string, delay time.Duration, fail bool, count int, evidenceType ...string) EvidenceCollector {
	et := "logs"
	if len(evidenceType) > 0 && evidenceType[0] != "" {
		et = evidenceType[0]
	}
	return &MockLogCollector{
		name:         name,
		evidenceType: et,
		delay:        delay,
		fail:         fail,
		count:        count,
	}
}
