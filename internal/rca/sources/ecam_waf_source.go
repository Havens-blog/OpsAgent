package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// WAFSource queries WAF attack facts (blocked/challenged entries) for one
// domain — the only signal dimension that can actually explain a WAF alarm.
type WAFSource interface {
	QueryAttackFacts(ctx context.Context, domain string, start, end time.Time) (*models.WAFAttackFacts, error)
}

// WAFAttackQuery is the "attack surface" pushdown expression: only entries
// carrying a disposition match. On the mixed WAF store a "*" query's
// newest-first sample is drowned by normal traffic (measured on
// proxy.jlc.com: a 500-row "*" sample held zero disposition entries while the
// disposition filter returned 100+ in the same window).
const WAFAttackQuery = "final_action: block or final_action: js or final_action: captcha or acl_action: block or waf_action: block or cc_action: block or antiscan_action: block"

// ECamWAFSource consumes the e-cam multi-cloud log federation (log_type
// "waf") over the S2S token path, scoped per domain via resources.
type ECamWAFSource struct {
	endpoint      string
	token         string
	tokenProvider TokenProvider
	pathPrefix    string
	client        *http.Client
}

// NewECamWAFSource builds a WAF facts source over the e-cam federation.
// timeout <= 0 defaults to 30s.
func NewECamWAFSource(endpoint, token string, timeout time.Duration) *ECamWAFSource {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &ECamWAFSource{
		endpoint:   strings.TrimRight(endpoint, "/"),
		token:      token,
		pathPrefix: "/api/v1/cam/logs",
		client:     &http.Client{Timeout: timeout},
	}
}

// WithTokenProvider injects the S2S token exchanger and switches the path to
// the /api/s2s/logs group (path + auth must move together to avoid a 401).
func (s *ECamWAFSource) WithTokenProvider(p TokenProvider) *ECamWAFSource {
	s.tokenProvider = p
	s.pathPrefix = "/api/s2s/logs"
	return s
}

func (s *ECamWAFSource) bearerToken(ctx context.Context) (string, error) {
	if s.tokenProvider != nil {
		return s.tokenProvider.TokenFor(ctx)
	}
	return s.token, nil
}

// Ping probes the federation for reachability (types endpoint doubles as a
// token check), same contract as the other real sources.
func (s *ECamWAFSource) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint+s.pathPrefix+"/types", nil)
	if err != nil {
		return fmt.Errorf("ecam waf source: build request: %w", err)
	}
	tok, err := s.bearerToken(ctx)
	if err != nil {
		return fmt.Errorf("ecam waf source: get token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ecam waf source: ping %s: %w", s.endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ecam waf source: ping status %d", resp.StatusCode)
	}
	var env ecamResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("ecam waf source: ping decode: %w", err)
	}
	if env.Code != 0 {
		return fmt.Errorf("ecam waf source: ping code %d: %s", env.Code, env.Message)
	}
	return nil
}

// ecamWAFEntry is the subset of the federation WAF entry the facts need.
type ecamWAFEntry struct {
	Timestamp int64  `json:"timestamp"`
	ClientIP  string `json:"client_ip"`
	Host      string `json:"host"`
	URI       string `json:"uri"`
	RuleName  string `json:"rule_name"`
	Action    string `json:"action"`
}

type ecamWAFSearchData struct {
	Total     int64          `json:"total"`
	Entries   []ecamWAFEntry `json:"entries"`
	Truncated bool           `json:"truncated"`
}

// QueryAttackFacts returns the aggregated attack facts for one domain in the
// window. An empty result is an honest "no attack entries", not an error.
func (s *ECamWAFSource) QueryAttackFacts(ctx context.Context, domain string, start, end time.Time) (*models.WAFAttackFacts, error) {
	domain = models.NormalizeWAFDomain(domain)
	if domain == "" {
		return nil, fmt.Errorf("ecam waf source: empty domain")
	}

	const sampleLimit = 500
	body := ecamSearchRequest{
		LogType:   "waf",
		StartTime: start.UnixMilli(),
		EndTime:   end.UnixMilli(),
		Query:     WAFAttackQuery,
		Resources: []string{domain},
		Limit:     sampleLimit,
	}
	var data ecamWAFSearchData
	if err := s.post(ctx, s.pathPrefix+"/search", body, &data); err != nil {
		return nil, err
	}

	byRule := map[string]int{}
	byIP := map[string]int{}
	byURI := map[string]int{}
	byAction := map[string]int{}
	for _, e := range data.Entries {
		byRule[e.RuleName]++
		byIP[e.ClientIP]++
		byURI[e.URI]++
		byAction[e.Action]++
	}
	// The aggregates below describe the SAMPLE. Hitting the cap (or a
	// truncated federation fan-out) means the real volume is larger — the
	// facts must say "at least", never pass the sample off as the total.
	return &models.WAFAttackFacts{
		Domain:        domain,
		Total:         len(data.Entries),
		Matched:       data.Total,
		SampleCapped:  data.Truncated || len(data.Entries) >= sampleLimit,
		WindowStart:   start,
		WindowEnd:     end,
		AttackRules:   topFacts(byRule, 10),
		SourceIPs:     topFacts(byIP, 10),
		SourceIPCount: len(byIP),
		TargetURIs:    topFacts(byURI, 10),
		Actions:       topFacts(byAction, 5),
	}, nil
}

// topFacts returns the n highest-count keys, descending.
func topFacts(m map[string]int, n int) []models.FactCount {
	type kv struct {
		k string
		v int
	}
	list := make([]kv, 0, len(m))
	for k, v := range m {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	if len(list) > n {
		list = list[:n]
	}
	out := make([]models.FactCount, 0, len(list))
	for _, x := range list {
		out = append(out, models.FactCount{Key: x.k, Count: x.v})
	}
	return out
}

// post performs one authenticated federation call and decodes data into out.
func (s *ECamWAFSource) post(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("ecam waf source: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("ecam waf source: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	tok, err := s.bearerToken(ctx)
	if err != nil {
		return fmt.Errorf("ecam waf source: get token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ecam waf source: POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("ecam waf source: POST %s status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	var env ecamResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("ecam waf source: POST %s decode: %w", path, err)
	}
	if env.Code != 0 {
		return fmt.Errorf("ecam waf source: POST %s code %d: %s", path, env.Code, env.Message)
	}
	if len(env.Data) == 0 {
		return nil // honest empty: code 0 with no payload
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("ecam waf source: POST %s data decode: %w", path, err)
	}
	return nil
}

// EmptyWAFSource is the honest no-backend implementation: no facts, no error.
type EmptyWAFSource struct{}

func NewEmptyWAFSource() *EmptyWAFSource { return &EmptyWAFSource{} }

func (EmptyWAFSource) QueryAttackFacts(ctx context.Context, domain string, start, end time.Time) (*models.WAFAttackFacts, error) {
	return nil, nil
}
