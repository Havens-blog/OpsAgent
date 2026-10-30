package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CMDBModel is one model (asset type) known to the CMDB.
type CMDBModel struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}

// CMDBInstance is one resource instance from the CMDB federation.
type CMDBInstance struct {
	ID         int64                  `json:"id"`
	ModelUID   string                 `json:"model_uid"`
	AssetID    string                 `json:"asset_id"`
	AssetName  string                 `json:"asset_name"`
	TenantID   int64                  `json:"tenant_id"`
	Attributes map[string]interface{} `json:"attributes"`
}

// CMDBStatGroup is one grouped count row (by asset type, or by provider).
type CMDBStatGroup struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// CMDBStats is the aggregated cloud-resource overview: total instances plus the
// per-asset-type, per-provider and per-business-line distributions. This is the
// "云资源概况" the conversational assistant answers from, fetched in one call.
type CMDBStats struct {
	Total       int64           `json:"total"`
	ByAssetType []CMDBStatGroup `json:"by_asset_type"`
	ByProvider  []CMDBStatGroup `json:"by_provider"`
	ByBusiness  []CMDBStatGroup `json:"by_business"`
}

// CMDBDomain is the domain->origin mapping the RCA needs to anchor a
// dial-test/probe alarm on a real backend instead of a string-token guess.
// It also carries the CDN hop (provider + acceleration CNAME) so the edge
// chain can be shown even when the CDN access-log federation has no data.
type CMDBDomain struct {
	Domain     string `json:"domain"`
	OriginHost string `json:"origin_host"`
	OriginType string `json:"origin_type"`
	Provider   string `json:"provider"`
	CNAME      string `json:"cname"`
}

// CMDBDataSource queries the e-cmdb resource topology over the S2S token path
// (EIAM client_credentials; the CMDB verifies EIAM-issued RS256 tokens).
type CMDBDataSource struct {
	endpoint      string
	tokenProvider TokenProvider
	client        *http.Client
}

// NewCMDBDataSource builds a CMDB source. endpoint is the base URL
// (e.g. http://127.0.0.1:8002); timeout <= 0 defaults to 30s.
func NewCMDBDataSource(endpoint string, timeout time.Duration) *CMDBDataSource {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &CMDBDataSource{
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{Timeout: timeout},
	}
}

// WithTokenProvider injects the EIAM S2S token exchanger (same token the CMDB
// verifies). Without it the source cannot authenticate to /internal.
func (s *CMDBDataSource) WithTokenProvider(p TokenProvider) *CMDBDataSource {
	s.tokenProvider = p
	return s
}

func (s *CMDBDataSource) bearer(ctx context.Context) (string, error) {
	if s.tokenProvider == nil {
		return "", fmt.Errorf("cmdb source: no S2S token provider configured")
	}
	return s.tokenProvider.TokenFor(ctx)
}

// ListModels returns the model inventory (uid -> display name) from the CMDB.
func (s *CMDBDataSource) ListModels(ctx context.Context) ([]CMDBModel, error) {
	var env struct {
		Code int               `json:"code"`
		Msg  string            `json:"msg"`
		Data map[string]string `json:"data"`
	}
	if err := s.get(ctx, "/api/v1/cmdb/internal/model-aliases", nil, &env); err != nil {
		return nil, err
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("cmdb model-aliases code %d: %s", env.Code, env.Msg)
	}
	out := make([]CMDBModel, 0, len(env.Data))
	for uid, name := range env.Data {
		out = append(out, CMDBModel{UID: uid, Name: name})
	}
	return out, nil
}

