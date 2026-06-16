// Package log provides Alibaba Cloud SLS data source implementation.
package log

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yourusername/OpsAgent/internal/datasource/core"
	"github.com/yourusername/OpsAgent/internal/datasource/credentials"
)

// AliyunSLSConfig represents the SLS-specific configuration.
type AliyunSLSConfig struct {
	*core.AliyunSLSConfig
}

// AliyunSLSClient is the client for Alibaba Cloud SLS.
type AliyunSLSClient struct {
	*BaseLogDataSource
	config *AliyunSLSConfig
	client *http.Client
	mu     sync.RWMutex
}

// NewAliyunSLSClient creates a new Alibaba Cloud SLS client.
func NewAliyunSLSClient(config *core.AliyunSLSConfig) (*AliyunSLSClient, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid SLS config: %w", err)
	}

	// Create credentials
	creds := &credentials.Credentials{
		Type:            credentials.CredentialTypeAccessKey,
		AccessKeyID:     config.AccessKeyID,
		AccessKeySecret: config.AccessKeySecret,
	}
	if config.SecurityToken != "" {
		creds.Type = credentials.CredentialTypeSTSToken
		creds.SecurityToken = config.SecurityToken
	}

	base := NewBaseLogDataSource(&config.Config)
	base.SetCredentials(creds)

	return &AliyunSLSClient{
		BaseLogDataSource: base,
		config:           &AliyunSLSConfig{AliyunSLSConfig: config},
		client: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

// Connect establishes a connection to SLS.
func (c *AliyunSLSClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Test connection by performing a simple list log stores operation
	req, err := c.buildRequest(ctx, "GET", fmt.Sprintf("/projects/%s/logstores", c.config.Project), nil)
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

// ping checks if SLS is reachable.
func (c *AliyunSLSClient) ping(ctx context.Context) error {
	// Simple ping by listing log stores
	req, err := c.buildRequest(ctx, "GET", fmt.Sprintf("/projects/%s/logstores", c.config.Project), nil)
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

// QueryLogs queries logs from SLS.
func (c *AliyunSLSClient) QueryLogs(ctx context.Context, req *core.LogQueryRequest) (*core.LogQueryResponse, error) {
	// Validate request
	if err := c.ValidateQueryRequest(req); err != nil {
		return nil, core.NewValidationError(string(c.Type()), "query_logs", err.Error())
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, core.NewInternalError(string(c.Type()), "query_logs", fmt.Errorf("not connected"))
	}

	// Apply defaults
	c.ApplyDefaults(req)

	// Build SLS query request
	slsReq := c.buildLogsQueryRequest(req)

	// Execute with retry
	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		return c.executeLogsQuery(ctx, slsReq)
	})

	if err != nil {
		return nil, err
	}

	return result.(*core.LogQueryResponse), nil
}

// GetLogCount returns the count of logs matching the query.
func (c *AliyunSLSClient) GetLogCount(ctx context.Context, req *core.LogQueryRequest) (int64, error) {
	// Validate request
	if err := c.ValidateQueryRequest(req); err != nil {
		return 0, core.NewValidationError(string(c.Type()), "get_log_count", err.Error())
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return 0, core.NewInternalError(string(c.Type()), "get_log_count", fmt.Errorf("not connected"))
	}

	// For SLS, we can get count from the histogram API
	slsReq := c.buildHistogramRequest(req)

	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		return c.executeHistogramQuery(ctx, slsReq)
	})

	if err != nil {
		return 0, err
	}

	return result.(int64), nil
}

// Type returns the data source type.
func (c *AliyunSLSClient) Type() core.DataSourceType {
	return core.DataSourceTypeSLS
}

// buildRequest builds an HTTP request for SLS API.
func (c *AliyunSLSClient) buildRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	// Construct full URL
	fullURL := c.config.Endpoint + path

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, err
	}

	// Add headers
	req.Header.Set("x-log-signaturemethod", "hmac-sha1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Add authentication headers
	if err := c.signRequest(req); err != nil {
		return nil, err
	}

	return req, nil
}

