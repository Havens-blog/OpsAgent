// Package synthetic provides synthetic RCA test scenarios for validation.
package synthetic

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/engine"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

// Scenario defines a synthetic RCA test scenario.
type Scenario struct {
	Name        string
	Description string
	Incident    *models.Incident
	Expected    *ExpectedResult
	DataSources ScenarioDataSources
}

// ExpectedResult defines the expected RCA analysis result.
type ExpectedResult struct {
	TopCandidateService string
	MinConfidence       float64
	ExpectedSignals     []string
	ExpectedFindings    []string
}

// ScenarioDataSources provides controlled data for testing.
type ScenarioDataSources struct {
	Logs       func(ctx context.Context, start, end time.Time) ([]models.LogEntry, error)
	Metrics    func(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error)
	Traces     func(ctx context.Context, start, end time.Time) ([]models.TraceSummary, error)
	Topology   func(ctx context.Context) (*models.Topology, error)
	Changes    func(ctx context.Context, start, end time.Time) ([]models.Change, error)
}

// Scenarios returns all available test scenarios.
func Scenarios() []Scenario {
	return []Scenario{
		DatabaseConnectionPoolExhaustion(),
		HighErrorRateAfterDeployment(),
		UpstreamDependencyFailure(),
		MemoryLeakProgressiveDegradation(),
	}
}

// DatabaseConnectionPoolExhaustion creates a scenario for DB pool exhaustion.
func DatabaseConnectionPoolExhaustion() Scenario {
	incidentTime := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	startTime := incidentTime.Add(-15 * time.Minute)
	endTime := incidentTime.Add(15 * time.Minute)

	return Scenario{
		Name:        "database_connection_pool_exhaustion",
		Description: "Database connection pool exhaustion causing service degradation",
		Incident: &models.Incident{
			ID:              "INC-DB-POOL-001",
			StartTime:       startTime,
			EndTime:         endTime,
			Severity:        "P1",
			AffectedServices: []string{"order-service", "api-gateway"},
			Description:     "High latency and timeouts in order processing",
		},
		Expected: &ExpectedResult{
			TopCandidateService: "order-service",
			MinConfidence:       0.6,
			ExpectedSignals:     []string{"error_spike", "metric_anomaly"},
			ExpectedFindings:    []string{"connection_pool", "timeout", "database"},
		},
		DataSources: ScenarioDataSources{
			Logs: func(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
				return []models.LogEntry{
					{
						Timestamp: incidentTime.Add(-5 * time.Minute),
						Service:   "order-service",
						Level:     "warning",
						Message:   "Connection pool usage at 80%",
					},
					{
						Timestamp: incidentTime.Add(-2 * time.Minute),
						Service:   "order-service",
						Level:     "error",
						Message:   "Timeout waiting for database connection",
					},
					{
						Timestamp: incidentTime,
						Service:   "order-service",
						Level:     "error",
						Message:   "Connection pool exhausted",
					},
					{
						Timestamp: incidentTime.Add(5 * time.Minute),
						Service:   "api-gateway",
						Level:     "error",
						Message:   "Upstream request timeout",
					},
				}, nil
			},
			Metrics: func(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
				return []models.MetricSeries{
					{
						Name: "db_connection_pool_usage",
						Labels: map[string]string{"service": "order-service"},
						Unit:  "percent",
						DataPoints: []models.DataPoint{
							{Timestamp: incidentTime.Add(-10 * time.Minute), Value: 50},
							{Timestamp: incidentTime.Add(-5 * time.Minute), Value: 80},
							{Timestamp: incidentTime, Value: 100},
							{Timestamp: incidentTime.Add(5 * time.Minute), Value: 100},
						},
					},
					{
						Name: "http_request_latency_seconds",
						Labels: map[string]string{"service": "order-service"},
						Unit:  "seconds",
						DataPoints: []models.DataPoint{
							{Timestamp: incidentTime.Add(-10 * time.Minute), Value: 0.1},
							{Timestamp: incidentTime.Add(-5 * time.Minute), Value: 0.5},
							{Timestamp: incidentTime, Value: 5.0},
							{Timestamp: incidentTime.Add(5 * time.Minute), Value: 10.0},
						},
					},
				}, nil
			},
			Topology: func(ctx context.Context) (*models.Topology, error) {
				return &models.Topology{
					Nodes: []models.ServiceNode{
						{Name: "api-gateway", Type: "application"},
						{Name: "order-service", Type: "application"},
						{Name: "payment-service", Type: "application"},
						{Name: "mysql-primary", Type: "database"},
					},
					Edges: []models.DependencyEdge{
						{From: "api-gateway", To: "order-service", Type: "http"},
						{From: "order-service", To: "mysql-primary", Type: "tcp"},
						{From: "order-service", To: "payment-service", Type: "http"},
					},
				}, nil
			},
			Changes: func(ctx context.Context, start, end time.Time) ([]models.Change, error) {
				return []models.Change{
					{
						ID:          "config-change-001",
						Timestamp:   incidentTime.Add(-30 * time.Minute),
						Service:     "order-service",
						Type:        "configuration",
						Description: "Max connection pool size reduced from 100 to 50",
					},
				}, nil
			},
		},
	}
}

