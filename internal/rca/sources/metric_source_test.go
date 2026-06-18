// Package sources provides tests for metric source interfaces.
package sources

import (
	"context"
	"testing"
	"time"

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
