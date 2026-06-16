// Package integrations provides concrete implementations of RCA data sources.
package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/yourusername/OpsAgent/internal/rca/models"
	"github.com/yourusername/OpsAgent/internal/rca/sources"
)

// PrometheusMetricSource implements MetricSource using Prometheus.
type PrometheusMetricSource struct {
	client       *http.Client
	endpoint     string
	queryAPI     string
	queryRangeAPI string
}

// NewPrometheusMetricSource creates a new Prometheus metric source.
func NewPrometheusMetricSource(endpoint string) (*PrometheusMetricSource, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("prometheus endpoint is required")
	}

	baseURL := fmt.Sprintf("%s/api/v1", endpoint)
	return &PrometheusMetricSource{
		client:       &http.Client{Timeout: 30 * time.Second},
		endpoint:     endpoint,
		queryAPI:     baseURL + "/query",
		queryRangeAPI: baseURL + "/query_range",
	}, nil
}

// QueryAnomalies retrieves metrics with anomalies.
func (p *PrometheusMetricSource) QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
	queries := []string{
		`rate(http_requests_total{status=~"5.."}[5m])`,
		`rate(http_requests_total{status=~"4.."}[5m])`,
		`rate(http_request_duration_seconds_sum[5m]) / rate(http_request_duration_seconds_count[5m])`,
	}

	var allSeries []models.MetricSeries
	for _, query := range queries {
		series, err := p.executeQuery(ctx, query, start, end)
		if err != nil {
			continue
		}
		allSeries = append(allSeries, series...)
	}

	return allSeries, nil
}

// QueryMetric retrieves a specific metric by name.
func (p *PrometheusMetricSource) QueryMetric(ctx context.Context, metricName string, start, end time.Time, filters map[string]string) (*models.MetricSeries, error) {
	query := buildQuery(metricName, filters)
	series, err := p.executeQuery(ctx, query, start, end)
	if err != nil {
		return nil, err
	}
	if len(series) == 0 {
		return nil, fmt.Errorf("no data for metric: %s", metricName)
	}
	return &series[0], nil
}

// QueryMetricsForService retrieves all metrics for a specific service.
func (p *PrometheusMetricSource) QueryMetricsForService(ctx context.Context, service string, start, end time.Time) ([]models.MetricSeries, error) {
	queries := []string{
		fmt.Sprintf(`rate(http_requests_total{service="%s"}[5m])`, service),
		fmt.Sprintf(`rate(http_request_duration_seconds_sum{service="%s"}[5m]) / rate(http_request_duration_seconds_count{service="%s"}[5m])`, service, service),
		fmt.Sprintf(`rate(http_requests_total{service="%s",status=~"5.."}[5m])`, service),
	}

	var allSeries []models.MetricSeries
	for _, query := range queries {
		series, err := p.executeQuery(ctx, query, start, end)
		if err != nil {
			continue
		}
		allSeries = append(allSeries, series...)
	}

	return allSeries, nil
}

// GetAlerts returns current active alerts from Prometheus.
func (p *PrometheusMetricSource) GetAlerts(ctx context.Context) ([]sources.Alert, error) {
	alertsURL := fmt.Sprintf("%s/api/v1/alerts", p.endpoint)

	req, err := http.NewRequestWithContext(ctx, "GET", alertsURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned status %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Alerts []struct {
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
				StartsAt    time.Time         `json:"startsAt"`
				EndsAt      time.Time         `json:"endsAt"`
				State       string            `json:"state"`
			} `json:"alerts"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	alerts := make([]sources.Alert, 0, len(result.Data.Alerts))
	for _, a := range result.Data.Alerts {
		alert := sources.Alert{
			Labels:      a.Labels,
			Annotations: a.Annotations,
			StartsAt:    a.StartsAt,
			EndsAt:      a.EndsAt,
			State:       a.State,
		}
		if summary, ok := a.Annotations["summary"]; ok {
			alert.Summary = summary
		}
		if description, ok := a.Annotations["description"]; ok {
			alert.Description = description
		}
		alerts = append(alerts, alert)
	}

	return alerts, nil
}

// executeQuery executes a PromQL query and returns metric series.
func (p *PrometheusMetricSource) executeQuery(ctx context.Context, query string, start, end time.Time) ([]models.MetricSeries, error) {
	u, _ := url.Parse(p.queryRangeAPI)
	q := u.Query()
	q.Set("query", query)
	q.Set("start", strconv.FormatInt(start.Unix(), 10))
	q.Set("end", strconv.FormatInt(end.Unix(), 10))
	q.Set("step", "15")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("prometheus query failed: %s", string(body))
	}

	var result struct {
		Data struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Values [][]interface{}   `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	series := make([]models.MetricSeries, 0, len(result.Data.Result))
	for _, r := range result.Data.Result {
		metricName := getMetricName(r.Metric)
		dataPoints := make([]models.DataPoint, 0, len(r.Values))
		for _, v := range r.Values {
			if len(v) < 2 {
				continue
			}
			ts, ok := v[0].(float64)
			if !ok {
				continue
			}
			val, ok := v[1].(string)
			if !ok {
				continue
			}
			fval, err := strconv.ParseFloat(val, 64)
			if err != nil {
				continue
			}
			dataPoints = append(dataPoints, models.DataPoint{
				Timestamp: time.Unix(int64(ts), 0),
				Value:     fval,
			})
		}

		series = append(series, models.MetricSeries{
			Name:       metricName,
			Labels:     r.Metric,
			DataPoints: dataPoints,
		})
	}

	return series, nil
}

func buildQuery(metricName string, filters map[string]string) string {
	query := metricName
	if len(filters) > 0 {
		query += "{"
		first := true
		for k, v := range filters {
			if !first {
				query += ","
			}
			query += fmt.Sprintf(`%s="%s"`, k, v)
			first = false
		}
		query += "}"
	}
	return query
}

func getMetricName(labels map[string]string) string {
	if name, ok := labels["__name__"]; ok {
		return name
	}
	if service, ok := labels["service"]; ok {
		if _, ok := labels["status"]; ok {
			return fmt.Sprintf("http_requests_total{service=%s,status=%s}", service, labels["status"])
		}
		return fmt.Sprintf("http_requests_total{service=%s}", service)
	}
	return "unknown_metric"
}
