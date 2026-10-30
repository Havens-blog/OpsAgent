// Package sources provides tests for the fanout metric source.
package sources

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// fanoutFakeSource is a configurable MetricSource double without the optional
// MetricRangeSource extension.
type fanoutFakeSource struct {
	anomalies     []models.MetricSeries
	anomaliesErr  error
	metric        *models.MetricSeries
	metricErr     error
	forService    []models.MetricSeries
	forServiceErr error
	alerts        []Alert
	alertsErr     error
}

func (s *fanoutFakeSource) QueryAnomalies(context.Context, time.Time, time.Time) ([]models.MetricSeries, error) {
	return s.anomalies, s.anomaliesErr
}

func (s *fanoutFakeSource) QueryMetric(context.Context, string, time.Time, time.Time, map[string]string) (*models.MetricSeries, error) {
	return s.metric, s.metricErr
}

func (s *fanoutFakeSource) QueryMetricsForService(context.Context, string, time.Time, time.Time) ([]models.MetricSeries, error) {
	return s.forService, s.forServiceErr
}

func (s *fanoutFakeSource) GetAlerts(context.Context) ([]Alert, error) {
	return s.alerts, s.alertsErr
}

// fanoutFakeRangeSource adds the optional MetricRangeSource extension.
type fanoutFakeRangeSource struct {
	*fanoutFakeSource
	rangeSeries []models.MetricSeries
	rangeErr    error
}

func (s *fanoutFakeRangeSource) QueryMetricRange(context.Context, string, time.Time, time.Time, string) ([]models.MetricSeries, error) {
	return s.rangeSeries, s.rangeErr
}

