// Package apm provides Alibaba Cloud ARMS data source implementation.
package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yourusername/OpsAgent/internal/datasource/core"
	"github.com/yourusername/OpsAgent/internal/datasource/credentials"
)

// AliyunARMSConfig represents the ARMS-specific configuration.
type AliyunARMSConfig struct {
	*core.AliyunARMSConfig
}

// AliyunARMSClient is the client for Alibaba Cloud ARMS.
type AliyunARMSClient struct {
	*BaseAPMDataSource
	config *AliyunARMSConfig
	client *http.Client
	mu     sync.RWMutex
}

// NewAliyunARMSClient creates a new Alibaba Cloud ARMS client.
func NewAliyunARMSClient(config *core.AliyunARMSConfig) (*AliyunARMSClient, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ARMS config: %w", err)
	}

	creds := &credentials.Credentials{
		Type:            credentials.CredentialTypeAccessKey,
		AccessKeyID:     config.AccessKeyID,
		AccessKeySecret: config.AccessKeySecret,
	}
	if config.SecurityToken != "" {
		creds.Type = credentials.CredentialTypeSTSToken
		creds.SecurityToken = config.SecurityToken
	}

	base := NewBaseAPMDataSource(&config.Config)
	base.SetCredentials(creds)

	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://arms.%s.aliyuncs.com", config.Region)
	}

	return &AliyunARMSClient{
		BaseAPMDataSource: base,
		config:           &AliyunARMSConfig{AliyunARMSConfig: config},
		client: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

// Connect establishes a connection to ARMS.
func (c *AliyunARMSClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Test connection via ListApps API
	req, err := c.buildAPIRequest(ctx, "2019-08-08", "ListApps", map[string]string{})
	if err != nil {
		return core.NewNetworkError(string(c.Type()), "connect", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return core.NewNetworkError(string(c.Type()), "connect", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return core.NewAuthError(string(c.Type()), "connect", fmt.Sprintf("connection failed: %s", string(body)))
	}

	c.connected = true
	c.healthStatus = core.StatusHealthy
	c.lastHealthCheck = time.Now()

	return nil
}

// ping checks if ARMS is reachable.
func (c *AliyunARMSClient) ping(ctx context.Context) error {
	req, err := c.buildAPIRequest(ctx, "2019-08-08", "ListApps", map[string]string{})
	if err != nil {
		return err
	}

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

// QueryTraces queries traces from ARMS.
func (c *AliyunARMSClient) QueryTraces(ctx context.Context, req *core.TraceQueryRequest) (*core.TraceQueryResponse, error) {
	if err := c.ValidateTraceQueryRequest(req); err != nil {
		return nil, core.NewValidationError(string(c.Type()), "query_traces", err.Error())
	}

	c.ApplyTraceDefaults(req)

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, core.NewInternalError(string(c.Type()), "query_traces", fmt.Errorf("not connected"))
	}

	params := c.buildTraceQueryParams(req)

	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		return c.executeTraceQuery(ctx, params)
	})

	if err != nil {
		return nil, err
	}

	return result.(*core.TraceQueryResponse), nil
}

// QueryMetrics queries APM metrics from ARMS.
func (c *AliyunARMSClient) QueryMetrics(ctx context.Context, req *core.APMMetricsRequest) (*core.APMMetricsResponse, error) {
	if err := c.ValidateAPMMetricsRequest(req); err != nil {
		return nil, core.NewValidationError(string(c.Type()), "query_metrics", err.Error())
	}

	c.NormalizeAPMMetricsTimeRange(req)

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, core.NewInternalError(string(c.Type()), "query_metrics", fmt.Errorf("not connected"))
	}

	params := c.buildMetricsQueryParams(req)

	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		return c.executeMetricsQuery(ctx, params)
	})

	if err != nil {
		return nil, err
	}

	return result.(*core.APMMetricsResponse), nil
}

// GetTraceDetails retrieves details for a specific trace from ARMS.
func (c *AliyunARMSClient) GetTraceDetails(ctx context.Context, traceID string) (*core.TraceDetails, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, core.NewInternalError(string(c.Type()), "get_trace_details", fmt.Errorf("not connected"))
	}

	params := map[string]string{
		"TraceId": traceID,
	}

	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		return c.executeGetTraceDetails(ctx, params)
	})

	if err != nil {
		return nil, err
	}

	return result.(*core.TraceDetails), nil
}

