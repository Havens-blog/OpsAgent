// Package log provides the Volcengine TLS (Tinder Log Service) data source.
//
// This mirrors the Alibaba Cloud SLS client (aliyun_sls.go) but speaks the
// Volcengine TLS dialect. Key differences from SLS, all confirmed against the
// official SDK (github.com/volcengine/volc-sdk-golang/service/tls):
//   - Constructor takes a region (TLS signs requests per-region).
//   - Logs are located by TopicID (the TLS analogue of an SLS logstore).
//   - Query times are Unix MILLISECONDS (SLS uses seconds).
//   - SearchLogs returns Logs as []map[string]interface{} and the total match
//     count as HitCount.
package log

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/datasource/core"
	"github.com/Havens-blog/OpsAgent/internal/datasource/credentials"

	tls "github.com/volcengine/volc-sdk-golang/service/tls"
)

// VolcengineTLSClient is the client for Volcengine TLS using the official SDK.
type VolcengineTLSClient struct {
	*BaseLogDataSource
	config    *core.VolcengineTLSConfig
	tlsClient tls.Client
	mu        sync.RWMutex
}

// NewVolcengineTLSClient creates a new Volcengine TLS client.
func NewVolcengineTLSClient(config *core.VolcengineTLSConfig) (*VolcengineTLSClient, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid TLS config: %w", err)
	}

	// NewClient(endpoint, accessKeyID, accessKeySecret, securityToken, region)
	tlsClient := tls.NewClient(
		config.Endpoint,
		config.AccessKeyID,
		config.AccessKeySecret,
		config.SecurityToken,
		config.Region,
	)

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

	return &VolcengineTLSClient{
		BaseLogDataSource: base,
		config:            config,
		tlsClient:         tlsClient,
	}, nil
}

// Connect establishes a connection to TLS by probing the configured topic.
func (c *VolcengineTLSClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := c.tlsClient.DescribeTopic(&tls.DescribeTopicRequest{TopicID: c.config.TopicID})
	if err != nil {
		return core.NewAuthError(string(c.Type()), "connect", fmt.Sprintf("failed to describe topic: %v", err), nil)
	}

	c.connected = true
	c.healthStatus = core.StatusHealthy
	c.lastHealthCheck = time.Now()
	return nil
}

// ping checks if TLS is reachable.
func (c *VolcengineTLSClient) ping(ctx context.Context) error {
	_, err := c.tlsClient.DescribeTopic(&tls.DescribeTopicRequest{TopicID: c.config.TopicID})
	return err
}

// Type returns the data source type.
func (c *VolcengineTLSClient) Type() core.DataSourceType {
	return core.DataSourceTypeTLS
}

// serviceField returns the configured service index field, defaulting to
// "service" when unset.
func (c *VolcengineTLSClient) serviceField() string {
	if c.config.ServiceField != "" {
		return c.config.ServiceField
	}
	return "service"
}

// levelField returns the configured log-level field name, defaulting to
// "level". Made configurable because the field is deployment-specific and not
// every log type has it (e.g. access logs), which otherwise makes an
// ErrorsOnly intent fail against TLS with a missing-key-index error.
func (c *VolcengineTLSClient) levelField() string {
	if c.config.LevelField != "" {
		return c.config.LevelField
	}
	return "level"
}

// buildQuery translates a request into the TLS search dialect. When Intent is
// set it is translated here (callers never embed TLS syntax); otherwise the
// raw Query is used verbatim. A non-empty raw Query is AND-combined with an
// Intent when both are present.
func (c *VolcengineTLSClient) buildQuery(req *core.LogQueryRequest) string {
	if req.Intent == nil {
		return req.Query
	}

	var clauses []string
	if req.Intent.ErrorsOnly {
		// TLS key-value search; the level field commonly holds ERROR/ERR.
		lf := c.levelField()
		clauses = append(clauses, fmt.Sprintf("(%s:ERROR OR %s:ERR)", lf, lf))
	}
	if req.Intent.ServiceName != "" {
		clauses = append(clauses, fmt.Sprintf("%s:%s", c.serviceField(), req.Intent.ServiceName))
	}
	if req.Query != "" {
		clauses = append(clauses, "("+req.Query+")")
	}

	if len(clauses) == 0 {
		// Intent present but empty: match everything (TLS treats empty Query
		// as "all logs in range").
		return ""
	}
	return joinClauses(clauses, " AND ")
}

