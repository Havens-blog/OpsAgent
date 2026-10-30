// Package sources provides data source interfaces for RCA analysis.
package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
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
// Resources scopes the query to specific federation resources (for app logs:
// store names) — a store-level scope is far cheaper than a field filter,
// which fans out over every store.
type ecamSearchRequest struct {
	LogType   string            `json:"log_type"`
	StartTime int64             `json:"start_time"`
	EndTime   int64             `json:"end_time"`
	Query     string            `json:"query"`
	Filters   []ecamFieldFilter `json:"filters,omitempty"`
	Resources []string          `json:"resources,omitempty"`
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

// ecamSearchData is the search response payload. Truncated marks that the
// federation merge cap cut the result set — counts derived from a truncated
// page under-report, so it is surfaced instead of silently ignored.
type ecamSearchData struct {
	Total     int64          `json:"total"`
	Entries   []ecamLogEntry `json:"entries"`
	Truncated bool           `json:"truncated"`
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

// appStoreSuffix is the federation's app-log store naming: one SLS store per
// service, "<service>-business". Scoping a query with resources=[store] hits
// that single store (~0.7s measured); a service field FILTER instead fans out
// over all ~800 stores (~26s+) because filters don't select stores.
const appStoreSuffix = "-business"

// serviceRequest builds a store-scoped search for one service. errorsOnly adds
// the level=error pushdown. Non-app log types have no per-service stores, so
// they keep the filter-based scoping.
func (s *ECamLogSource) serviceRequest(service string, start, end time.Time, errorsOnly bool) ecamSearchRequest {
	req := ecamSearchRequest{
		LogType:   s.logType,
		StartTime: start.UnixMilli(),
		EndTime:   end.UnixMilli(),
		Query:     "*",
		Limit:     ecamQueryLimit,
	}
	if errorsOnly {
		req.Filters = append(req.Filters, ecamFieldFilter{Field: "level", Op: "eq", Value: "error"})
	}
	if s.logType == "app" {
		req.Resources = []string{service + appStoreSuffix}
	} else {
		req.Filters = append(req.Filters, ecamFieldFilter{Field: "service", Op: "eq", Value: service})
	}
	return req
}

// QueryErrorsByService implements LogSource: error-level logs of one service,
// store-scoped (see appStoreSuffix).
func (s *ECamLogSource) QueryErrorsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	if strings.TrimSpace(service) == "" {
		return nil, fmt.Errorf("ecam log source: empty service name")
	}
	return s.search(ctx, s.serviceRequest(service, start, end, true))
}

// QueryLogsByService implements LogSource: logs of one service in the window,
// store-scoped (see appStoreSuffix).
func (s *ECamLogSource) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	if strings.TrimSpace(service) == "" {
		return nil, fmt.Errorf("ecam log source: empty service name")
	}
	return s.search(ctx, s.serviceRequest(service, start, end, false))
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
	if data.Truncated {
		log.Printf("[ecam-log-source] WARN: search truncated at the federation merge cap (total=%d, returned=%d); error counts may under-report", data.Total, len(data.Entries))
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

// ecamSLBEntry is the SLB/ALB access-log entry subset that maps a host to its
// backend: target_ip (upstream) and raw.server_group_name (which names the k8s
// Service when the backend is a cluster workload).
type ecamSLBEntry struct {
	Host       string         `json:"host"`
	TargetIP   string         `json:"target_ip"`
	TargetPort int            `json:"target_port"`
	Status     int            `json:"status"`
	Raw        map[string]any `json:"raw"`
}

// SLBBackend is one resolved backend of a domain from the SLB access log.
type SLBBackend struct {
	Domain   string `json:"domain"`
	TargetIP string `json:"target_ip"`
	Service  string `json:"service"` // k8s Service name from server_group_name
	Port     int    `json:"port"`
}

// DomainBackendResolver resolves a host to its backend k8s services. The RCA
// engine uses it to anchor a domain-scoped (dial-test) alarm on the real
// backend instead of a business-unit token guess.
type DomainBackendResolver interface {
	QueryDomainBackends(ctx context.Context, domain string, start, end time.Time) ([]SLBBackend, error)
}

// SLBEdgeResolver returns window-scoped SLB access-log facts for a domain:
// total requests, sampled 5xx count, and the resolved backends. The engine
// renders these as edge reference facts (never root-cause candidates) so a
// dial-test "unavailable rate" alarm can show whether the SLB actually
// forwarded 5xx in the window, instead of leaving "edge suspicion" unsupported.
type SLBEdgeResolver interface {
	QueryDomainSLB(ctx context.Context, domain string, start, end time.Time) (*models.SLBEdgeFacts, error)
}

// CDNEdgeResolver returns window-scoped CDN facts for a domain: the CMDB CDN
// hop (provider/CNAME/origin) plus the e-cam CDN access-log edge-5xx count. The
// engine renders these as edge reference facts so a dial-test alarm can show
// the CDN hop and whether its logs are even collected (Total == 0 means no
// data, stated honestly rather than passed off as "CDN healthy").
type CDNEdgeResolver interface {
	QueryDomainCDN(ctx context.Context, domain string, start, end time.Time) (*models.CDNEdgeFacts, error)
}

// QueryDomainBackends resolves a domain to its backend services by reading the
// SLB access log: host -> server_group_name -> "<service>-svc" (and the target
// IP). An empty result is honest "no recorded traffic", not an error.
func (s *ECamLogSource) QueryDomainBackends(ctx context.Context, domain string, start, end time.Time) ([]SLBBackend, error) {
	var data struct {
		Total     int64          `json:"total"`
		Entries   []ecamSLBEntry `json:"entries"`
		Truncated bool           `json:"truncated"`
	}
	body := ecamSearchRequest{
		LogType:   "slb",
		StartTime: start.UnixMilli(),
		EndTime:   end.UnixMilli(),
		Query:     "*",
		Filters:   []ecamFieldFilter{{Field: "host", Op: "contains", Value: domain}},
		Limit:     200,
	}
	if err := s.post(ctx, s.pathPrefix+"/search", body, &data); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	out := make([]SLBBackend, 0, 4)
	for _, e := range data.Entries {
		svc, port := parseServerGroup(strAttr(e.Raw, "server_group_name"))
		if svc == "" && e.TargetIP == "" {
			continue
		}
		key := svc + "|" + e.TargetIP
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, SLBBackend{Domain: domain, TargetIP: e.TargetIP, Service: svc, Port: port})
	}
	return out, nil
}

// QueryDomainSLB returns the domain's SLB/ALB access-log facts for the window:
// total matched requests, the sampled 5xx count, and the resolved backends. It
// reads the same slb store as QueryDomainBackends but with a larger sample so
// the 5xx ratio is meaningful; the two share the host-contains filter.
func (s *ECamLogSource) QueryDomainSLB(ctx context.Context, domain string, start, end time.Time) (*models.SLBEdgeFacts, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return nil, fmt.Errorf("ecam log source: empty domain")
	}
	const sampleLimit = 500
	var data struct {
		Total     int64          `json:"total"`
		Entries   []ecamSLBEntry `json:"entries"`
		Truncated bool           `json:"truncated"`
	}
	body := ecamSearchRequest{
		LogType:   "slb",
		StartTime: start.UnixMilli(),
		EndTime:   end.UnixMilli(),
		Query:     "*",
		Filters:   []ecamFieldFilter{{Field: "host", Op: "contains", Value: domain}},
		Limit:     sampleLimit,
	}
	if err := s.post(ctx, s.pathPrefix+"/search", body, &data); err != nil {
		return nil, err
	}

	facts := &models.SLBEdgeFacts{
		Total:      data.Total,
		SampleSize: len(data.Entries),
	}
	seen := map[string]bool{}
	for _, e := range data.Entries {
		if e.Status >= 500 && e.Status < 600 {
			facts.Status5xx++
		}
		svc, port := parseServerGroup(strAttr(e.Raw, "server_group_name"))
		if svc == "" && e.TargetIP == "" {
			continue
		}
		key := svc + "|" + e.TargetIP
		if seen[key] {
			continue
		}
		seen[key] = true
		facts.Backends = append(facts.Backends, models.EdgeBackend{
			Service:  svc,
			TargetIP: e.TargetIP,
			Port:     port,
		})
	}
	return facts, nil
}