// HighErrorRateAfterDeployment creates a scenario for post-deployment errors.
func HighErrorRateAfterDeployment() Scenario {
	deployTime := time.Date(2024, 6, 15, 14, 0, 0, 0, time.UTC)
	startTime := deployTime.Add(-15 * time.Minute)
	endTime := deployTime.Add(30 * time.Minute)

	return Scenario{
		Name:        "high_error_rate_after_deployment",
		Description: "Error rate spike immediately after deployment",
		Incident: &models.Incident{
			ID:              "INC-DEPLOY-001",
			StartTime:       startTime,
			EndTime:         endTime,
			Severity:        "P0",
			AffectedServices: []string{"user-service"},
			Description:     "500 errors increased from 0.1% to 15% after deployment",
		},
		Expected: &ExpectedResult{
			TopCandidateService: "user-service",
			MinConfidence:       0.7,
			ExpectedSignals:     []string{"error_spike", "change_proximity"},
			ExpectedFindings:    []string{"deployment", "new_version", "regression"},
		},
		DataSources: ScenarioDataSources{
			Logs: func(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
				return []models.LogEntry{
					{
						Timestamp: deployTime.Add(-10 * time.Minute),
						Service:   "user-service",
						Level:     "info",
						Message:   "Service healthy, version v1.2.3",
					},
					{
						Timestamp: deployTime.Add(2 * time.Minute),
						Service:   "user-service",
						Level:     "error",
						Message:   "NullPointerException in UserProfileService",
					},
					{
						Timestamp: deployTime.Add(5 * time.Minute),
						Service:   "user-service",
						Level:     "error",
						Message:   "NullPointerException in UserProfileService",
					},
				}, nil
			},
			Metrics: func(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
				return []models.MetricSeries{
					{
						Name: "http_requests_total",
						Labels: map[string]string{"service": "user-service", "status": "5xx"},
						Unit:  "requests",
						DataPoints: []models.DataPoint{
							{Timestamp: deployTime.Add(-10 * time.Minute), Value: 5},
							{Timestamp: deployTime.Add(2 * time.Minute), Value: 150},
							{Timestamp: deployTime.Add(5 * time.Minute), Value: 200},
						},
					},
				}, nil
			},
			Changes: func(ctx context.Context, start, end time.Time) ([]models.Change, error) {
				return []models.Change{
					{
						ID:          "deploy-20240615-001",
						Timestamp:   deployTime,
						Service:     "user-service",
						Type:        "deployment",
						Description: "Deployed version v1.2.4 (image: user-service:v1.2.4)",
					},
				}, nil
			},
			Topology: func(ctx context.Context) (*models.Topology, error) {
				return &models.Topology{
					Nodes: []models.ServiceNode{
						{Name: "user-service", Type: "application"},
						{Name: "database", Type: "database"},
					},
					Edges: []models.DependencyEdge{
						{From: "user-service", To: "database", Type: "tcp"},
					},
				}, nil
			},
		},
	}
}

