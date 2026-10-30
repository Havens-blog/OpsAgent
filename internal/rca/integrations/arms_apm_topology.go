// Package integrations provides an ARMS-APM-Prometheus-based topology source.
package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

// runConcurrent runs the given functions concurrently and returns the first
// non-nil error (after all have finished). It is used to parallelize the
// independent PromQL round-trips the topology source makes.
func runConcurrent(fns ...func() error) error {
	var wg sync.WaitGroup
	errs := make([]error, len(fns))
	for i, fn := range fns {
		wg.Add(1)
		go func(i int, fn func() error) {
			defer wg.Done()
			errs[i] = fn()
		}(i, fn)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// wrapErr annotates err with a context label, or returns nil when err is nil.
func wrapErr(label string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", label, err)
}

// ARMSPromTopologySource builds a service dependency graph from ARMS APM
// metrics exposed via an OpenAPI-compatible Prometheus endpoint (the
// arms-metrics-* workspace), authenticated with a bearer token.
//
// Unlike ARMSTopologySource (which calls the ARMS OpenAPI with AK/SK signing),
// this source needs only the Prometheus query endpoint + token. It derives the
// topology from the labels on arms_app_requests_count_raw:
//
//   - service    : the calling service (a graph node)
//   - callKind   : http / http_client / sql / cache / nosql / producer / ...
//   - serverIp   : the IP:port a service serves on (server side)
//   - destId     : the IP:port / resource a client calls (client side)
//   - callType   : http / mysql / redis / kafka / elasticsearch / ...
//
// Edges between application services are recovered by joining a client's destId
// IP to the serverIp of the service listening on it (verified ~71% resolvable
// for http_client calls in production). Infrastructure dependencies
// (mysql/redis/kafka/...) become edges to synthetic infrastructure nodes keyed
// by destId.
type ARMSPromTopologySource struct {
	client   *http.Client
	endpoint string
	token    string

	// Each topology/metrics fetch fans out to 3-4 PromQL round-trips to ARMS.
	// The console polls this endpoint continuously, so a short cache both cuts
	// ARMS load and, on a transient ARMS error, lets us serve the last-good
	// result instead of a 502 that would ripple up the proxy chain.
	topoMu        sync.Mutex
	topoCache     *models.Topology
	topoExpire    time.Time
	topoRetryAt   time.Time
	metricsMu     sync.Mutex
	metricsCache  map[string]sources.ServiceMetrics
	metricsExpire time.Time
	metricsRetryAt time.Time
	ttl           time.Duration
}

// NewARMSPromTopologySource creates a topology source over an ARMS APM
// Prometheus endpoint. endpoint is the workspace base URL (this code appends
// /api/v1); token is the bearer token.
func NewARMSPromTopologySource(endpoint, token string) (*ARMSPromTopologySource, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("ARMS APM endpoint is required")
	}
	return &ARMSPromTopologySource{
		client:   &http.Client{Timeout: 40 * time.Second},
		endpoint: strings.TrimSuffix(endpoint, "/"),
		token:    token,
		ttl:      15 * time.Second,
	}, nil
}

// Ping verifies the ARMS APM Prometheus endpoint is reachable and the bearer
// token is accepted, by issuing a trivial instant query. Returns nil only on a
// successful round-trip, so a successful Ping means the source is genuinely
// usable rather than merely constructed.
func (a *ARMSPromTopologySource) Ping(ctx context.Context) error {
	_, err := a.queryVector(ctx, "vector(1)")
	return err
}

// vectorSample is one result of an instant query.
type vectorSample struct {
	Metric map[string]string
}

// queryVector runs an instant PromQL query and returns the per-series label
// sets. Values are ignored (we only need the label combinations for topology).
func (a *ARMSPromTopologySource) queryVector(ctx context.Context, promql string) ([]vectorSample, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", a.endpoint, url.QueryEscape(promql))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ARMS APM query failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("ARMS APM query returned status %q", result.Status)
	}

	out := make([]vectorSample, 0, len(result.Data.Result))
	for _, r := range result.Data.Result {
		out = append(out, vectorSample{Metric: r.Metric})
	}
	return out, nil
}