// signRequest signs the request with Alibaba Cloud signature.
func (c *AliyunSLSClient) signRequest(req *http.Request) error {
	creds := c.GetCredentials()
	if creds == nil || !creds.IsValid() {
		return fmt.Errorf("invalid credentials")
	}

	// Add common headers
	req.Header.Set("x-log-apiversion", "0.6.0")
	req.Header.Set("x-log-appid", "opsagent")

	// Sign the request (simplified - actual SLS signing is more complex)
	// In production, use the official SDK or implement proper HMAC-SHA1 signing

	return nil
}

// slsLogsQueryRequest represents an SLS logs query request.
type slsLogsQueryRequest struct {
	Project     string                 `json:"project"`
	LogStore    string                 `json:"logstore"`
	From        int64                  `json:"from"`
	To          int64                  `json:"to"`
	Query       string                 `json:"query"`
	Limit       int                    `json:"limit"`
	Offset      int                    `json:"offset,omitempty"`
	Line        bool                   `json:"line"`
	Reverse     bool                   `json:"reverse"`
	PowerSQL    bool                   `json:"powerSql"`
	ProgressIn  bool                   `json:"progressIn"`
	Format      string                 `json:"format,omitempty"`
	Filter      map[string]interface{} `json:"filter,omitempty"`
}

// buildLogsQueryRequest builds an SLS logs query request.
func (c *AliyunSLSClient) buildLogsQueryRequest(req *core.LogQueryRequest) *slsLogsQueryRequest {
	return &slsLogsQueryRequest{
		Project:  c.config.Project,
		LogStore: c.config.LogStore,
		From:     req.StartTime.Unix(),
		To:       req.EndTime.Unix(),
		Query:    req.Query,
		Limit:    req.Limit,
		Offset:   req.Offset,
		Reverse:  req.Sort == "desc",
		Line:     true,
		Format:   "json",
	}
}

// executeLogsQuery executes an SLS logs query.
func (c *AliyunSLSClient) executeLogsQuery(ctx context.Context, slsReq *slsLogsQueryRequest) (*core.LogQueryResponse, error) {
	path := fmt.Sprintf("/projects/%s/logstores/%s?type=log", slsReq.Project, slsReq.LogStore)

	body, err := json.Marshal(slsReq)
	if err != nil {
		return nil, core.NewInternalError(string(c.Type()), "execute_logs_query", err)
	}

	req, err := c.buildRequest(ctx, "POST", path, strings.NewReader(string(body)))
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "execute_logs_query", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "execute_logs_query", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, core.NewInternalError(string(c.Type()), "execute_logs_query", fmt.Errorf("query failed with status %d: %s", resp.StatusCode, string(body)))
	}

	// Parse response
	var slsResp slsLogsQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&slsResp); err != nil {
		return nil, core.NewParsingError(string(c.Type()), "execute_logs_query", "failed to parse response", err)
	}

	// Convert to our format
	return c.convertSLSResponse(&slsResp)
}

// slsLogsQueryResponse represents an SLS logs query response.
type slsLogsQueryResponse struct {
	TotalCount   int64            `json:"totalCount"`
	HasMore      bool             `json:"hasMore"`
	Logs         []json.RawMessage `json:"logs"`
	Count        int              `json:"count"`
	ProcessTime  float64          `json:"processTime"`
}

// convertSLSResponse converts SLS response to our format.
func (c *AliyunSLSClient) convertSLSResponse(slsResp *slsLogsQueryResponse) (*core.LogQueryResponse, error) {
	response := &core.LogQueryResponse{
		Logs:       make([]core.LogEntry, 0, len(slsResp.Logs)),
		TotalCount: slsResp.TotalCount,
		HasMore:    slsResp.HasMore,
	}

	for _, rawLog := range slsResp.Logs {
		var logMap map[string]interface{}
		if err := json.Unmarshal(rawLog, &logMap); err != nil {
			continue
		}

		entry, err := c.ParseLogEntry(logMap)
		if err != nil {
			continue
		}

		response.Logs = append(response.Logs, *entry)
	}

	return response, nil
}

