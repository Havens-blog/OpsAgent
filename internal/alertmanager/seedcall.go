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

	// Collect evidence
	var wg sync.WaitGroup
	var mu sync.Mutex

	if s.config.ParallelCollection {
		// Parallel collection
		errChan := make(chan error, len(s.collectors))

		for _, collector := range s.collectors {
			wg.Add(1)
			go func(c EvidenceCollector) {
				defer wg.Done()

				collectorCtx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
				defer cancel()

				collectionResult, err := c.Collect(collectorCtx, req)

				mu.Lock()
				result.Results = append(result.Results, collectionResult)
				if err != nil {
					result.Errors = append(result.Errors, err)
				} else if collectionResult != nil {
					result.TotalEvidence += len(collectionResult.Evidence)
				}
				mu.Unlock()

				if err != nil {
					errChan <- fmt.Errorf("%s collector error: %w", c.Name(), err)
				}
			}(collector)
		}

		wg.Wait()
		close(errChan)

		// Collect errors
		for err := range errChan {
			result.Errors = append(result.Errors, err)
		}
	} else {
		// Sequential collection
		for _, collector := range s.collectors {
			collectorCtx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
			defer cancel()

			collectionResult, err := collector.Collect(collectorCtx, req)

			result.Results = append(result.Results, collectionResult)
			if err != nil {
				result.Errors = append(result.Errors, err)
			} else if collectionResult != nil {
				result.TotalEvidence += len(collectionResult.Evidence)
			}
		}
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

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
	name  string
	delay time.Duration
	fail  bool
	count int
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

	evidence := make([]Evidence, m.count)
	for i := 0; i < m.count; i++ {
		evidence[i] = Evidence{
			Type:        "logs",
			Source:      req.Services[0],
			StartTime:   req.StartTime,
			EndTime:     req.EndTime,
			Description: fmt.Sprintf("Mock log entry %d", i),
		}
	}

	return &CollectionResult{
		Evidence: evidence,
	}, nil
}

func (m *MockLogCollector) Name() string {
	return m.name
}

func (m *MockLogCollector) Type() string {
	return "logs"
}

// NewMockLogCollector creates a new mock log collector.
func NewMockLogCollector(name string, delay time.Duration, fail bool, count int) EvidenceCollector {
	return &MockLogCollector{
		name: name,
		delay: delay,
		fail: fail,
		count: count,
	}
}