// queryServiceValues runs an instant query that is pre-aggregated
// `... by (service)` and returns a service -> value map. Non-finite results
// (+Inf / NaN, which arise from divide-by-near-zero on low-traffic services)
// are dropped so they never reach json.Marshal, which would otherwise fail the
// whole HTTP response.
func (a *ARMSPromTopologySource) queryServiceValues(ctx context.Context, promql string) (map[string]float64, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", a.endpoint, url.QueryEscape(promql))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ARMS APM query failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []interface{}     `json:"value"` // [ <ts float>, "<value string>" ]
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("ARMS APM query returned status %q", result.Status)
	}

	out := make(map[string]float64, len(result.Data.Result))
	for _, r := range result.Data.Result {
		svc := r.Metric["service"]
		if svc == "" || len(r.Value) != 2 {
			continue
		}
		s, ok := r.Value[1].(string)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			continue // drop non-finite values (JSON-unsafe)
		}
		out[svc] = v
	}
	return out, nil
}

// GetServiceMetrics returns per-service RED figures derived from the ARMS APM
// counters. It satisfies sources.ServiceMetricsProvider. Values are sanitized
// (no Inf/NaN) in queryServiceValues.
func (a *ARMSPromTopologySource) GetServiceMetrics(ctx context.Context) (map[string]sources.ServiceMetrics, error) {
	a.metricsMu.Lock()
	defer a.metricsMu.Unlock()

	now := time.Now()
	if a.metricsCache != nil && now.Before(a.metricsExpire) {
		return a.metricsCache, nil
	}
	// Negative cache: after a failed fetch, back off briefly so concurrent
	// callers don't each pay a fresh timeout serially.
	if now.Before(a.metricsRetryAt) {
		if a.metricsCache != nil {
			return a.metricsCache, nil
		}
		return nil, fmt.Errorf("ARMS service metrics unavailable, retry after %s", a.metricsRetryAt.Format(time.RFC3339))
	}

	m, err := a.fetchServiceMetrics(ctx)
	if err == nil {
		a.metricsCache = m
		a.metricsExpire = now.Add(a.ttl)
		return m, nil
	}
	a.metricsRetryAt = now.Add(5 * time.Second)
	if a.metricsCache != nil {
		// Transient ARMS failure: serve the last-good snapshot instead of a 502.
		log.Printf("[arms-topology] serving stale service metrics: %v", err)
		return a.metricsCache, nil
	}
	return nil, err
}

// fetchServiceMetrics runs the three independent RED queries concurrently
// rather than paying three sequential round-trips to ARMS.
func (a *ARMSPromTopologySource) fetchServiceMetrics(ctx context.Context) (map[string]sources.ServiceMetrics, error) {
	var throughput, errRate, latency map[string]float64
	if err := runConcurrent(
		func() (err error) {
			throughput, err = a.queryServiceValues(ctx,
				`sum by (service) (rate(arms_app_requests_count_raw[5m]))`)
			return wrapErr("throughput query", err)
		},
		func() (err error) {
			errRate, err = a.queryServiceValues(ctx,
				`sum by (service) (rate(arms_app_requests_error_count_raw[5m])) / sum by (service) (rate(arms_app_requests_count_raw[5m]))`)
			return wrapErr("error-rate query", err)
		},
		func() (err error) {
			latency, err = a.queryServiceValues(ctx,
				`1000 * sum by (service) (rate(arms_app_requests_seconds_raw[5m])) / sum by (service) (rate(arms_app_requests_count_raw[5m]))`)
			return wrapErr("latency query", err)
		},
	); err != nil {
		return nil, err
	}

	out := make(map[string]sources.ServiceMetrics, len(throughput))
	for svc, tp := range throughput {
		out[svc] = sources.ServiceMetrics{
			Throughput: tp,
			ErrorRate:  errRate[svc], // zero value when absent, which is correct
			LatencyMs:  latency[svc],
		}
	}
	return out, nil
}