// Type returns the data source type.
func (c *AliyunARMSClient) Type() core.DataSourceType {
	return core.DataSourceTypeARMS
}

// buildAPIRequest builds an HTTP request for ARMS API.
func (c *AliyunARMSClient) buildAPIRequest(ctx context.Context, version, action, params map[string]string) (*http.Request, error) {
	endpoint := c.config.Endpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://arms.%s.aliyuncs.com", c.config.Region)
	}

	// Add common parameters
	allParams := map[string]string{
		"Version":        version,
		"Action":         action,
		"AccessKeyId":    c.config.AccessKeyID,
		"SignatureMethod": "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":  fmt.Sprintf("%d", time.Now().UnixNano()),
		"Timestamp":       time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Format":         "JSON",
		"RegionId":       c.config.Region,
	}

	for k, v := range params {
		allParams[k] = v
	}

	if c.config.Pid != "" {
		allParams["Pid"] = c.config.Pid
	}

	// Build URL with query parameters
	var queryParts []string
	for k, v := range allParams {
		queryParts = append(queryParts, fmt.Sprintf("%s=%s", k, v))
	}
	fullURL := fmt.Sprintf("%s/?%s", endpoint, strings.Join(queryParts, "&"))

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, err
	}

	return req, nil
}

// buildTraceQueryParams builds trace query parameters for ARMS API.
func (c *AliyunARMSClient) buildTraceQueryParams(req *core.TraceQueryRequest) map[string]string {
	params := map[string]string{
		"StartTime": fmt.Sprintf("%d", req.StartTime.Unix()*1000),
		"EndTime":   fmt.Sprintf("%d", req.EndTime.Unix()*1000),
	}

	if req.TraceID != "" {
		params["TraceId"] = req.TraceID
	}
	if req.ServiceName != "" {
		params["ServiceName"] = req.ServiceName
	}
	if req.MinDuration > 0 {
		params["MinDuration"] = fmt.Sprintf("%d", req.MinDuration.Milliseconds())
	}
	if req.Limit > 0 {
		params["Limit"] = fmt.Sprintf("%d", req.Limit)
	}

	return params
}

// buildMetricsQueryParams builds metrics query parameters for ARMS API.
func (c *AliyunARMSClient) buildMetricsQueryParams(req *core.APMMetricsRequest) map[string]string {
	params := map[string]string{
		"StartTime":  fmt.Sprintf("%d", req.StartTime.Unix()*1000),
		"EndTime":    fmt.Sprintf("%d", req.EndTime.Unix()*1000),
		"MetricType": req.MetricType,
	}

	if req.ServiceName != "" {
		params["ServiceName"] = req.ServiceName
	}
	if req.Interval > 0 {
		params["Interval"] = fmt.Sprintf("%d", req.Interval.Milliseconds())
	}

	return params
}

// executeTraceQuery executes a trace query against ARMS.
func (c *AliyunARMSClient) executeTraceQuery(ctx context.Context, params map[string]string) (*core.TraceQueryResponse, error) {
	req, err := c.buildAPIRequest(ctx, "2019-08-08", "SearchTraces", params)
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "execute_trace_query", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "execute_trace_query", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, core.NewInternalError(string(c.Type()), "execute_trace_query", fmt.Errorf("query failed: %s", string(body)))
	}

	var result struct {
		Data   []map[string]interface{} `json:"Data"`
		Total  int64                    `json:"Total"`
		Code   int                      `json:"Code"`
		Message string                  `json:"Message"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, core.NewParsingError(string(c.Type()), "execute_trace_query", "failed to parse response", err)
	}

	if result.Code != 200 {
		return nil, core.NewInternalError(string(c.Type()), "execute_trace_query", fmt.Errorf("API error: %s", result.Message))
	}

	// Convert traces
	traces := make([]core.TraceSummary, 0, len(result.Data))
	for _, raw := range result.Data {
		summary, err := c.ParseTraceSummary(raw)
		if err != nil {
			continue
		}
		traces = append(traces, *summary)
	}

	return &core.TraceQueryResponse{
		Traces:     traces,
		TotalCount: result.Total,
	}, nil
}

// executeMetricsQuery executes a metrics query against ARMS.
func (c *AliyunARMSClient) executeMetricsQuery(ctx context.Context, params map[string]string) (*core.APMMetricsResponse, error) {
	req, err := c.buildAPIRequest(ctx, "2019-08-08", "QueryMetricByPage", params)
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "execute_metrics_query", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "execute_metrics_query", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, core.NewInternalError(string(c.Type()), "execute_metrics_query", fmt.Errorf("query failed: %s", string(body)))
	}

	var result struct {
		Data   []map[string]interface{} `json:"Data"`
		Code   int                      `json:"Code"`
		Message string                  `json:"Message"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, core.NewParsingError(string(c.Type()), "execute_metrics_query", "failed to parse response", err)
	}

	if result.Code != 200 {
		return nil, core.NewInternalError(string(c.Type()), "execute_metrics_query", fmt.Errorf("API error: %s", result.Message))
	}

	serviceName := params["ServiceName"]
	metricType := params["MetricType"]

	dataPoints := make([]core.APMDataPoint, 0, len(result.Data))
	for _, raw := range result.Data {
		dp, err := c.parseAPMDataPoint(raw)
		if err != nil {
			continue
		}
		dataPoints = append(dataPoints, *dp)
	}

	return &core.APMMetricsResponse{
		ServiceName: serviceName,
		MetricType:  metricType,
		DataPoints:  dataPoints,
	}, nil
}

