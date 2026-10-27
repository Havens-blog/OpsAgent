// Package apm provides Alibaba Cloud ARMS data source implementation.
package apm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/datasource/core"
	dscredentials "github.com/Havens-blog/OpsAgent/internal/datasource/credentials"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/arms"
)

// AliyunARMSConfig represents the ARMS-specific configuration.
type AliyunARMSConfig struct {
	*core.AliyunARMSConfig
}

// AliyunARMSClient is the client for Alibaba Cloud ARMS using the official SDK.
type AliyunARMSClient struct {
	*BaseAPMDataSource
	config     *AliyunARMSConfig
	armsClient *arms.Client
	mu         sync.RWMutex
}

// NewAliyunARMSClient creates a new Alibaba Cloud ARMS client using the official SDK.
func NewAliyunARMSClient(config *core.AliyunARMSConfig) (*AliyunARMSClient, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ARMS config: %w", err)
	}

	// Prepare credential for SDK
	var credential auth.Credential
	if config.SecurityToken != "" {
		// Use STS token
		credential = credentials.NewStsTokenCredential(
			config.AccessKeyID,
			config.AccessKeySecret,
			config.SecurityToken,
		)
	} else {
		// Use access key
		credential = credentials.NewAccessKeyCredential(
			config.AccessKeyID,
			config.AccessKeySecret,
		)
	}

	// Create SDK config with timeout
	sdkConfig := sdk.NewConfig()
	if config.Timeout > 0 {
		sdkConfig.Timeout = time.Duration(config.Timeout) * time.Second
	}
	sdkConfig.Scheme = "https"

	// Create ARMS client using NewClientWithOptions
	armsClient, err := arms.NewClientWithOptions(
		config.Region,
		sdkConfig,
		credential,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ARMS client: %w", err)
	}

	// Create credentials for base datasource
	creds := &dscredentials.Credentials{
		Type:            dscredentials.CredentialTypeAccessKey,
		AccessKeyID:     config.AccessKeyID,
		AccessKeySecret: config.AccessKeySecret,
	}
	if config.SecurityToken != "" {
		creds.Type = dscredentials.CredentialTypeSTSToken
		creds.SecurityToken = config.SecurityToken
	}

	base := NewBaseAPMDataSource(&config.Config)
	base.SetCredentials(creds)

	return &AliyunARMSClient{
		BaseAPMDataSource: base,
		config:            &AliyunARMSConfig{AliyunARMSConfig: config},
		armsClient:        armsClient,
	}, nil
}

// Connect establishes a connection to ARMS.
func (c *AliyunARMSClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Test connection by searching traces with minimal params
	req := arms.CreateSearchTracesRequest()
	req.Pid = c.config.Pid
	req.StartTime = requests.NewInteger64(time.Now().Add(-5 * time.Minute).UnixMilli())
	req.EndTime = requests.NewInteger64(time.Now().UnixMilli())

	_, err := c.armsClient.SearchTraces(req)
	if err != nil {
		return core.NewAuthError(string(c.Type()), "connect", fmt.Sprintf("failed to connect: %v", err), nil)
	}

	c.connected = true
	c.healthStatus = core.StatusHealthy
	c.lastHealthCheck = time.Now()

	return nil
}

// ping checks if ARMS is reachable.
func (c *AliyunARMSClient) ping(ctx context.Context) error {
	req := arms.CreateSearchTracesRequest()
	req.Pid = c.config.Pid
	req.StartTime = requests.NewInteger64(time.Now().Add(-5 * time.Minute).UnixMilli())
	req.EndTime = requests.NewInteger64(time.Now().UnixMilli())

	_, err := c.armsClient.SearchTraces(req)
	return err
}

// QueryTraces queries traces from ARMS using the official SDK.
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

	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		return c.executeTraceQuery(req)
	})

	if err != nil {
		return nil, err
	}

	return result.(*core.TraceQueryResponse), nil
}

// QueryMetrics queries APM metrics from ARMS using the official SDK.
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

	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		return c.executeMetricsQuery(req)
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

	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		return c.executeGetTraceDetails(traceID)
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

