// Package integrations provides concrete implementations of RCA data sources.
package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

// PrometheusMetricSource implements MetricSource using Prometheus.
type PrometheusMetricSource struct {
	client        *http.Client
	endpoint      string
	queryAPI      string
	queryRangeAPI string
	token         string // optional bearer token (e.g. ARMS Prometheus)
}

// NewPrometheusMetricSource creates a new Prometheus metric source.
func NewPrometheusMetricSource(endpoint string) (*PrometheusMetricSource, error) {
	return NewPrometheusMetricSourceWithToken(endpoint, "")
}

// NewPrometheusMetricSourceWithToken creates a Prometheus metric source that
// authenticates with a bearer token. Managed services such as Alibaba Cloud
// ARMS Prometheus require this token on every request.
func NewPrometheusMetricSourceWithToken(endpoint, token string) (*PrometheusMetricSource, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("prometheus endpoint is required")
	}

	baseURL := fmt.Sprintf("%s/api/v1", endpoint)
	return &PrometheusMetricSource{
		client:        &http.Client{Timeout: 30 * time.Second},
		endpoint:      endpoint,
		queryAPI:      baseURL + "/query",
		queryRangeAPI: baseURL + "/query_range",
		token:         token,
	}, nil
}

// Ping verifies the Prometheus/ARMS endpoint is reachable and the token (if
// any) is accepted, by issuing a trivial instant query. It returns nil only on
// a real HTTP 200, so callers can treat a successful Ping as "this source is
// genuinely usable" rather than merely "constructed". vector(1) always yields
// a result on a healthy Prometheus-compatible backend, so an empty dataset is
// not mistaken for failure.
func (p *PrometheusMetricSource) Ping(ctx context.Context) error {
	u, err := url.Parse(p.queryAPI)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("query", "vector(1)")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}
	p.setAuth(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("prometheus ping failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// setAuth attaches the bearer token to a request when one is configured.
func (p *PrometheusMetricSource) setAuth(req *http.Request) {
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
}

// QueryAnomalies retrieves metrics that commonly signal anomalies.
//
// It issues two families of queries and returns whatever the backend has data
// for, so it works against both self-hosted application Prometheus and managed
// Alibaba Cloud ARMS/CloudMonitor:
//
//   - Application metrics (self-hosted Prometheus): http_requests_total etc.,
//     which are counters, hence wrapped in rate().
//   - CloudMonitor metrics (ARMS managed Prometheus): AliyunAlb_*/AliyunSlb_*
//     error-code and latency series. These are already per-second gauges
//     (measure unit "Count/s"), so they must NOT be wrapped in rate().
//
// Queries with no data simply return no series and are skipped.
func (p *PrometheusMetricSource) QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
	queries := []string{
		// --- Self-hosted application Prometheus (counters -> rate) ---
		`rate(http_requests_total{status=~"5.."}[5m])`,
		`rate(http_requests_total{status=~"4.."}[5m])`,
		`rate(http_request_duration_seconds_sum[5m]) / rate(http_request_duration_seconds_count[5m])`,

		// --- Alibaba Cloud CloudMonitor / ARMS (gauges -> raw) ---
		// 5xx error rates at load-balancer and listener scope.
		`AliyunAlb_LoadBalancerHTTPCode5XX`,
		`AliyunAlb_ListenerHTTPCode5XX`,
		`AliyunSlb_InstanceStatusCode5xx`,
		// Upstream latency / response time.
		`AliyunAlb_LoadBalancerUpstreamResponseTime`,
		`AliyunAlb_ListenerUpstreamResponseTime`,
		// Connection-level errors.
		`AliyunAlb_LoadBalancerUpstreamConnectionError`,

		// --- Resource saturation signals (topk of RAW gauges) ---
		//
		// Important: the RCA engine detects anomalies by TREND (window-over-
		// window change), not by absolute threshold. So we must feed it
		// CONTINUOUS series and let it decide — a threshold filter like
		// `metric > 85` returns discontinuous points that break trend
		// detection. This also matches RCA semantics: the engine should surface
		// resources that RAMPED during the incident window (a trend), not those
		// merely sitting flat-high (a standing condition, rarely the cause of a
		// new incident).
		//
		// topk bounds each family to the N busiest resources so thousands of
		// per-instance series (e.g. ECS CPU has ~680) cannot flood RCA.
		`topk(10, AliyunEcs_CPUUtilization)`,
		`topk(10, AliyunEcs_memory_usedutilization)`,
		`topk(10, AliyunRds_CpuUsage)`,
		`topk(10, AliyunRds_ConnectionUsage)`,
		`topk(10, AliyunRds_MemoryUsage)`,
		`topk(10, AliyunRds_IOPSUsage)`,
		`topk(10, kafka_consumergroup_lag)`,

		// --- Alibaba Cloud ARMS APM (per-service RED trends) ---
		//
		// ARMS APM stores expose only application RED counters (arms_app_*),
		// none of the families above, so without these an APM-integrated
		// workspace yields no metric anomalies at all. These are the three
		// confirmed-populated raw counters (the same ones the topology source's
		// RED view uses), aggregated by service and wrapped in rate() because
		// they are counters.
		//
		// Two deliberate choices:
		//   - topk(20, ...) bounds cardinality: a busy workspace has hundreds
		//     of services, and feeding every one would flood RCA. We surface
		//     the busiest/worst by each signal and let trend detection decide.
		//   - label_replace injects __name__, because `sum by (service)` drops
		//     the metric name; without it getMetricName would mislabel these
		//     APM series as http_requests_total. Pure PromQL, no Go-side change.
		`label_replace(topk(20, sum by (service) (rate(arms_app_requests_error_count_raw[5m]))), "__name__", "arms_app_errors_per_sec", "", "")`,
		`label_replace(topk(20, 1000 * sum by (service) (rate(arms_app_requests_seconds_raw[5m])) / sum by (service) (rate(arms_app_requests_count_raw[5m]))), "__name__", "arms_app_latency_ms", "", "")`,
		`label_replace(topk(20, sum by (service) (rate(arms_app_requests_count_raw[5m]))), "__name__", "arms_app_throughput_per_sec", "", "")`,
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
	p.setAuth(req)

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
	p.setAuth(req)

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
			// Drop non-finite values (+Inf/NaN). They arise from divide-by-
			// near-zero in ratio queries (e.g. latency = time/count for a
			// low-traffic service) and are JSON-unsafe: a single one makes
			// json.Marshal fail and takes down the whole RCA response. The
			// topology source's queryServiceValues guards the same way.
			if math.IsInf(fval, 0) || math.IsNaN(fval) {
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

// QueryMetricRange executes an arbitrary PromQL range query with the given
// step and returns every matching series. It is the multi-series counterpart
// to QueryMetric and honours the caller's step instead of a fixed 15s.
func (p *PrometheusMetricSource) QueryMetricRange(ctx context.Context, query string, start, end time.Time, step string) ([]models.MetricSeries, error) {
	if step == "" {
		step = "60"
	}

	u, _ := url.Parse(p.queryRangeAPI)
	q := u.Query()
	q.Set("query", query)
	q.Set("start", strconv.FormatInt(start.Unix(), 10))
	q.Set("end", strconv.FormatInt(end.Unix(), 10))
	q.Set("step", step)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	p.setAuth(req)

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
			if math.IsInf(fval, 0) || math.IsNaN(fval) {
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