// slsHistogramRequest represents an SLS histogram request.
type slsHistogramRequest struct {
	Project  string `json:"project"`
	LogStore string `json:"logstore"`
	From     int64  `json:"from"`
	To       int64  `json:"to"`
	Query    string `json:"query"`
	Interval string `json:"interval,omitempty"`
}

// buildHistogramRequest builds an SLS histogram request.
func (c *AliyunSLSClient) buildHistogramRequest(req *core.LogQueryRequest) *slsHistogramRequest {
	return &slsHistogramRequest{
		Project:  c.config.Project,
		LogStore: c.config.LogStore,
		From:     req.StartTime.Unix(),
		To:       req.EndTime.Unix(),
		Query:    req.Query,
	}
}

// executeHistogramQuery executes an SLS histogram query.
func (c *AliyunSLSClient) executeHistogramQuery(ctx context.Context, slsReq *slsHistogramRequest) (int64, error) {
	path := fmt.Sprintf("/projects/%s/logstores/%s?type=histogram", slsReq.Project, slsReq.LogStore)

	body, err := json.Marshal(slsReq)
	if err != nil {
		return 0, core.NewInternalError(string(c.Type()), "execute_histogram_query", err)
	}

	req, err := c.buildRequest(ctx, "POST", path, strings.NewReader(string(body)))
	if err != nil {
		return 0, core.NewNetworkError(string(c.Type()), "execute_histogram_query", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, core.NewNetworkError(string(c.Type()), "execute_histogram_query", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, core.NewInternalError(string(c.Type()), "execute_histogram_query", fmt.Errorf("query failed with status %d: %s", resp.StatusCode, string(body)))
	}

	// Parse response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, core.NewParsingError(string(c.Type()), "execute_histogram_query", "failed to parse response", err)
	}

	// Extract total count from histogram
	if count, ok := result["total_count"].(float64); ok {
		return int64(count), nil
	}

	if count, ok := result["count"].(float64); ok {
		return int64(count), nil
	}

	return 0, fmt.Errorf("failed to extract count from histogram response")
}

// GetLogStores returns a list of log stores for the configured project.
func (c *AliyunSLSClient) GetLogStores(ctx context.Context) ([]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	req, err := c.buildRequest(ctx, "GET", fmt.Sprintf("/projects/%s/logstores", c.config.Project), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "get_logstores", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, core.NewInternalError(string(c.Type()), "get_logstores", fmt.Errorf("failed with status %d", resp.StatusCode))
	}

	var result struct {
		LogStores []string `json:"logstores"`
		Count     int      `json:"count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, core.NewParsingError(string(c.Type()), "get_logstores", "failed to parse response", err)
	}

	return result.LogStores, nil
}

// GetIndexFields returns the index fields for a log store.
func (c *AliyunSLSClient) GetIndexFields(ctx context.Context, logStore string) (map[string]interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	path := fmt.Sprintf("/projects/%s/logstores/%s/index", c.config.Project, logStore)
	req, err := c.buildRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, core.NewNetworkError(string(c.Type()), "get_index_fields", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, core.NewInternalError(string(c.Type()), "get_index_fields", fmt.Errorf("failed with status %d", resp.StatusCode))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, core.NewParsingError(string(c.Type()), "get_index_fields", "failed to parse response", err)
	}

	return result, nil
}

// buildURL builds a URL with query parameters.
func (c *AliyunSLSClient) buildURL(basePath string, params map[string]string) string {
	u, _ := url.Parse(basePath)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Close closes the SLS client.
func (c *AliyunSLSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	c.healthStatus = core.StatusUnknown
	c.client.CloseIdleConnections()

	return nil
}