// executeGetTraceDetails executes a trace details query against ARMS.
func (c *AliyunARMSClient) executeGetTraceDetails(ctx context.Context, params map[string]string) (*core.TraceDetails, error) {
	req, err := c.buildAPIRequest(ctx, "2019-08-08", "GetTrace", params)
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "execute_get_trace_details", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "execute_get_trace_details", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, core.NewInternalError(string(c.Type()), "execute_get_trace_details", fmt.Errorf("query failed: %s", string(body)))
	}

	var result struct {
		Data   map[string]interface{} `json:"Data"`
		Code   int                    `json:"Code"`
		Message string                `json:"Message"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, core.NewParsingError(string(c.Type()), "execute_get_trace_details", "failed to parse response", err)
	}

	if result.Code != 200 {
		return nil, core.NewInternalError(string(c.Type()), "execute_get_trace_details", fmt.Errorf("API error: %s", result.Message))
	}

	// Parse trace details
	data := result.Data

	// Extract trace summary
	summary, err := c.ParseTraceSummary(data)
	if err != nil {
		return nil, err
	}

	// Extract spans
	var spans []core.Span
	if rawSpans, ok := data["spans"].([]interface{}); ok {
		for _, rawSpan := range rawSpans {
			if spanMap, ok := rawSpan.(map[string]interface{}); ok {
				span, err := c.ParseSpan(spanMap)
				if err != nil {
					continue
				}
				spans = append(spans, *span)
			}
		}
	}

	return &core.TraceDetails{
		TraceSummary: *summary,
		Spans:       spans,
	}, nil
}

// parseAPMDataPoint parses a raw APM data point.
func (c *AliyunARMSClient) parseAPMDataPoint(raw map[string]interface{}) (*core.APMDataPoint, error) {
	dp := &core.APMDataPoint{}

	// Extract timestamp
	if ts, ok := raw["timestamp"]; ok {
		switch v := ts.(type) {
		case float64:
			dp.Timestamp = time.Unix(int64(v)/1000, 0)
		case string:
			if parsed, err := time.Parse(time.RFC3339, v); err == nil {
				dp.Timestamp = parsed
			}
		}
	}

	// Extract value
	if val, ok := raw["value"]; ok {
		switch v := val.(type) {
		case float64:
			dp.Value = v
		case string:
			if f, err := fmt.Sprintf(v); err == nil {
				_ = f // handled below
			}
		}
	}

	return dp, nil
}

// ListApps lists ARMS-monitored applications.
func (c *AliyunARMSClient) ListApps(ctx context.Context) ([]map[string]interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, core.NewInternalError(string(c.Type()), "list_apps", fmt.Errorf("not connected"))
	}

	req, err := c.buildAPIRequest(ctx, "2019-08-08", "ListApps", map[string]string{})
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "list_apps", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, core.NewInternalError(string(c.Type()), "list_apps", fmt.Errorf("failed with status %d", resp.StatusCode))
	}

	var result struct {
		Data   []map[string]interface{} `json:"Data"`
		Code   int                      `json:"Code"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, core.NewParsingError(string(c.Type()), "list_apps", "failed to parse response", err)
	}

	return result.Data, nil
}

// Close closes the ARMS client.
func (c *AliyunARMSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	c.healthStatus = core.StatusUnknown
	c.client.CloseIdleConnections()

	return nil
}
