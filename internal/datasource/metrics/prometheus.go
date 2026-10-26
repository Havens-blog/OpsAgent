// Package metrics provides Prometheus data source implementation.
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/datasource/core"
)

// PrometheusClient is the client for Prometheus.
type PrometheusClient struct {
	*BaseMetricsDataSource
	config *core.PrometheusConfig
	client *http.Client
}

// NewPrometheusClient creates a new Prometheus client.
func NewPrometheusClient(config *core.PrometheusConfig) (*PrometheusClient, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid Prometheus config: %w", err)
	}

	base := NewBaseMetricsDataSource(&config.Config)

	return &PrometheusClient{
		BaseMetricsDataSource: base,
		config:                config,
		client: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

// Connect establishes a connection to Prometheus.
func (c *PrometheusClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Test connection via /api/v1/query with a simple query
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/query", c.config.Endpoint))
	if err != nil {
		return core.NewNetworkError(string(c.Type()), "connect", err)
	}

	q := u.Query()
	q.Set("query", "up")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return core.NewNetworkError(string(c.Type()), "connect", err)
	}

	c.addAuthHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return core.NewNetworkError(string(c.Type()), "connect", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return core.NewAuthError(string(c.Type()), "connect", fmt.Sprintf("connection failed: %s", string(body)), nil)
	}

	c.connected = true
	c.healthStatus = core.StatusHealthy
	c.lastHealthCheck = time.Now()

	return nil
}

// ping checks if Prometheus is reachable.
func (c *PrometheusClient) ping(ctx context.Context) error {
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/query", c.config.Endpoint))
	if err != nil {
		return err
	}

	q := u.Query()
	q.Set("query", "up")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}

	c.addAuthHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping failed with status %d", resp.StatusCode)
	}
	return nil
}

// QueryMetrics queries metrics from Prometheus.
func (c *PrometheusClient) QueryMetrics(ctx context.Context, req *core.MetricsQueryRequest) (*core.MetricsQueryResponse, error) {
	if err := c.ValidateMetricsQueryRequest(req); err != nil {
		return nil, core.NewValidationError(string(c.Type()), "query_metrics", err.Error())
	}

	c.NormalizeMetricsTimeRange(req)

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, core.NewInternalError(string(c.Type()), "query_metrics", fmt.Errorf("not connected"))
	}

	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		return c.executeQuery(ctx, req)
	})

	if err != nil {
		return nil, err
	}

	return result.(*core.MetricsQueryResponse), nil
}

// Type returns the data source type.
func (c *PrometheusClient) Type() core.DataSourceType {
	return core.DataSourceTypePrometheus
}

// Close closes the Prometheus client.
func (c *PrometheusClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	c.healthStatus = core.StatusUnknown
	c.client.CloseIdleConnections()

	return nil
}

// addAuthHeaders adds authentication headers to the request.
func (c *PrometheusClient) addAuthHeaders(req *http.Request) {
	// Bearer token takes precedence
	if c.config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.BearerToken)
		return
	}

	// Basic auth
	if c.config.Username != "" && c.config.Password != "" {
		req.SetBasicAuth(c.config.Username, c.config.Password)
		return
	}
}

// executeQuery executes a PromQL query and returns metric series.
func (c *PrometheusClient) executeQuery(ctx context.Context, req *core.MetricsQueryRequest) (*core.MetricsQueryResponse, error) {
	// Use query_range API for time range queries
	u, _ := url.Parse(fmt.Sprintf("%s/api/v1/query_range", c.config.Endpoint))
	q := u.Query()
	q.Set("query", req.Query)
	q.Set("start", strconv.FormatInt(req.StartTime.Unix(), 10))
	q.Set("end", strconv.FormatInt(req.EndTime.Unix(), 10))
	q.Set("step", strconv.FormatInt(int64(req.Step.Seconds()), 10))
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "execute_query", err)
	}

	c.addAuthHeaders(httpReq)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "execute_query", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, core.NewInternalError(string(c.Type()), "execute_query", fmt.Errorf("query failed: %s", string(body)))
	}

	var result struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Values [][]interface{}   `json:"values"`
			} `json:"result"`
		} `json:"data"`
		Error string `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, core.NewParsingError(string(c.Type()), "execute_query", "failed to parse response", err)
	}

	if result.Status != "success" {
		return nil, core.NewInternalError(string(c.Type()), "execute_query", fmt.Errorf("API error: %s", result.Error))
	}

	series := make([]core.MetricSeries, 0, len(result.Data.Result))
	for _, r := range result.Data.Result {
		dataPoints := make([]core.DataPoint, 0, len(r.Values))
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
			dataPoints = append(dataPoints, core.DataPoint{
				Timestamp: time.Unix(int64(ts), 0),
				Value:     fval,
			})
		}

		series = append(series, core.MetricSeries{
			Labels:     r.Metric,
			DataPoints: dataPoints,
		})
	}

	return &core.MetricsQueryResponse{
		Series: series,
	}, nil
}
