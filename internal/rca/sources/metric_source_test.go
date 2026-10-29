// Package sources provides tests for metric source interfaces.
package sources

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/datasource/core"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// TestMetricSourceAlert tests Alert struct.
func TestMetricSourceAlert(t *testing.T) {
	alert := Alert{
		Labels:      map[string]string{"alertname": "HighErrorRate"},
		Annotations: map[string]string{"summary": "Too many errors"},
		StartsAt:    time.Now(),
		EndsAt:      time.Now().Add(5 * time.Minute),
		State:       "firing",
		Summary:     "High error rate",
		Description: "Error rate exceeded threshold",
	}

	if alert.State != "firing" {
		t.Errorf("Expected state 'firing', got '%s'", alert.State)
	}
	if alert.Summary != "High error rate" {
		t.Errorf("Expected summary 'High error rate', got '%s'", alert.Summary)
	}
}

// MockMetricSourceForTest is a test implementation of MetricSource.
type MockMetricSourceForTest struct{}

func (m *MockMetricSourceForTest) QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
	return []models.MetricSeries{
		{Name: "http_errors", Labels: map[string]string{"status": "5xx"}},
	}, nil
}

func (m *MockMetricSourceForTest) QueryMetric(ctx context.Context, metricName string, start, end time.Time, filters map[string]string) (*models.MetricSeries, error) {
	return &models.MetricSeries{Name: metricName, Labels: filters}, nil
}

func (m *MockMetricSourceForTest) QueryMetricsForService(ctx context.Context, service string, start, end time.Time) ([]models.MetricSeries, error) {
	return []models.MetricSeries{
		{Name: "http_requests", Labels: map[string]string{"service": service}},
	}, nil
}

func (m *MockMetricSourceForTest) GetAlerts(ctx context.Context) ([]Alert, error) {
	return []Alert{
		{Labels: map[string]string{"alertname": "Test"}, State: "firing"},
	}, nil
}

func (m *MockMetricSourceForTest) QueryMetricRange(ctx context.Context, query string, start, end time.Time, step string) ([]models.MetricSeries, error) {
	return []models.MetricSeries{
		{Name: "http_errors", Labels: map[string]string{"status": "5xx"}},
		{Name: "http_errors", Labels: map[string]string{"status": "4xx"}},
	}, nil
}

// TestMetricSourceInterface tests metric source interface compliance.
func TestMetricSourceInterface(t *testing.T) {
	ctx := context.Background()
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()
	mock := &MockMetricSourceForTest{}

	series, err := mock.QueryAnomalies(ctx, start, end)
	if err != nil {
		t.Fatalf("QueryAnomalies failed: %v", err)
	}
	if len(series) == 0 {
		t.Error("Expected at least one series")
	}

	s, err := mock.QueryMetric(ctx, "test_metric", start, end, nil)
	if err != nil {
		t.Fatalf("QueryMetric failed: %v", err)
	}
	if s == nil {
		t.Error("Expected non-nil series")
	}

	series, err = mock.QueryMetricsForService(ctx, "api-service", start, end)
	if err != nil {
		t.Fatalf("QueryMetricsForService failed: %v", err)
	}
	if len(series) == 0 {
		t.Error("Expected at least one series")
	}

	alerts, err := mock.GetAlerts(ctx)
	if err != nil {
		t.Fatalf("GetAlerts failed: %v", err)
	}
	if len(alerts) == 0 {
		t.Error("Expected at least one alert")
	}
}

// TestMetricSourceQueryMetricRange pins the multi-series contract of the range
// extension: it must return every series, never just the first one.
func TestMetricSourceQueryMetricRange(t *testing.T) {
	ctx := context.Background()
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()
	mock := &MockMetricSourceForTest{}

	series, err := mock.QueryMetricRange(ctx, "http_errors", start, end, "1m")
	if err != nil {
		t.Fatalf("QueryMetricRange failed: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("QueryMetricRange returned %d series, want 2", len(series))
	}
}

// TestParseStepDuration pins the step parsing used to forward step into the
// underlying core.MetricsQueryRequest.
func TestParseStepDuration(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{in: "", want: time.Minute},
		{in: "5m", want: 5 * time.Minute},
		{in: "1h", want: time.Hour},
		{in: "30s", want: 30 * time.Second},
		{in: "not-a-duration", want: time.Minute},
	}
	for _, tc := range tests {
		if got := parseStepDuration(tc.in); got != tc.want {
			t.Errorf("parseStepDuration(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// fakeMetricsDataSource is a minimal core.MetricsDataSource for proving that
// DSMetricSource.QueryMetricRange forwards the parsed step and returns every
// core series converted to models.MetricSeries.
type fakeMetricsDataSource struct {
	gotStep  time.Duration
	gotQuery string
	series   []core.MetricSeries
}

func (f *fakeMetricsDataSource) QueryMetrics(_ context.Context, req *core.MetricsQueryRequest) (*core.MetricsQueryResponse, error) {
	f.gotStep = req.Step
	f.gotQuery = req.Query
	return &core.MetricsQueryResponse{Series: f.series}, nil
}

func (f *fakeMetricsDataSource) Connect(context.Context) error { return nil }
func (f *fakeMetricsDataSource) Close() error                  { return nil }
func (f *fakeMetricsDataSource) Ping(context.Context) error    { return nil }
func (f *fakeMetricsDataSource) Health(context.Context) (*core.HealthInfo, error) {
	return nil, nil
}
func (f *fakeMetricsDataSource) Type() core.DataSourceType { return core.DataSourceTypePrometheus }
func (f *fakeMetricsDataSource) Name() string              { return "fake" }

// TestDSMetricSourceQueryMetricRangeForwardsStepAndAllSeries verifies the real
// source path end to end at the core-boundary level.
func TestDSMetricSourceQueryMetricRangeForwardsStepAndAllSeries(t *testing.T) {
	fake := &fakeMetricsDataSource{
		series: []core.MetricSeries{
			{
				Labels:     map[string]string{"service": "checkout-api", "cloud": "arms"},
				DataPoints: []core.DataPoint{{Timestamp: time.Now(), Value: 1.5}},
			},
			{
				Labels:     map[string]string{"service": "ledger-db", "cloud": "arms"},
				DataPoints: []core.DataPoint{{Timestamp: time.Now(), Value: 2.5}},
			},
		},
	}
	src := NewDSMetricSource(fake)

	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()
	series, err := src.QueryMetricRange(context.Background(), `arms_app_errors_per_sec{cloud="arms"}`, start, end, "5m")
	if err != nil {
		t.Fatalf("QueryMetricRange failed: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("QueryMetricRange returned %d series, want 2", len(series))
	}
	if fake.gotStep != 5*time.Minute {
		t.Errorf("step forwarded to core = %s, want 5m", fake.gotStep)
	}
	if fake.gotQuery != `arms_app_errors_per_sec{cloud="arms"}` {
		t.Errorf("query forwarded to core = %q", fake.gotQuery)
	}
}