// QueryLogs queries logs from TLS using the official SDK.
func (c *VolcengineTLSClient) QueryLogs(ctx context.Context, req *core.LogQueryRequest) (*core.LogQueryResponse, error) {
	if err := c.ValidateQueryRequest(req); err != nil {
		return nil, core.NewValidationError(string(c.Type()), "query_logs", err.Error())
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, core.NewInternalError(string(c.Type()), "query_logs", fmt.Errorf("not connected"))
	}

	c.ApplyDefaults(req)

	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		return c.executeSearch(req)
	})
	if err != nil {
		return nil, err
	}
	return result.(*core.LogQueryResponse), nil
}

// GetLogCount returns the count of logs matching the query.
func (c *VolcengineTLSClient) GetLogCount(ctx context.Context, req *core.LogQueryRequest) (int64, error) {
	if err := c.ValidateQueryRequest(req); err != nil {
		return 0, core.NewValidationError(string(c.Type()), "get_log_count", err.Error())
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return 0, core.NewInternalError(string(c.Type()), "get_log_count", fmt.Errorf("not connected"))
	}

	c.ApplyDefaults(req)

	result, err := c.ExecuteWithRetry(ctx, func() (interface{}, error) {
		// A minimal-limit search still returns HitCount = total matches.
		resp, e := c.search(req, 1)
		if e != nil {
			return int64(0), e
		}
		return int64(resp.HitCount), nil
	})
	if err != nil {
		return 0, err
	}
	return result.(int64), nil
}

// executeSearch runs a full search and converts the response.
func (c *VolcengineTLSClient) executeSearch(req *core.LogQueryRequest) (*core.LogQueryResponse, error) {
	resp, err := c.search(req, req.Limit)
	if err != nil {
		return nil, core.NewInternalError(string(c.Type()), "execute_search", fmt.Errorf("search failed: %w", err))
	}
	return c.convertSearchResponse(resp), nil
}

// search issues a SearchLogs call. TLS requires Unix MILLISECONDS and a
// TopicID; sort "desc" maps to the SDK's descending sort.
func (c *VolcengineTLSClient) search(req *core.LogQueryRequest, limit int) (*tls.SearchLogsResponse, error) {
	sort := "desc"
	if req.Sort != "" {
		sort = req.Sort
	}
	return c.tlsClient.SearchLogs(&tls.SearchLogsRequest{
		TopicID:   c.config.TopicID,
		Query:     c.buildQuery(req),
		StartTime: req.StartTime.UnixMilli(),
		EndTime:   req.EndTime.UnixMilli(),
		Limit:     limit,
		Sort:      sort,
	})
}

// convertSearchResponse converts the SDK response to our format.
func (c *VolcengineTLSClient) convertSearchResponse(resp *tls.SearchLogsResponse) *core.LogQueryResponse {
	out := &core.LogQueryResponse{
		Logs:       make([]core.LogEntry, 0, len(resp.Logs)),
		TotalCount: int64(resp.HitCount),
		HasMore:    !resp.ListOver,
	}

	for _, raw := range resp.Logs {
		// TLS stores the log time in the reserved "__time__" field (seconds).
		ts := parseLogTime(raw["__time__"])

		entry, err := c.ParseLogEntry(raw)
		if err != nil {
			continue
		}
		if !ts.IsZero() {
			entry.Timestamp = ts
		}
		// Resolve the service identifier from the configured field (TLS logs
		// in this cluster carry it in e.g. "document_type", not "service"), so
		// the entry reaches RCA attributed to a service.
		backfillSource(entry, c.serviceField())
		out.Logs = append(out.Logs, *entry)
	}

	return out
}

// parseLogTime extracts a timestamp from the TLS reserved "__time__" value.
// The SDK decodes the response with decoder.UseNumber(), so numeric fields
// arrive as json.Number (NOT float64/int64) — the earlier naive type switch
// missed this and silently fell back to time.Now(). This handles json.Number,
// float64, int64 and string forms, and normalizes millisecond values to
// seconds. Returns the zero time when the value is absent or unparseable, so
// the caller keeps whatever default it had.
func parseLogTime(v interface{}) time.Time {
	var sec int64
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return time.Time{}
		}
		sec = i
	case float64:
		sec = int64(n)
	case int64:
		sec = n
	case int:
		sec = int64(n)
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return time.Time{}
		}
		sec = i
	default:
		return time.Time{}
	}
	if sec <= 0 {
		return time.Time{}
	}
	// Normalize milliseconds (13-digit) to seconds.
	if sec >= 1e12 {
		return time.UnixMilli(sec)
	}
	return time.Unix(sec, 0)
}

// Close closes the TLS client.
func (c *VolcengineTLSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	c.healthStatus = core.StatusUnknown
	return nil
}