// UpstreamDependencyFailure creates a scenario for upstream service failure.
func UpstreamDependencyFailure() Scenario {
	failTime := time.Date(2024, 6, 15, 16, 45, 0, 0, time.UTC)
	startTime := failTime.Add(-20 * time.Minute)
	endTime := failTime.Add(20 * time.Minute)

	return Scenario{
		Name:        "upstream_dependency_failure",
		Description: "Cascading failure due to upstream dependency",
		Incident: &models.Incident{
			ID:              "INC-DEPS-001",
			StartTime:       startTime,
			EndTime:         endTime,
			Severity:        "P1",
			AffectedServices: []string{"checkout-service", "cart-service"},
			Description:     "Checkout failures due to inventory service unavailability",
		},
		Expected: &ExpectedResult{
			TopCandidateService: "inventory-service",
			MinConfidence:       0.6,
			ExpectedSignals:     []string{"error_spike", "topology_propagation"},
			ExpectedFindings:    []string{"upstream", "dependency", "timeout"},
		},
		DataSources: ScenarioDataSources{
			Logs: func(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
				return []models.LogEntry{
					{
						Timestamp: failTime.Add(-15 * time.Minute),
						Service:   "inventory-service",
						Level:     "error",
						Message:   "Database connection lost",
					},
					{
						Timestamp: failTime.Add(-5 * time.Minute),
						Service:   "checkout-service",
						Level:     "error",
						Message:   "Inventory service timeout",
					},
					{
						Timestamp: failTime,
						Service:   "cart-service",
						Level:     "error",
						Message:   "Cannot update cart - inventory unavailable",
					},
				}, nil
			},
			Topology: func(ctx context.Context) (*models.Topology, error) {
				return &models.Topology{
					Nodes: []models.ServiceNode{
						{Name: "checkout-service", Type: "application"},
						{Name: "cart-service", Type: "application"},
						{Name: "inventory-service", Type: "application"},
						{Name: "inventory-db", Type: "database"},
					},
					Edges: []models.DependencyEdge{
						{From: "checkout-service", To: "inventory-service", Type: "http"},
						{From: "cart-service", To: "inventory-service", Type: "http"},
						{From: "inventory-service", To: "inventory-db", Type: "tcp"},
					},
				}, nil
			},
		},
	}
}

// MemoryLeakProgressiveDegradation creates a scenario for memory leak.
func MemoryLeakProgressiveDegradation() Scenario {
	detectTime := time.Date(2024, 6, 15, 9, 0, 0, 0, time.UTC)
	startTime := detectTime.Add(-2 * time.Hour)
	endTime := detectTime.Add(30 * time.Minute)

	return Scenario{
		Name:        "memory_leak_progressive_degradation",
		Description: "Progressive degradation due to memory leak",
		Incident: &models.Incident{
			ID:              "INC-MEM-001",
			StartTime:       startTime,
			EndTime:         endTime,
			Severity:        "P2",
			AffectedServices: []string{"analytics-service"},
			Description:     "Gradual performance degradation over 2 hours",
		},
		Expected: &ExpectedResult{
			TopCandidateService: "analytics-service",
			MinConfidence:       0.5,
			ExpectedSignals:     []string{"metric_anomaly"},
			ExpectedFindings:    []string{"memory", "leak", "gc_pressure"},
		},
		DataSources: ScenarioDataSources{
			Metrics: func(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
				// Create data points showing memory increase over time
				points := []models.DataPoint{}
				for i := 0; i < 12; i++ {
					t := start.Add(time.Duration(i) * 10 * time.Minute)
					// Memory grows from 1GB to 8GB
					value := 1.0 + float64(i)*0.6
					points = append(points, models.DataPoint{Timestamp: t, Value: value})
				}
				return []models.MetricSeries{
					{
						Name:      "jvm_memory_used_bytes",
						Labels:    map[string]string{"service": "analytics-service", "area": "heap"},
						Unit:      "gigabytes",
						DataPoints: points,
					},
					{
						Name: "gc_pause_time_seconds",
						Labels: map[string]string{"service": "analytics-service"},
						Unit:  "seconds",
						DataPoints: []models.DataPoint{
							{Timestamp: start, Value: 0.05},
							{Timestamp: start.Add(30 * time.Minute), Value: 0.2},
							{Timestamp: start.Add(60 * time.Minute), Value: 0.8},
							{Timestamp: start.Add(90 * time.Minute), Value: 2.0},
							{Timestamp: detectTime, Value: 5.0},
						},
					},
				}, nil
			},
		},
	}
}