// executeTraceQuery executes a trace query using the official SDK.
func (c *AliyunARMSClient) executeTraceQuery(req *core.TraceQueryRequest) (*core.TraceQueryResponse, error) {
	// Build SearchTraces request
	searchReq := arms.CreateSearchTracesRequest()
	searchReq.Pid = c.config.Pid
	searchReq.StartTime = requests.NewInteger64(req.StartTime.UnixMilli())
	searchReq.EndTime = requests.NewInteger64(req.EndTime.UnixMilli())

	if req.ServiceName != "" {
		searchReq.ServiceName = req.ServiceName
	}
	if req.MinDuration > 0 {
		searchReq.MinDuration = requests.NewInteger64(int64(req.MinDuration.Milliseconds()))
	}

	// Execute search
	resp, err := c.armsClient.SearchTraces(searchReq)
	if err != nil {
		return nil, core.NewInternalError(string(c.Type()), "execute_trace_query", fmt.Errorf("search failed: %w", err))
	}

	// Convert to our format
	return c.convertSearchTracesResponse(resp, req.Limit)
}

// convertSearchTracesResponse converts SDK response to our format.
func (c *AliyunARMSClient) convertSearchTracesResponse(resp *arms.SearchTracesResponse, limit int) (*core.TraceQueryResponse, error) {
	response := &core.TraceQueryResponse{
		Traces: make([]core.TraceSummary, 0),
	}

	for _, traceInfo := range resp.TraceInfos {
		// Use Timestamp instead of StartTime
		summary, err := c.ParseTraceSummary(map[string]interface{}{
			"traceID":       traceInfo.TraceID,
			"duration":      traceInfo.Duration,
			"serviceName":   traceInfo.ServiceName,
			"operationName": traceInfo.OperationName,
			"startTime":     traceInfo.Timestamp,
		})
		if err != nil {
			continue
		}

		response.Traces = append(response.Traces, *summary)

		// Apply limit if specified
		if limit > 0 && len(response.Traces) >= limit {
			break
		}
	}

	response.TotalCount = int64(len(response.Traces))

	return response, nil
}

// executeMetricsQuery executes a metrics query using the official SDK.
func (c *AliyunARMSClient) executeMetricsQuery(req *core.APMMetricsRequest) (*core.APMMetricsResponse, error) {
	// Build QueryMetricByPage request
	queryReq := arms.CreateQueryMetricByPageRequest()
	queryReq.StartTime = requests.NewInteger64(req.StartTime.UnixMilli())
	queryReq.EndTime = requests.NewInteger64(req.EndTime.UnixMilli())
	queryReq.Metric = req.MetricType

	if req.ServiceName != "" {
		// Add service filter
		filters := &[]arms.QueryMetricByPageFilters{
			{Key: "serviceName", Value: req.ServiceName},
		}
		queryReq.Filters = filters
	}

	if req.Interval > 0 {
		queryReq.IntervalInSec = requests.NewInteger64(int64(req.Interval.Seconds()))
	}

	// Pagination
	queryReq.CurrentPage = requests.NewInteger64(1)
	queryReq.PageSize = requests.NewInteger64(1000)

	// Execute query
	resp, err := c.armsClient.QueryMetricByPage(queryReq)
	if err != nil {
		return nil, core.NewInternalError(string(c.Type()), "execute_metrics_query", fmt.Errorf("query failed: %w", err))
	}

	if !resp.Success {
		return nil, core.NewInternalError(string(c.Type()), "execute_metrics_query", fmt.Errorf("API error: %s", resp.Message))
	}

	// Convert to our format
	return c.convertMetricsResponse(resp, req)
}

// convertMetricsResponse converts SDK response to our format.
func (c *AliyunARMSClient) convertMetricsResponse(resp *arms.QueryMetricByPageResponse, req *core.APMMetricsRequest) (*core.APMMetricsResponse, error) {
	response := &core.APMMetricsResponse{
		ServiceName: req.ServiceName,
		MetricType:  req.MetricType,
		DataPoints:  make([]core.APMDataPoint, 0),
	}

	// Parse data from response - Items is []map[string]interface{}
	for _, item := range resp.Data.Items {
		dp, err := c.parseAPMDataPoint(item)
		if err != nil {
			continue
		}
		response.DataPoints = append(response.DataPoints, *dp)
	}

	return response, nil
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
			if f, err := fmt.Sscanf(v, "%f", &dp.Value); err == nil && f == 1 {
				// value parsed
			}
		}
	}

	return dp, nil
}