func TestFanoutMetricSourceMergesBothSources(t *testing.T) {
	ctx := context.Background()
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()

	app := &fanoutFakeSource{anomalies: []models.MetricSeries{
		{Name: "arms_app_throughput_per_sec", Labels: map[string]string{"service": "order-service"}},
	}}
	infra := &fanoutFakeSource{anomalies: []models.MetricSeries{
		{Name: "AliyunAlb_LoadBalancerHTTPCode5XX", Labels: map[string]string{"instance": "alb-1"}},
	}}

	series, err := NewFanoutMetricSource(app, infra).QueryAnomalies(ctx, start, end)
	if err != nil {
		t.Fatalf("QueryAnomalies failed: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("got %d series, want 2", len(series))
	}
	// Deterministic sort: "AliyunAlb_*" < "arms_app_*".
	if series[0].Name != "AliyunAlb_LoadBalancerHTTPCode5XX" {
		t.Errorf("series[0] = %q, want AliyunAlb_LoadBalancerHTTPCode5XX", series[0].Name)
	}
	if series[1].Name != "arms_app_throughput_per_sec" {
		t.Errorf("series[1] = %q, want arms_app_throughput_per_sec", series[1].Name)
	}
}

func TestFanoutMetricSourceDeduplicatesByNameAndLabels(t *testing.T) {
	ctx := context.Background()
	dup := models.MetricSeries{Name: "dup", Labels: map[string]string{"service": "svc"}}

	a := &fanoutFakeSource{anomalies: []models.MetricSeries{dup}}
	b := &fanoutFakeSource{anomalies: []models.MetricSeries{dup}}

	series, err := NewFanoutMetricSource(a, b).QueryAnomalies(ctx, time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("QueryAnomalies failed: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("got %d series after dedup, want 1", len(series))
	}
}

func TestFanoutMetricSourceDeterministicSortByNameThenService(t *testing.T) {
	ctx := context.Background()
	a := &fanoutFakeSource{anomalies: []models.MetricSeries{
		{Name: "m", Labels: map[string]string{"service": "z"}},
		{Name: "m", Labels: map[string]string{"service": "a"}},
	}}
	b := &fanoutFakeSource{anomalies: []models.MetricSeries{
		{Name: "m", Labels: map[string]string{"service": "b"}},
	}}

	series, err := NewFanoutMetricSource(a, b).QueryAnomalies(ctx, time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("QueryAnomalies failed: %v", err)
	}
	if len(series) != 3 {
		t.Fatalf("got %d series, want 3", len(series))
	}
	if series[0].Labels["service"] != "a" || series[1].Labels["service"] != "b" || series[2].Labels["service"] != "z" {
		t.Errorf("unexpected service order: %v, %v, %v",
			series[0].Labels["service"], series[1].Labels["service"], series[2].Labels["service"])
	}
}

func TestFanoutMetricSourcePartialFailureReturnsSuccess(t *testing.T) {
	ctx := context.Background()
	good := &fanoutFakeSource{anomalies: []models.MetricSeries{
		{Name: "ok", Labels: map[string]string{"service": "svc"}},
	}}
	bad := &fanoutFakeSource{anomaliesErr: errors.New("boom")}

	series, err := NewFanoutMetricSource(good, bad).QueryAnomalies(ctx, time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("expected nil error on partial failure, got %v", err)
	}
	if len(series) != 1 || series[0].Name != "ok" {
		t.Fatalf("expected the healthy source's series, got %v", series)
	}
}

func TestFanoutMetricSourceAllFailureReturnsError(t *testing.T) {
	ctx := context.Background()
	a := &fanoutFakeSource{anomaliesErr: errors.New("first")}
	b := &fanoutFakeSource{anomaliesErr: errors.New("second")}

	series, err := NewFanoutMetricSource(a, b).QueryAnomalies(ctx, time.Now().Add(-time.Hour), time.Now())
	if err == nil {
		t.Fatal("expected error when every source fails")
	}
	if series != nil {
		t.Errorf("expected nil series on total failure, got %v", series)
	}
}

func TestFanoutMetricSourceQueryMetricRangeSkipsNonRangeSources(t *testing.T) {
	ctx := context.Background()
	plain := &fanoutFakeSource{} // no QueryMetricRange method
	r1 := &fanoutFakeRangeSource{
		fanoutFakeSource: &fanoutFakeSource{},
		rangeSeries:      []models.MetricSeries{{Name: "arms_app_errors_per_sec", Labels: map[string]string{"service": "a"}}},
	}
	r2 := &fanoutFakeRangeSource{
		fanoutFakeSource: &fanoutFakeSource{},
		rangeSeries:      []models.MetricSeries{{Name: "arms_app_errors_per_sec", Labels: map[string]string{"service": "b"}}},
	}

	series, err := NewFanoutMetricSource(plain, r1, r2).QueryMetricRange(ctx, "arms_app_errors_per_sec", time.Now().Add(-time.Hour), time.Now(), "5m")
	if err != nil {
		t.Fatalf("QueryMetricRange failed: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("got %d series, want 2", len(series))
	}
	if series[0].Labels["service"] != "a" || series[1].Labels["service"] != "b" {
		t.Errorf("unexpected range order: %v, %v", series[0].Labels["service"], series[1].Labels["service"])
	}
}

func TestFanoutMetricSourceGetAlertsDeduplicates(t *testing.T) {
	ctx := context.Background()
	alert := Alert{Labels: map[string]string{"alertname": "HighErrorRate"}, State: "firing"}
	a := &fanoutFakeSource{alerts: []Alert{alert}}
	b := &fanoutFakeSource{alerts: []Alert{alert}}

	alerts, err := NewFanoutMetricSource(a, b).GetAlerts(ctx)
	if err != nil {
		t.Fatalf("GetAlerts failed: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts, want 1", len(alerts))
	}
}

func TestFanoutMetricSourceQueryMetricFirstSuccess(t *testing.T) {
	ctx := context.Background()
	want := &models.MetricSeries{Name: "cpu_usage"}
	a := &fanoutFakeSource{metric: want}
	b := &fanoutFakeSource{metric: &models.MetricSeries{Name: "cpu_usage_other"}}

	got, err := NewFanoutMetricSource(a, b).QueryMetric(ctx, "cpu_usage", time.Now().Add(-time.Hour), time.Now(), nil)
	if err != nil {
		t.Fatalf("QueryMetric failed: %v", err)
	}
	if got != want {
		t.Errorf("QueryMetric returned %p, want %p", got, want)
	}
}