// ipOf strips the port from an "ip:port" (or "host:port") string.
func ipOf(addr string) string {
	if addr == "" {
		return ""
	}
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}

// infraType maps an ARMS callType to a topology node type.
func infraType(callType string) (nodeType string, isInfra bool) {
	switch strings.ToLower(callType) {
	case "mysql", "postgresql", "sqlserver", "mongodb":
		return "database", true
	case "redis":
		return "cache", true
	case "kafka", "kafka_client", "mq", "mq_client", "rocketmq":
		return "queue", true
	case "elasticsearch":
		return "database", true
	}
	return "", false
}

// GetTopology returns the service dependency graph, cached briefly so the
// console's continuous polling and concurrent RCA runs do not each fan out a
// fresh set of PromQL queries to ARMS. A transient ARMS error falls back to
// the last-good snapshot rather than surfacing a 502 up the proxy chain.
func (a *ARMSPromTopologySource) GetTopology(ctx context.Context) (*models.Topology, error) {
	a.topoMu.Lock()
	defer a.topoMu.Unlock()

	now := time.Now()
	if a.topoCache != nil && now.Before(a.topoExpire) {
		return a.topoCache, nil
	}
	// Negative cache: after a failed fetch, back off briefly so concurrent
	// callers don't each pay a fresh timeout serially.
	if now.Before(a.topoRetryAt) {
		if a.topoCache != nil {
			return a.topoCache, nil
		}
		return nil, fmt.Errorf("ARMS topology unavailable, retry after %s", a.topoRetryAt.Format(time.RFC3339))
	}

	topo, err := a.fetchTopology(ctx)
	if err == nil {
		a.topoCache = topo
		a.topoExpire = now.Add(a.ttl)
		return topo, nil
	}
	a.topoRetryAt = now.Add(5 * time.Second)
	if a.topoCache != nil {
		log.Printf("[arms-topology] serving stale topology: %v", err)
		return a.topoCache, nil
	}
	return nil, err
}

