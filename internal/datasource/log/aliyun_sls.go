// Package log provides Alibaba Cloud SLS data source implementation.
package log

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/datasource/core"
	"github.com/Havens-blog/OpsAgent/internal/datasource/credentials"

	sls "github.com/aliyun/aliyun-log-go-sdk"
)

// AliyunSLSConfig represents the SLS-specific configuration.
type AliyunSLSConfig struct {
	*core.AliyunSLSConfig
}

// AliyunSLSClient is the client for Alibaba Cloud SLS using the official SDK.
type AliyunSLSClient struct {
	*BaseLogDataSource
	config    *AliyunSLSConfig
	slsClient sls.ClientInterface
	mu        sync.RWMutex
}

// NewAliyunSLSClient creates a new Alibaba Cloud SLS client using the official SDK.
func NewAliyunSLSClient(config *core.AliyunSLSConfig) (*AliyunSLSClient, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid SLS config: %w", err)
	}

	// Create SLS client using official SDK
	slsClient := sls.CreateNormalInterface(
		config.Endpoint,
		config.AccessKeyID,
		config.AccessKeySecret,
		config.SecurityToken,
	)

	// Create credentials for base datasource
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
		slsClient:        slsClient,
	}, nil
}

// Connect establishes a connection to SLS.
func (c *AliyunSLSClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Test connection by getting logstore info
	_, err := c.slsClient.GetLogStore(c.config.Project, c.config.LogStore)
	if err != nil {
		return core.NewAuthError(string(c.Type()), "connect", fmt.Sprintf("failed to get logstore: %v", err), nil)
	}

	c.connected = true
	c.healthStatus = core.StatusHealthy
	c.lastHealthCheck = time.Now()

	return nil
}

// ping checks if SLS is reachable.
func (c *AliyunSLSClient) ping(ctx context.Context) error {
	_, err := c.slsClient.GetLogStore(c.config.Project, c.config.LogStore)
	return err
}

// QueryLogs queries logs from SLS using the official SDK.
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

	// Execute with retry
	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		return c.executeLogsQuery(req)
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

	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		return c.executeHistogramQuery(req)
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

// serviceField returns the configured service index field, defaulting to
// "service" when unset.
func (c *AliyunSLSClient) serviceField() string {
	if c.config.ServiceField != "" {
		return c.config.ServiceField
	}
	return "service"
}

// levelField returns the configured log-level field name, defaulting to
// "level". Made configurable because the field is deployment-specific and not
// every log type has it (e.g. access logs), which otherwise makes an
// ErrorsOnly intent fail against SLS with a missing-key-index error.
func (c *AliyunSLSClient) levelField() string {
	if c.config.LevelField != "" {
		return c.config.LevelField
	}
	return "level"
}

// buildQuery resolves the effective SLS search string for a request. When the
// request carries a structured Intent, it is translated into SLS dialect here
// (so callers never embed SLS-specific syntax); otherwise the raw Query is
// used verbatim. A non-empty raw Query is AND-combined with an Intent when
// both are present, letting callers add an extra constraint on top of intent.
func (c *AliyunSLSClient) buildQuery(req *core.LogQueryRequest) string {
	if req.Intent == nil {
		return req.Query
	}

	var clauses []string
	if req.Intent.ErrorsOnly {
		// The level field commonly holds ERROR/ERR; match either.
		lf := c.levelField()
		clauses = append(clauses, fmt.Sprintf("(%s: ERROR OR %s: ERR)", lf, lf))
	}
	if req.Intent.ServiceName != "" {
		clauses = append(clauses, fmt.Sprintf("%s: %s", c.serviceField(), req.Intent.ServiceName))
	}
	if req.Query != "" {
		clauses = append(clauses, "("+req.Query+")")
	}

	if len(clauses) == 0 {
		// Intent present but empty: match everything in the time range.
		return ""
	}
	return joinClauses(clauses, " AND ")
}

// joinClauses joins non-empty clauses with the given separator.
func joinClauses(clauses []string, sep string) string {
	out := ""
	for i, cl := range clauses {
		if i > 0 {
			out += sep
		}
		out += cl
	}
	return out
}