// ListInstances returns instances of one model (plus the federation total),
// optionally filtered by asset name and cloud provider (attributes.provider).
// The total is the true match count across all pages, so callers can report
// "共 N 个，前 M 个如下" instead of mistaking a bounded page for the whole set.
// tenantID is explicit because the S2S path carries no session tenant.
func (s *CMDBDataSource) ListInstances(ctx context.Context, tenantID int64, modelUID, assetName, provider string, limit int) ([]CMDBInstance, int64, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := url.Values{}
	if modelUID != "" {
		q.Set("model_uid", modelUID)
	}
	if assetName != "" {
		q.Set("asset_name", assetName)
	}
	if provider != "" {
		q.Set("provider", normalizeProvider(provider))
	}
	q.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	q.Set("limit", strconv.Itoa(limit))

	var resp struct {
		Instances []CMDBInstance `json:"instances"`
		Total     int64          `json:"total"`
	}
	if err := s.get(ctx, "/api/v1/cmdb/internal/instances/list?"+q.Encode(), nil, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Instances, resp.Total, nil
}

// StatsAll returns the aggregated cloud-resource overview for one tenant: total
// instances plus the per-asset-type and per-provider distributions. It is the
// single call behind the "云资源概况" assistant tool.
func (s *CMDBDataSource) StatsAll(ctx context.Context, tenantID int64) (*CMDBStats, error) {
	q := url.Values{}
	q.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	var stats CMDBStats
	if err := s.get(ctx, "/api/v1/cmdb/internal/stats/all?"+q.Encode(), nil, &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

// cloudProviderPrefixes are the model_uid vendor prefixes the CMDB uses for
// per-vendor real models (e.g. volcano_ecs, tencent_ecs) that must collapse
// into one semantic type for the resource overview.
var cloudProviderPrefixes = []string{
	"aliyun_", "aws_", "azure_", "tencent_", "huawei_", "volcengine_", "volcano_",
}

// assetTypeSynonyms collapses cross-vendor names that mean the same resource.
var assetTypeSynonyms = map[string]string{
	"s3":  "oss", // AWS S3 == object storage
	"slb": "lb",  // Aliyun SLB == load balancer
}

// normalizeProvider folds the e-cam "volcengine" provider spelling into the
// CMDB's "volcano" family prefix, so a caller can pass either.
func normalizeProvider(provider string) string {
	if provider == "volcengine" {
		return "volcano"
	}
	return provider
}

// normalizeAssetType maps a model_uid to its semantic resource type: strip the
// vendor prefix, then fold known synonyms. Bare names (ecs/oss/rds/...) pass
// through unchanged.
func normalizeAssetType(modelUID string) string {
	base := modelUID
	for _, p := range cloudProviderPrefixes {
		if strings.HasPrefix(base, p) {
			base = strings.TrimPrefix(base, p)
			break
		}
	}
	if syn, ok := assetTypeSynonyms[base]; ok {
		return syn
	}
	return base
}

// StatsAllNormalized returns the cloud-resource overview with per-asset-type
// counts merged by semantic type (volcano_ecs + tencent_ecs + ecs -> ecs), so
// the assistant reports "ECS N 台" instead of scattering one type across many
// model_uid keys. ByProvider and Total are unchanged.
func (s *CMDBDataSource) StatsAllNormalized(ctx context.Context, tenantID int64) (*CMDBStats, error) {
	stats, err := s.StatsAll(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	agg := make(map[string]int64, len(stats.ByAssetType))
	for _, g := range stats.ByAssetType {
		agg[normalizeAssetType(g.Key)] += g.Count
	}
	normalized := make([]CMDBStatGroup, 0, len(agg))
	for k, v := range agg {
		normalized = append(normalized, CMDBStatGroup{Key: k, Count: v})
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Count != normalized[j].Count {
			return normalized[i].Count > normalized[j].Count
		}
		return normalized[i].Key < normalized[j].Key
	})
	stats.ByAssetType = normalized
	return stats, nil
}

// LookupDomain resolves a host to its CMDB-recorded origin (CDN/WAF domain
// records carry origin_host). It returns nil, nil when the host is not a known
// CDN domain — an honest "no mapping", never a guessed one.
func (s *CMDBDataSource) LookupDomain(ctx context.Context, tenantID int64, domain string) (*CMDBDomain, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return nil, fmt.Errorf("cmdb source: empty domain")
	}
	instances, _, err := s.ListInstances(ctx, tenantID, "cdn", "", "", 500)
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		name := strings.ToLower(strings.TrimSpace(inst.AssetName))
		if name != domain {
			continue
		}
		return &CMDBDomain{
			Domain:     domain,
			OriginHost: strAttr(inst.Attributes, "origin_host"),
			OriginType: strAttr(inst.Attributes, "origin_type"),
			Provider:   strAttr(inst.Attributes, "provider"),
			CNAME:      strAttr(inst.Attributes, "cname"),
		}, nil
	}
	return nil, nil
}

// get performs one authenticated GET and decodes the body into out.
func (s *CMDBDataSource) get(ctx context.Context, path string, query url.Values, out any) error {
	u := s.endpoint + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("cmdb source: build request: %w", err)
	}
	tok, err := s.bearer(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("cmdb source: GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("cmdb source: GET %s status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("cmdb source: GET %s decode: %w", path, err)
	}
	return nil
}

func strAttr(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