// fetchTopology builds the service dependency graph from ARMS APM metrics.
func (a *ARMSPromTopologySource) fetchTopology(ctx context.Context) (*models.Topology, error) {
	nodes := map[string]*models.ServiceNode{} // name -> node
	type edgeKey struct{ from, to, typ string }
	edges := map[edgeKey]bool{}

	addNode := func(name, typ string) {
		if name == "" {
			return
		}
		if n, ok := nodes[name]; ok {
			// Prefer a concrete type over an empty one.
			if n.Type == "" && typ != "" {
				n.Type = typ
			}
			return
		}
		nodes[name] = &models.ServiceNode{Name: name, Type: typ, Labels: map[string]string{}}
	}

	// The four discovery queries are independent at the network level, so fetch
	// them concurrently; only the in-memory processing below has an ordering
	// dependency (client-edge resolution needs the serverIp map first).
	var svcRows, serverRows, clientRows, infraRows []vectorSample
	if err := runConcurrent(
		func() (err error) {
			svcRows, err = a.queryVector(ctx, `count by (service) (arms_app_requests_count_raw)`)
			return wrapErr("query services", err)
		},
		func() (err error) {
			serverRows, err = a.queryVector(ctx, `count by (service, serverIp) (arms_app_requests_count_raw{callKind="http"})`)
			return wrapErr("query server ips", err)
		},
		func() (err error) {
			clientRows, err = a.queryVector(ctx, `count by (service, destId) (arms_app_requests_count_raw{callKind="http_client"})`)
			return wrapErr("query client edges", err)
		},
		func() (err error) {
			// Infra edges are best-effort: a failure here must not fail the
			// whole topology, so swallow the error and leave infraRows empty.
			infraRows, _ = a.queryVector(ctx, `count by (service, callType, destId) (arms_app_requests_count_raw{callKind=~"sql|cache|nosql|producer|consumer"})`)
			return nil
		},
	); err != nil {
		return nil, err
	}

	// 1) All application services are nodes.
	for _, r := range svcRows {
		addNode(r.Metric["service"], "application")
	}

	// 2) Build an IP -> service map from server-side (callKind="http") series.
	ipToService := map[string]string{}
	for _, r := range serverRows {
		ip := ipOf(r.Metric["serverIp"])
		svc := r.Metric["service"]
		if ip != "" && svc != "" {
			ipToService[ip] = svc
		}
	}

	// 3) Client-side http edges: resolve destId IP to a server service.
	for _, r := range clientRows {
		from := r.Metric["service"]
		destIP := ipOf(r.Metric["destId"])
		if from == "" || destIP == "" {
			continue
		}
		if to, ok := ipToService[destIP]; ok && to != from {
			addNode(to, "application")
			edges[edgeKey{from, to, "http"}] = true
		}
	}

	// 4) Infrastructure dependencies (db/cache/queue). The destId becomes a
	//    synthetic infra node so the dependency is visible in the graph.
	//    infraRows was fetched concurrently above (best-effort; may be empty).
	for _, r := range infraRows {
		from := r.Metric["service"]
		dest := r.Metric["destId"]
		nodeType, isInfra := infraType(r.Metric["callType"])
		if from == "" || dest == "" || !isInfra {
			continue
		}
		// Use a readable infra node name: "<type>:<host>".
		infraName := fmt.Sprintf("%s:%s", strings.ToLower(r.Metric["callType"]), ipOf(dest))
		addNode(infraName, nodeType)
		edges[edgeKey{from, infraName, edgeTypeFor(nodeType)}] = true
	}

	topo := &models.Topology{
		Nodes: make([]models.ServiceNode, 0, len(nodes)),
		Edges: make([]models.DependencyEdge, 0, len(edges)),
	}
	for _, n := range nodes {
		topo.Nodes = append(topo.Nodes, *n)
	}
	for e := range edges {
		topo.Edges = append(topo.Edges, models.DependencyEdge{From: e.from, To: e.to, Type: e.typ})
	}
	return topo, nil
}

// edgeTypeFor maps a node type to a dependency edge type.
func edgeTypeFor(nodeType string) string {
	switch nodeType {
	case "database":
		return "database"
	case "cache":
		return "cache"
	case "queue":
		return "queue"
	}
	return "rpc"
}

// GetServiceNeighbors returns dependents and dependencies for a service.
func (a *ARMSPromTopologySource) GetServiceNeighbors(ctx context.Context, service string) (dependents, dependencies []string, err error) {
	topo, err := a.GetTopology(ctx)
	if err != nil {
		return nil, nil, err
	}
	dependencies = topo.GetDependencies(service)
	dependents = topo.GetDependents(service)
	return dependents, dependencies, nil
}

// GetServiceHealth derives a coarse health map from error-rate presence.
// A service with any error requests in the recent window is marked "degraded",
// otherwise "healthy". This is best-effort and non-fatal.
func (a *ARMSPromTopologySource) GetServiceHealth(ctx context.Context) (map[string]string, error) {
	health := map[string]string{}
	// Services currently serving traffic -> healthy baseline.
	svcRows, err := a.queryVector(ctx, `count by (service) (arms_app_requests_count_raw)`)
	if err != nil {
		return nil, err
	}
	for _, r := range svcRows {
		if s := r.Metric["service"]; s != "" {
			health[s] = "healthy"
		}
	}
	// Services with errors -> degraded.
	errRows, err := a.queryVector(ctx, `count by (service) (arms_app_requests_error_count_raw > 0)`)
	if err == nil {
		for _, r := range errRows {
			if s := r.Metric["service"]; s != "" {
				health[s] = "degraded"
			}
		}
	}
	return health, nil
}