// ecamCDNEntry is the subset of the CDN/DCDN access-log entry the edge section
// needs: the edge response status. DCDN realtime logs carry no origin status
// field, so only the edge 5xx is derivable — reported as such, never invented.
type ecamCDNEntry struct {
	Status int `json:"status"`
}

// QueryDomainCDNLog returns the domain's CDN access-log totals for the window:
// federation match count, the sampled edge-5xx count, and the sample size. The
// CDN federation is per-domain configured, so a domain whose collection is not
// enabled comes back 0 with no error — the caller must distinguish "no traffic"
// from "not collected" when reporting.
func (s *ECamLogSource) QueryDomainCDNLog(ctx context.Context, domain string, start, end time.Time) (total int64, status5xx int, sampleSize int, err error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return 0, 0, 0, fmt.Errorf("ecam log source: empty domain")
	}
	const sampleLimit = 500
	var data struct {
		Total     int64          `json:"total"`
		Entries   []ecamCDNEntry `json:"entries"`
		Truncated bool           `json:"truncated"`
	}
	body := ecamSearchRequest{
		LogType:   "cdn",
		StartTime: start.UnixMilli(),
		EndTime:   end.UnixMilli(),
		Query:     "*",
		Filters:   []ecamFieldFilter{{Field: "host", Op: "contains", Value: domain}},
		Limit:     sampleLimit,
	}
	if err := s.post(ctx, s.pathPrefix+"/search", body, &data); err != nil {
		return 0, 0, 0, err
	}
	for _, e := range data.Entries {
		if e.Status >= 500 && e.Status < 600 {
			status5xx++
		}
	}
	return data.Total, status5xx, len(data.Entries), nil
}

// parseServerGroup extracts the backend name + port from an ALB server group
// name. k8s groups are "k8s_<cluster>_<ns>_<service>-svc_<port>"; the service
// is NORMALIZED to the app-log naming (env prefix and "-svc" stripped), because
// the SLB group ("prod-electronic-access-gateway-svc") must match the app
// service field ("electronic-access-gateway") for the RCA log scope to hit.
// Anything else (e.g. a bare "prod-eureka-xxx") is returned verbatim.
func parseServerGroup(sgn string) (string, int) {
	if sgn == "" {
		return "", 0
	}
	if !strings.HasPrefix(sgn, "k8s_") {
		return sgn, 0
	}
	parts := strings.Split(sgn, "_")
	if len(parts) < 4 {
		return sgn, 0
	}
	port := 0
	if len(parts) >= 5 {
		port, _ = strconv.Atoi(parts[4])
	}
	return normalizeSLBService(parts[3]), port
}

// normalizeSLBService maps a k8s service name to the app-log service field:
// "<env>-<name>-svc" -> "<name>".
func normalizeSLBService(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, prefix := range []string{"prod-", "uat-", "fat-", "dev-", "test-", "pre-", "gray-", "sim-", "daily-", "stable-"} {
		if strings.HasPrefix(n, prefix) {
			n = n[len(prefix):]
			break
		}
	}
	return strings.TrimSuffix(n, "-svc")
}