// executeLogsQuery executes a log query using the official SDK.
func (c *AliyunSLSClient) executeLogsQuery(req *core.LogQueryRequest) (*core.LogQueryResponse, error) {
	// Use GetLogs from SDK
	// topic is empty (legacy, now use __topic__ in query)
	resp, err := c.slsClient.GetLogs(
		c.config.Project,
		c.config.LogStore,
		"", // topic - use __topic__ in query instead
		req.StartTime.Unix(),
		req.EndTime.Unix(),
		c.buildQuery(req),
		int64(req.Limit),
		int64(req.Offset),
		req.Sort == "desc", // reverse
	)
	if err != nil {
		return nil, core.NewInternalError(string(c.Type()), "execute_logs_query", fmt.Errorf("query failed: %w", err))
	}

	// Convert to our format
	return c.convertSDKLogsResponse(resp)
}

// convertSDKLogsResponse converts SDK response to our format.
func (c *AliyunSLSClient) convertSDKLogsResponse(resp *sls.GetLogsResponse) (*core.LogQueryResponse, error) {
	response := &core.LogQueryResponse{
		Logs:       make([]core.LogEntry, 0),
		TotalCount: resp.Count,
		HasMore:    resp.Progress != "Complete",
	}

	// SDK returns logs as []map[string]string
	for _, logMap := range resp.Logs {
		// Parse timestamp if present
		var timestamp time.Time
		if timeStr, ok := logMap["time"]; ok {
			if sec, err := strconv.ParseInt(timeStr, 10, 64); err == nil {
				timestamp = time.Unix(sec, 0)
			}
		}

		// Convert map[string]string to map[string]interface{}
		logData := make(map[string]interface{})
		for k, v := range logMap {
			logData[k] = v
		}

		entry, err := c.ParseLogEntry(logData)
		if err != nil {
			continue
		}

		// Override timestamp if extracted
		if !timestamp.IsZero() {
			entry.Timestamp = timestamp
		}

		// Resolve the service identifier from the configured field when the
		// parser could not fill it from a "source" key, so the entry reaches
		// RCA attributed to a service rather than an empty bucket.
		backfillSource(entry, c.serviceField())

		response.Logs = append(response.Logs, *entry)
	}

	return response, nil
}

// executeHistogramQuery executes a histogram query using the official SDK.
func (c *AliyunSLSClient) executeHistogramQuery(req *core.LogQueryRequest) (int64, error) {
	// Use GetHistograms from SDK
	// topic is empty (legacy, now use __topic__ in query)
	resp, err := c.slsClient.GetHistograms(
		c.config.Project,
		c.config.LogStore,
		"", // topic - use __topic__ in query instead
		req.StartTime.Unix(),
		req.EndTime.Unix(),
		c.buildQuery(req),
	)
	if err != nil {
		return 0, core.NewInternalError(string(c.Type()), "execute_histogram_query", fmt.Errorf("histogram query failed: %w", err))
	}

	// Get total count from histogram response
	totalCount := int64(0)
	for _, histogram := range resp.Histograms {
		totalCount += histogram.Count
	}

	// If count is provided directly, use it
	if resp.Count > 0 && resp.Progress == "Complete" {
		totalCount = resp.Count
	}

	return totalCount, nil
}

// GetLogStores returns a list of log stores for the configured project.
func (c *AliyunSLSClient) GetLogStores(ctx context.Context) ([]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, core.NewInternalError(string(c.Type()), "get_logstores", fmt.Errorf("not connected"))
	}

	// Use ListLogStoreV2 (note: singular name in SDK)
	logStores, err := c.slsClient.ListLogStoreV2(c.config.Project, 0, 100, "")
	if err != nil {
		return nil, core.NewInternalError(string(c.Type()), "get_logstores", err)
	}

	return logStores, nil
}

// GetIndexFields returns the index fields for a log store.
func (c *AliyunSLSClient) GetIndexFields(ctx context.Context, logStore string) (map[string]interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, core.NewInternalError(string(c.Type()), "get_index_fields", fmt.Errorf("not connected"))
	}

	index, err := c.slsClient.GetIndex(c.config.Project, logStore)
	if err != nil {
		return nil, core.NewInternalError(string(c.Type()), "get_index_fields", err)
	}

	// Convert index to map format
	result := make(map[string]interface{})
	if index.Keys != nil {
		for key, value := range index.Keys {
			result[key] = map[string]interface{}{
				"type":          value.Type,
				"alias":         value.Alias,
				"doc_value":     value.DocValue,
				"case_sensitive": value.CaseSensitive,
				"token":         value.Token,
			}
		}
	}

	return result, nil
}

// Close closes the SLS client.
func (c *AliyunSLSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	c.healthStatus = core.StatusUnknown

	return nil
}