// executeGetTraceDetails executes a trace details query using the official SDK.
func (c *AliyunARMSClient) executeGetTraceDetails(traceID string) (*core.TraceDetails, error) {
	// Build GetTrace request
	getReq := arms.CreateGetTraceRequest()
	getReq.TraceID = traceID
	getReq.StartTime = requests.NewInteger64(time.Now().Add(-24 * time.Hour).UnixMilli())
	getReq.EndTime = requests.NewInteger64(time.Now().UnixMilli())

	// Execute query
	resp, err := c.armsClient.GetTrace(getReq)
	if err != nil {
		return nil, core.NewInternalError(string(c.Type()), "execute_get_trace_details", fmt.Errorf("query failed: %w", err))
	}

	// Convert to our format
	return c.convertGetTraceResponse(resp, traceID)
}

// convertGetTraceResponse converts SDK GetTrace response to our format.
func (c *AliyunARMSClient) convertGetTraceResponse(resp *arms.GetTraceResponse, traceID string) (*core.TraceDetails, error) {
	// Build trace summary from spans
	var traceSummary *core.TraceSummary
	spans := make([]core.Span, 0, len(resp.Spans))

	for _, spanInfo := range resp.Spans {
		// Parse span
		span, err := c.ParseSpan(map[string]interface{}{
			"spanID":        spanInfo.SpanId,
			"parentSpanID":  spanInfo.ParentSpanId,
			"operationName": spanInfo.OperationName,
			"startTime":     spanInfo.Timestamp,
			"duration":      spanInfo.Duration,
			"serviceName":   spanInfo.ServiceName,
		})
		if err != nil {
			continue
		}
		spans = append(spans, *span)

		// Use the root span (no parent) for trace summary
		if traceSummary == nil && spanInfo.ParentSpanId == "" {
			summary, err := c.ParseTraceSummary(map[string]interface{}{
				"traceID":       traceID,
				"duration":      spanInfo.Duration,
				"serviceName":   spanInfo.ServiceName,
				"operationName": spanInfo.OperationName,
				"startTime":     spanInfo.Timestamp,
			})
			if err == nil {
				traceSummary = summary
			}
		}
	}

	// If no root span found, use first span
	if traceSummary == nil && len(resp.Spans) > 0 {
		firstSpan := resp.Spans[0]
		summary, err := c.ParseTraceSummary(map[string]interface{}{
			"traceID":       traceID,
			"duration":      firstSpan.Duration,
			"serviceName":   firstSpan.ServiceName,
			"operationName": firstSpan.OperationName,
			"startTime":     firstSpan.Timestamp,
		})
		if err == nil {
			traceSummary = summary
		}
	}

	if traceSummary == nil {
		return nil, fmt.Errorf("unable to extract trace summary from response")
	}

	return &core.TraceDetails{
		TraceSummary: *traceSummary,
		Spans:       spans,
	}, nil
}

// ListApps lists ARMS-monitored applications.
func (c *AliyunARMSClient) ListApps(ctx context.Context) ([]map[string]interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, core.NewInternalError(string(c.Type()), "list_apps", fmt.Errorf("not connected"))
	}

	// Use SearchTraces with empty filter to list apps
	req := arms.CreateSearchTracesRequest()
	req.Pid = c.config.Pid
	req.StartTime = requests.NewInteger64(time.Now().Add(-5 * time.Minute).UnixMilli())
	req.EndTime = requests.NewInteger64(time.Now().UnixMilli())

	resp, err := c.armsClient.SearchTraces(req)
	if err != nil {
		return nil, err
	}

	// Extract unique service names from trace results
	serviceMap := make(map[string]bool)
	for _, trace := range resp.TraceInfos {
		serviceMap[trace.ServiceName] = true
	}

	// Convert to array
	result := make([]map[string]interface{}, 0, len(serviceMap))
	for service := range serviceMap {
		result = append(result, map[string]interface{}{
			"serviceName": service,
		})
	}

	return result, nil
}

// Close closes the ARMS client.
func (c *AliyunARMSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	c.healthStatus = core.StatusUnknown

	return nil
}