// SyntheticMetricSource implements MetricSource with synthetic data.
type SyntheticMetricSource struct {
	dataFunc func(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error)
}

func (s *SyntheticMetricSource) QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
	if s.dataFunc != nil {
		return s.dataFunc(ctx, start, end)
	}
	return []models.MetricSeries{}, nil
}

func (s *SyntheticMetricSource) QueryMetric(ctx context.Context, metricName string, start, end time.Time, filters map[string]string) (*models.MetricSeries, error) {
	if s.dataFunc != nil {
		series, err := s.dataFunc(ctx, start, end)
		if err != nil {
			return nil, err
		}
		for _, s := range series {
			if s.Name == metricName {
				return &s, nil
			}
		}
	}
	return &models.MetricSeries{Name: metricName}, nil
}

func (s *SyntheticMetricSource) QueryMetricsForService(ctx context.Context, service string, start, end time.Time) ([]models.MetricSeries, error) {
	if s.dataFunc != nil {
		return s.dataFunc(ctx, start, end)
	}
	return []models.MetricSeries{}, nil
}

func (s *SyntheticMetricSource) GetAlerts(ctx context.Context) ([]sources.Alert, error) {
	return []sources.Alert{}, nil
}

// SyntheticLogSource implements LogSource with synthetic data.
type SyntheticLogSource struct {
	dataFunc func(ctx context.Context, start, end time.Time) ([]models.LogEntry, error)
}

func (s *SyntheticLogSource) QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
	if s.dataFunc != nil {
		return s.dataFunc(ctx, start, end)
	}
	return []models.LogEntry{}, nil
}

func (s *SyntheticLogSource) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	if s.dataFunc != nil {
		logs, err := s.dataFunc(ctx, start, end)
		if err != nil {
			return nil, err
		}
		filtered := []models.LogEntry{}
		for _, log := range logs {
			if log.Service == service {
				filtered = append(filtered, log)
			}
		}
		return filtered, nil
	}
	return []models.LogEntry{}, nil
}

func (s *SyntheticLogSource) GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error) {
	if s.dataFunc != nil {
		logs, err := s.dataFunc(ctx, start, end)
		if err != nil {
			return 0, err
		}
		count := int64(0)
		for _, log := range logs {
			if log.Service == service {
				count++
			}
		}
		return count, nil
	}
	return 0, nil
}

// SyntheticTopologySource implements TopologySource with synthetic data.
type SyntheticTopologySource struct {
	topologyFunc func(ctx context.Context) (*models.Topology, error)
}

func (s *SyntheticTopologySource) GetTopology(ctx context.Context) (*models.Topology, error) {
	if s.topologyFunc != nil {
		return s.topologyFunc(ctx)
	}
	return &models.Topology{}, nil
}

func (s *SyntheticTopologySource) GetServiceNeighbors(ctx context.Context, service string) (dependents, dependencies []string, err error) {
	if s.topologyFunc != nil {
		topo, err := s.topologyFunc(ctx)
		if err != nil {
			return nil, nil, err
		}
		// Find neighbors from topology
		for _, edge := range topo.Edges {
			if edge.From == service {
				dependencies = append(dependencies, edge.To)
			}
			if edge.To == service {
				dependents = append(dependents, edge.From)
			}
		}
	}
	return dependents, dependencies, nil
}

