// Package sources provides data source interfaces for RCA analysis.
package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// ECamLogSource consumes the e-cam multi-cloud log federation
// (/api/v1/cam/logs/search + /aggregate) instead of talking to any single
// cloud SDK. The "app" log type there maps application business logs
// (<service>-business SLS stores) into the contract fields
// service/level/message/timestamp, which is exactly what RCA needs — and the
// federation fans out across every configured cloud account for us.
//
// Honesty rules (same contract as the other real sources):
//   - HTTP/transport failure or code != 0 => error, no fabricated data;
//   - an empty result set is returned as an empty slice with nil error —
//     "no logs" is a legitimate answer, not a failure;
//   - the bearer token is sent per request and never logged.
type ECamLogSource struct {
	endpoint      string // e.g. http://127.0.0.1:8001 (no trailing slash)
	token         string // bearer JWT (static fallback when no token provider)
	tokenProvider TokenProvider
	pathPrefix    string // 老会话路径 /api/v1/cam/logs;配 S2S 后 /api/s2s/logs
	logType       string // federation log type; default "app"
	client        *http.Client
}

// NewECamLogSource builds an e-cam federation log source. logType may be empty
// (defaults to "app"); timeout <= 0 defaults to 30s.
func NewECamLogSource(endpoint, token, logType string, timeout time.Duration) *ECamLogSource {
	if logType == "" {
		logType = "app"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &ECamLogSource{
		endpoint:   strings.TrimRight(endpoint, "/"),
		token:      token,
		pathPrefix: "/api/v1/cam/logs",
		logType:    logType,
		client:     &http.Client{Timeout: timeout},
	}
}

// WithTokenProvider 注入 S2S 令牌换取器（client_credentials）。
// 未注入时回退静态 token（向后兼容 mktok 会话令牌）。
func (s *ECamLogSource) WithTokenProvider(p TokenProvider) *ECamLogSource {
	s.tokenProvider = p
	// S2S 令牌走独立验签路由组；路径与鉴权方式必须同步切换，避免错位 401
	s.pathPrefix = "/api/s2s/logs"
	return s
}

// bearerToken 返回本次请求的 Bearer 令牌：优先 tokenProvider 换取，否则静态 token。
func (s *ECamLogSource) bearerToken(ctx context.Context) (string, error) {
	if s.tokenProvider != nil {
		return s.tokenProvider.TokenFor(ctx)
	}
	return s.token, nil
}

// ecamFieldFilter mirrors the e-cam search filter wire format.
type ecamFieldFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// ecamSearchRequest is the /api/v1/cam/logs/search body. Timestamps are Unix
// MILLISECONDS (the federation passes them through to the SDK unchanged).
type ecamSearchRequest struct {
	LogType   string            `json:"log_type"`
	StartTime int64             `json:"start_time"`
	EndTime   int64             `json:"end_time"`
	Query     string            `json:"query"`
	Filters   []ecamFieldFilter `json:"filters,omitempty"`
	Limit     int               `json:"limit"`
}

// ecamLogEntry is the subset of the federation entry shape RCA consumes.
// The app type guarantees service/level/message/timestamp; exception/trace_id
// and meta are present for application logs but may be empty (missing is
// carried through as missing — never invented).
type ecamLogEntry struct {
	Timestamp int64          `json:"timestamp"` // Unix ms UTC
	Service   string         `json:"service"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Exception string         `json:"exception"`
	TraceID   string         `json:"trace_id"`
	Meta      ecamEntryMeta  `json:"meta"`
	Raw       map[string]any `json:"raw,omitempty"`
}

// ecamEntryMeta identifies the originating store/cloud.
type ecamEntryMeta struct {
	Cloud       string `json:"cloud"`
	AccountName string `json:"account_name"`
	Region      string `json:"region"`
	Source      string `json:"source"`
}

// ecamSearchData is the search response payload.
type ecamSearchData struct {
	Total   int64          `json:"total"`
	Entries []ecamLogEntry `json:"entries"`
}

// ecamAggregateRequest is the /api/v1/cam/logs/aggregate body (count metric).
type ecamAggregateRequest struct {
	LogType   string            `json:"log_type"`
	StartTime int64             `json:"start_time"`
	EndTime   int64             `json:"end_time"`
	Filters   []ecamFieldFilter `json:"filters,omitempty"`
	Metric    string            `json:"metric"`
}

// ecamAggregateData is the aggregate response payload (Total = exact count
// pushed down to the source query engine).
type ecamAggregateData struct {
	Total   int64 `json:"total"`
	Buckets []struct {
		Timestamp int64 `json:"timestamp"`
		Count     int64 `json:"count"`
	} `json:"buckets"`
}

// ecamResponse is the common e-cam envelope: {code:0, data:{...}}.
type ecamResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// ecamQueryLimit caps entries fetched per RCA query. The federation hard-caps
// at 1000 merged entries; RCA consumes a bounded sample per analysis, so a
// few hundred per source is plenty.
const ecamQueryLimit = 300

// QueryErrors implements LogSource: error-level application logs in the window.
func (s *ECamLogSource) QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
	return s.search(ctx, ecamSearchRequest{
		LogType:   s.logType,
		StartTime: start.UnixMilli(),
		EndTime:   end.UnixMilli(),
		Query:     "*",
		Filters:   []ecamFieldFilter{{Field: "level", Op: "eq", Value: "error"}},
		Limit:     ecamQueryLimit,
	})
}

// QueryLogsByService implements LogSource: logs of one service in the window.
func (s *ECamLogSource) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	if strings.TrimSpace(service) == "" {
		return nil, fmt.Errorf("ecam log source: empty service name")
	}
	return s.search(ctx, ecamSearchRequest{
		LogType:   s.logType,
		StartTime: start.UnixMilli(),
		EndTime:   end.UnixMilli(),
		Query:     "*",
		Filters:   []ecamFieldFilter{{Field: "service", Op: "eq", Value: service}},
		Limit:     ecamQueryLimit,
	})
}

// GetLogCount implements LogSource: exact window count for a service, pushed
// down via the federation aggregate (SQL count at the source, unaffected by
// sampling limits). If the aggregate endpoint fails, falls back to counting a
// search page — that fallback is a lower bound (capped by ecamQueryLimit),
// which is still honest: fewer logs than reality, never more.
func (s *ECamLogSource) GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error) {
	if strings.TrimSpace(service) == "" {
		return 0, fmt.Errorf("ecam log source: empty service name")
	}
	body := ecamAggregateRequest{
		LogType:   s.logType,
		StartTime: start.UnixMilli(),
		EndTime:   end.UnixMilli(),
		Filters:   []ecamFieldFilter{{Field: "service", Op: "eq", Value: service}},
		Metric:    "count",
	}
	var data ecamAggregateData
	err := s.post(ctx, s.pathPrefix+"/aggregate", body, &data)
	if err == nil {
		return data.Total, nil
	}
	// Aggregate unavailable (endpoint/permission/engine error): fall back to
	// a bounded search count instead of failing the whole RCA log dimension.
	entries, searchErr := s.QueryLogsByService(ctx, service, start, end)
	if searchErr != nil {
		return 0, fmt.Errorf("ecam log source: aggregate failed (%v) and search fallback failed: %w", err, searchErr)
	}
	return int64(len(entries)), nil
}

// Ping probes the federation for reachability (pinger interface): the types
// endpoint is the cheapest authenticated call and doubles as a token check.
func (s *ECamLogSource) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint+s.pathPrefix+"/types", nil)
	if err != nil {
		return fmt.Errorf("ecam log source: build request: %w", err)
	}
	tok, err := s.bearerToken(ctx)
	if err != nil {
		return fmt.Errorf("ecam log source: get token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ecam log source: ping %s: %w", s.endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ecam log source: ping status %d", resp.StatusCode)
	}
	var env ecamResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("ecam log source: ping decode: %w", err)
	}
	if env.Code != 0 {
		return fmt.Errorf("ecam log source: ping code %d: %s", env.Code, env.Message)
	}
	return nil
}

// search posts a search request and maps entries into RCA models.
func (s *ECamLogSource) search(ctx context.Context, body ecamSearchRequest) ([]models.LogEntry, error) {
	var data ecamSearchData
	if err := s.post(ctx, s.pathPrefix+"/search", body, &data); err != nil {
		return nil, err
	}
	out := make([]models.LogEntry, 0, len(data.Entries))
	for _, e := range data.Entries {
		out = append(out, s.mapEntry(e))
	}
	return out, nil
}

// post performs one authenticated federation call and decodes data into out.
func (s *ECamLogSource) post(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("ecam log source: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("ecam log source: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	tok, err := s.bearerToken(ctx)
	if err != nil {
		return fmt.Errorf("ecam log source: get token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ecam log source: POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Drain a bounded prefix for the error message; never dump the whole
		// body (it may echo request data) and never the token.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("ecam log source: POST %s status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	var env ecamResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("ecam log source: POST %s decode: %w", path, err)
	}
	if env.Code != 0 {
		return fmt.Errorf("ecam log source: POST %s code %d: %s", path, env.Code, env.Message)
	}
	if len(env.Data) == 0 {
		return nil // honest empty: code 0 with no payload
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("ecam log source: POST %s data decode: %w", path, err)
	}
	return nil
}

// mapEntry converts a federation entry into the RCA log model. Missing
// fields stay missing; error marking uses the normalized lowercase level.
func (s *ECamLogSource) mapEntry(e ecamLogEntry) models.LogEntry {
	labels := map[string]string{}
	if e.Meta.Source != "" {
		labels["source"] = e.Meta.Source
	} else if e.Meta.Region != "" || e.Meta.Cloud != "" {
		labels["source"] = strings.Join(nonEmpty(e.Meta.Cloud, e.Meta.Region), "/")
	}
	if e.Exception != "" {
		labels["exception"] = e.Exception
	}
	return models.LogEntry{
		Timestamp: time.UnixMilli(e.Timestamp),
		Service:   e.Service,
		Level:     strings.ToLower(strings.TrimSpace(e.Level)),
		Message:   e.Message,
		TraceID:   e.TraceID,
		Labels:    labels,
		IsError:   strings.EqualFold(strings.TrimSpace(e.Level), "error"),
	}
}

// nonEmpty filters out empty strings (order-preserving).
func nonEmpty(vals ...string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