func (s *SyntheticTopologySource) GetServiceHealth(ctx context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

// SyntheticChangeSource implements ChangeSource with synthetic data.
type SyntheticChangeSource struct {
	dataFunc func(ctx context.Context, start, end time.Time) ([]models.Change, error)
}

func (s *SyntheticChangeSource) QueryChanges(ctx context.Context, start, end time.Time) ([]models.Change, error) {
	if s.dataFunc != nil {
		return s.dataFunc(ctx, start, end)
	}
	return []models.Change{}, nil
}

func (s *SyntheticChangeSource) QueryChangesByService(ctx context.Context, service string, start, end time.Time) ([]models.Change, error) {
	if s.dataFunc != nil {
		changes, err := s.dataFunc(ctx, start, end)
		if err != nil {
			return nil, err
		}
		filtered := []models.Change{}
		for _, change := range changes {
			if change.Service == service {
				filtered = append(filtered, change)
			}
		}
		return filtered, nil
	}
	return []models.Change{}, nil
}

func (s *SyntheticChangeSource) GetLastChange(ctx context.Context, service string) (*models.Change, error) {
	if s.dataFunc != nil {
		changes, err := s.dataFunc(ctx, time.Now().Add(-24*time.Hour), time.Now())
		if err != nil {
			return nil, err
		}
		for i := len(changes) - 1; i >= 0; i-- {
			if changes[i].Service == service {
				return &changes[i], nil
			}
		}
	}
	return nil, nil
}

// CreateEngineForScenario creates an RCA engine configured for the given scenario.
func CreateEngineForScenario(scenario Scenario) (*engine.Engine, error) {
	opts := []engine.Option{
		engine.WithLogSource(&SyntheticLogSource{dataFunc: scenario.DataSources.Logs}),
		engine.WithMetricSource(&SyntheticMetricSource{dataFunc: scenario.DataSources.Metrics}),
	}

	if scenario.DataSources.Topology != nil {
		opts = append(opts, engine.WithTopologySource(&SyntheticTopologySource{topologyFunc: scenario.DataSources.Topology}))
	}

	if scenario.DataSources.Changes != nil {
		opts = append(opts, engine.WithChangeSource(&SyntheticChangeSource{dataFunc: scenario.DataSources.Changes}))
	}

	return engine.NewEngine(opts...)
}

// RunScenario executes a single test scenario and returns the result.
func RunScenario(ctx context.Context, scenario Scenario) (*ScenarioResult, error) {
	eng, err := CreateEngineForScenario(scenario)
	if err != nil {
		return nil, fmt.Errorf("failed to create engine: %w", err)
	}

	report, err := eng.Analyze(ctx, scenario.Incident)
	if err != nil {
		return nil, fmt.Errorf("analysis failed: %w", err)
	}

	return &ScenarioResult{
		Scenario:  scenario.Name,
		Report:    report,
		Expected:  scenario.Expected,
		Passed:    validateResult(report, scenario.Expected),
		StartTime: time.Now(),
	}, nil
}

// ScenarioResult represents the result of running a scenario.
type ScenarioResult struct {
	Scenario  string
	Report    *models.RCAReport
	Expected  *ExpectedResult
	Passed    bool
	StartTime time.Time
	Duration  time.Duration
}

// validateResult checks if the RCA result matches expectations.
func validateResult(report *models.RCAReport, expected *ExpectedResult) bool {
	if report.TopCandidate == nil {
		return false
	}

	// Check top candidate service
	if report.TopCandidate.Hypothesis.Service != expected.TopCandidateService {
		return false
	}

	// Check confidence
	if report.Confidence < expected.MinConfidence {
		return false
	}

	return true
}
