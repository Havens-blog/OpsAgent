// Package engine provides Root Cause Analysis (RCA) engine implementation.
package engine

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/scoring"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

// Engine is the RCA engine that orchestrates root cause analysis.
type Engine struct {
	logSource       sources.LogSource
	metricSource    sources.MetricSource
	traceSource     sources.TraceSource
	topologySource  sources.TopologySource
	changeSource    sources.ChangeSource
	wafSource       sources.WAFSource
	backendResolver sources.DomainBackendResolver
	slbEdgeResolver sources.SLBEdgeResolver
	cdnEdgeResolver sources.CDNEdgeResolver
	weights         scoring.Weights
	windowSize      time.Duration
	minConfidence   float64
}

// NewEngine creates a new RCA engine.
func NewEngine(opts ...Option) (*Engine, error) {
	e := &Engine{
		weights:       scoring.DefaultWeights(),
		windowSize:    15 * time.Minute,
		minConfidence: 0.3,
	}

	for _, opt := range opts {
		opt(e)
	}

	if e.logSource == nil {
		return nil, fmt.Errorf("log source is required")
	}
	if e.metricSource == nil {
		return nil, fmt.Errorf("metric source is required")
	}

	return e, nil
}

// Option configures the Engine.
type Option func(*Engine)

func WithLogSource(s sources.LogSource) Option {
	return func(e *Engine) { e.logSource = s }
}

func WithMetricSource(s sources.MetricSource) Option {
	return func(e *Engine) { e.metricSource = s }
}

func WithTraceSource(s sources.TraceSource) Option {
	return func(e *Engine) { e.traceSource = s }
}

func WithTopologySource(s sources.TopologySource) Option {
	return func(e *Engine) { e.topologySource = s }
}

func WithChangeSource(s sources.ChangeSource) Option {
	return func(e *Engine) { e.changeSource = s }
}

func WithDomainBackendResolver(r sources.DomainBackendResolver) Option {
	return func(e *Engine) { e.backendResolver = r }
}

func WithSLBEdgeResolver(r sources.SLBEdgeResolver) Option {
	return func(e *Engine) { e.slbEdgeResolver = r }
}

func WithCDNEdgeResolver(r sources.CDNEdgeResolver) Option {
	return func(e *Engine) { e.cdnEdgeResolver = r }
}

func WithWAFSource(s sources.WAFSource) Option {
	return func(e *Engine) { e.wafSource = s }
}

func WithWeights(w scoring.Weights) Option {
	return func(e *Engine) { e.weights = w }
}

func WithWindowSize(d time.Duration) Option {
	return func(e *Engine) { e.windowSize = d }
}

func WithMinConfidence(c float64) Option {
	return func(e *Engine) { e.minConfidence = c }
}

// Analyze performs root cause analysis for the given incident.
func (e *Engine) Analyze(ctx context.Context, incident *models.Incident) (*models.RCAReport, error) {
	startTime := time.Now()

	// Step 0: Resolve a domain-scoped (dial-test) alarm to its real backend
	// services before gathering — so the log/k8s/metric scoping anchors on the
	// actual gateway the probe hit, not a business-unit token guess.
	resolvedServices := e.resolveDomainServices(ctx, incident)

	// Step 0b: Gather window-scoped edge (WAF/SLB) facts as reference material.
	// For a dial-test alarm these answer "did the edge act in the window" and
	// are rendered separately — never scored into a root-cause candidate.
	edgeFacts := e.gatherEdgeFacts(ctx, incident)

	// Step 1: Gather signals in parallel
	signals, err := e.gatherSignals(ctx, incident)
	if err != nil {
		return nil, fmt.Errorf("failed to gather signals: %w", err)
	}

	// Step 2: Normalize time windows (trend-based alignment)
	normalized := e.normalizeTimeWindows(signals, incident)

	// Step 3: Generate and score hypotheses
	candidates := e.generateHypotheses(ctx, signals, normalized, incident)
	scores := e.scoreCandidates(ctx, candidates, normalized, incident)

	// Step 4: Rank by confidence and filter
	ranked := e.rankAndFilter(scores, e.minConfidence, incident)

	// Step 5: Generate report
	note := ""
	if len(resolvedServices) > 0 && !hasBackendSignal(ranked) {
		// The domain resolved to a real backend AND neither the gateway nor its
		// downstream showed an error/k8s signal — say so instead of falling
		// back to unrelated infra metric noise. Edge facts narrow the vague
		// "edge suspicion" to the actual hop: 5xx on SLB->backend or CDN edge
		// localizes the failure, and a clean forward on both clears the edge and
		// points at the probe network.
		svc := strings.Join(resolvedServices, ", ")
		note = edgeNote(edgeFacts, svc,
			fmt.Sprintf("域名已解析到后端服务 %s,但该服务及下游在窗口内无错误日志/k8s 故障,后端大概率正常;不可用可能来自边缘(WAF/SLB/CDN)或瞬时抖动", svc))
	} else if len(resolvedServices) == 0 {
		if domain, unmapped := domainUnmapped(incident, ranked); unmapped {
			note = fmt.Sprintf("未能将域名 %s 映射到后端服务(拓扑/k8s 无匹配命名),无法确认根因;以下候选为窗口内信号,仅供参考", domain)
		}
	}
	report := &models.RCAReport{
		Incident:       incident,
		Signals:        signals,
		Candidates:     ranked,
		TopCandidate:   getTopCandidate(ranked),
		Confidence:     getOverallConfidence(ranked),
		AnalysisWindow: getAnalysisWindow(incident, e.windowSize),
		Duration:       time.Since(startTime),
		GeneratedAt:    time.Now(),
		Warnings:       buildWarnings(signals.SourceErrors),
		Note:           note,
		EdgeFacts:      edgeFacts,
	}

	return report, nil
}

// srcErr records a single source collection failure. A buffered channel of
// these lets goroutines report errors without sharing and mutating one map.
type srcErr struct {
	name string
	msg  string
}

// resolveDomainServices resolves a domain-scoped (non-WAF) alarm to its backend
// k8s services via the SLB access log, then appends those service names to
// incident.AffectedServices so the log/k8s/metric scoping anchors on the real
// gateway. It returns the resolved service names (empty = no resolution).
func (e *Engine) resolveDomainServices(ctx context.Context, incident *models.Incident) []string {
	if e.backendResolver == nil || models.IsWAFAlarm(incident.Description) {
		return nil
	}
	domain := models.ExtractWAFDomain(incident.Description)
	if domain == "" {
		return nil
	}
	backends, err := e.backendResolver.QueryDomainBackends(ctx, domain,
		incident.StartTime.Add(-30*time.Minute), incident.EndTime)
	if err != nil || len(backends) == 0 {
		return nil
	}

	seen := map[string]bool{}
	for _, s := range incident.AffectedServices {
		seen[s] = true
	}
	var resolved []string
	for _, b := range backends {
		if b.Service == "" || seen[b.Service] {
			continue
		}
		seen[b.Service] = true
		incident.AffectedServices = append(incident.AffectedServices, b.Service)
		resolved = append(resolved, b.Service)
	}
	return resolved
}

// gatherEdgeFacts collects window-scoped SLB facts for a domain alarm as
// REFERENCE material. It is gated to non-WAF alarms: a WAF alarm already
// queries WAF facts into signals.WAFFacts (and scores them as a root cause),
// so the edge section is only for dial-test/probe alarms. It is never scored —
// a dial-test "unavailable rate" is answered by whether the SLB forwarded 5xx
// in the window, which localizes the failure to the SLB->backend hop or clears
// it upstream.
func (e *Engine) gatherEdgeFacts(ctx context.Context, incident *models.Incident) *models.EdgeFacts {
	if models.IsWAFAlarm(incident.Description) {
		return nil
	}
	domain := models.ExtractWAFDomain(incident.Description)
	if domain == "" {
		return nil
	}
	if e.slbEdgeResolver == nil && e.cdnEdgeResolver == nil {
		return nil
	}

	facts := &models.EdgeFacts{Domain: domain}
	start := incident.StartTime.Add(-30 * time.Minute)
	end := incident.EndTime

	var wg sync.WaitGroup
	if e.slbEdgeResolver != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if slb, err := e.slbEdgeResolver.QueryDomainSLB(ctx, domain, start, end); err == nil && slb != nil {
				facts.SLB = slb
			}
		}()
	}
	if e.cdnEdgeResolver != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cdn, err := e.cdnEdgeResolver.QueryDomainCDN(ctx, domain, start, end); err == nil && cdn != nil {
				facts.CDN = cdn
			}
		}()
	}
	wg.Wait()

	if facts.SLB == nil && facts.CDN == nil {
		return nil
	}
	return facts
}

// edgeNote narrows the vague "edge suspicion" Note to the actual hop when edge
// facts are available. Order matters: SLB 5xx and CDN edge 5xx are the only
// localizing signals; a clean forward on both clears the edge entirely. When
// CDN logs are missing (collection off), that gap is stated rather than guessed.
func edgeNote(ef *models.EdgeFacts, svc, fallback string) string {
	if ef == nil {
		return fallback
	}
	slb, cdn := ef.SLB, ef.CDN

	if slb != nil && slb.Status5xx > 0 {
		return fmt.Sprintf("域名已解析到后端服务 %s,后端无错误日志/k8s 故障,但 SLB→后端在窗口内出现 %d 条 5xx,不可用大概率在网关→后端这一跳", svc, slb.Status5xx)
	}
	if cdn != nil && cdn.Status5xx > 0 {
		return fmt.Sprintf("域名已解析到后端服务 %s,后端与 SLB 均正常,但 CDN 边缘在窗口内返回 %d 条 5xx,不可用大概率在 CDN 这一跳", svc, cdn.Status5xx)
	}
	if slb != nil && slb.Total > 0 {
		switch {
		case cdn != nil && cdn.LogChecked && cdn.Total > 0:
			return fmt.Sprintf("域名已解析到后端服务 %s,后端无错误日志,SLB→后端与 CDN 边缘均无 5xx,不可用不在 CDN/SLB/后端,收敛到探测网络或瞬时抖动", svc)
		case cdn != nil && cdn.LogChecked:
			return fmt.Sprintf("域名已解析到后端服务 %s,后端无错误日志且 SLB→后端转发正常(无 5xx),CDN 日志未采集,收敛到 CDN/探测网络或瞬时抖动", svc)
		default:
			return fmt.Sprintf("域名已解析到后端服务 %s,后端无错误日志且 SLB→后端转发正常(无 5xx),收敛到 CDN/探测网络或瞬时抖动", svc)
		}
	}
	if slb != nil {
		return fmt.Sprintf("域名已解析到后端服务 %s,后端无错误日志,但窗口内该域名无 SLB 记录,探测流量可能未到达网关,不可用疑似在 CDN/网络侧", svc)
	}
	return fallback
}

// domainUnmapped reports whether the alarm names a host that cannot be linked
// to any backend signal — a dial-test/probe URL whose business-unit token is
// generic (e.g. "gw") or matches none of the surviving candidates. Such
// candidates are window noise, not the probe's cause, so the report must say
// "cannot confirm" rather than crown an unrelated failure as root cause.
func domainUnmapped(incident *models.Incident, ranked []models.ScoredHypothesis) (string, bool) {
	if models.IsWAFAlarm(incident.Description) {
		return "", false
	}
	domain := models.ExtractWAFDomain(incident.Description)
	if domain == "" {
		return "", false
	}
	token := models.ExtractBusinessUnitToken(incident.Description)
	if token == "" || len(token) < 3 {
		return domain, true
	}
	for _, s := range ranked {
		haystack := strings.ToLower(s.Hypothesis.Service + " " + s.Hypothesis.Description)
		if strings.Contains(haystack, token) {
			return "", false
		}
	}
	return domain, true
}

// gatherSignals collects data from all sources in parallel.
func (e *Engine) gatherSignals(ctx context.Context, incident *models.Incident) (*models.Signals, error) {
	signals := &models.Signals{SourceErrors: map[string]string{}}
	srcErrChan := make(chan srcErr, 5)
	doneChan := make(chan struct{}, 5)

	// Collect logs — scoped to the affected services when the incident names
	// any: one store-scoped query per service (parallel, ~0.7s each) instead
	// of a fleet-wide error scan (~39s over 800+ stores) that also hits the
	// federation merge cap (truncated pages bias spike counts). The global
	// scan remains only as the fallback for incidents without services.
	go func() {
		start := incident.StartTime.Add(-e.windowSize)
		end := incident.EndTime
		if len(incident.AffectedServices) == 0 {
			logs, err := e.logSource.QueryErrors(ctx, start, end)
			if err == nil {
				signals.Logs = logs
			} else {
				srcErrChan <- srcErr{name: "logs", msg: err.Error()}
			}
			doneChan <- struct{}{}
			return
		}
		var (
			wg   sync.WaitGroup
			mu   sync.Mutex
			logs []models.LogEntry
		)
		for _, svc := range incident.AffectedServices {
			wg.Add(1)
			go func(svc string) {
				defer wg.Done()
				entries, err := e.logSource.QueryErrorsByService(ctx, svc, start, end)
				if err != nil {
					// Report the first per-service failure without blocking the
					// fan-out (the channel is small and a fleet of failures must
					// not deadlock collection).
					select {
					case srcErrChan <- srcErr{name: "logs", msg: err.Error()}:
					default:
					}
					return
				}
				mu.Lock()
				logs = append(logs, entries...)
				mu.Unlock()
			}(svc)
		}
		wg.Wait()
		signals.Logs = logs
		doneChan <- struct{}{}
	}()

	// Collect metrics — scoped to the affected services when the incident
	// names any. Per-service queries keep the owning service on each series,
	// which the global anomaly scan loses (and which left metric_anomaly
	// candidates with an empty Service, zeroing topology score).
	go func() {
		start := incident.StartTime.Add(-e.windowSize)
		end := incident.EndTime
		if len(incident.AffectedServices) == 0 {
			metrics, err := e.metricSource.QueryAnomalies(ctx, start, end)
			if err == nil {
				signals.Metrics = metrics
			} else {
				srcErrChan <- srcErr{name: "metrics", msg: err.Error()}
			}
			doneChan <- struct{}{}
			return
		}
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			metrics []models.MetricSeries
		)
		for _, svc := range incident.AffectedServices {
			wg.Add(1)
			go func(svc string) {
				defer wg.Done()
				series, err := e.metricSource.QueryMetricsForService(ctx, svc, start, end)
				if err != nil {
					// Report the first per-service failure without blocking the
					// fan-out (mirrors the log fan-out error handling).
					select {
					case srcErrChan <- srcErr{name: "metrics", msg: err.Error()}:
					default:
					}
					return
				}
				mu.Lock()
				for _, s := range series {
					if s.Labels == nil {
						s.Labels = map[string]string{}
					}
					if s.Labels["service"] == "" {
						s.Labels["service"] = svc
					}
					metrics = append(metrics, s)
				}
				mu.Unlock()
			}(svc)
		}
		wg.Wait()
		signals.Metrics = metrics
		doneChan <- struct{}{}
	}()

	// Collect traces
	if e.traceSource != nil {
		go func() {
			traces, err := e.traceSource.QueryErrorTraces(ctx, incident.StartTime, incident.EndTime)
			if err == nil {
				signals.Traces = traces
			} else {
				srcErrChan <- srcErr{name: "traces", msg: err.Error()}
			}
			doneChan <- struct{}{}
		}()
	} else {
		doneChan <- struct{}{}
	}

	// Collect topology
	if e.topologySource != nil {
		go func() {
			topo, err := e.topologySource.GetTopology(ctx)
			if err == nil {
				signals.Topology = topo
			} else {
				srcErrChan <- srcErr{name: "topology", msg: err.Error()}
			}
			doneChan <- struct{}{}
		}()
	} else {
		doneChan <- struct{}{}
	}

	// Collect changes
	if e.changeSource != nil {
		go func() {
			changes, err := e.changeSource.QueryChanges(ctx, incident.StartTime.Add(-e.windowSize), incident.EndTime)
			if err == nil {
				signals.Changes = changes
			} else {
				srcErrChan <- srcErr{name: "changes", msg: err.Error()}
			}
			doneChan <- struct{}{}
		}()
	} else {
		doneChan <- struct{}{}
	}

	// Wait for all to complete
	for i := 0; i < 5; i++ {
		select {
		case <-doneChan:
		case <-ctx.Done():
			return signals, ctx.Err()
		}
	}

	// Every source has reported completion, so the buffered error channel now
	// holds all (at most 5) source errors. Drain it into the map without a lock.
	for len(srcErrChan) > 0 {
		se := <-srcErrChan
		signals.SourceErrors[se.name] = se.msg
	}

	// WAF attack facts: only when the incident text names a protected domain
	// (WAF alarms carry "域名 xxx-waf"). Best-effort and parallel on its own
	// channel — the five doneChan slots above are fully occupied by the core
	// dimensions. Without a named domain there is nothing to query.
	wafDone := make(chan struct{}, 1)
	go func() {
		defer func() { wafDone <- struct{}{} }()
		if e.wafSource == nil {
			return
		}
		// WAF facts only explain a WAF alarm. Synthetic probes (云拨测) are
		// whitelisted at the WAF edge and can never be blocked, so querying
		// WAF for them produces a red herring that hides the real backend
		// signal (a deployment / readiness failure) the probe actually caught.
		if !models.IsWAFAlarm(incident.Description) {
			return
		}
		domain := models.ExtractWAFDomain(incident.Description)
		if domain == "" {
			return
		}
		// Clamp the window tail to start+90min: on the mixed WAF store an
		// over-wide window degrades (a domain outside the probe sample can
		// silently come back empty via the fallback fan-out), and the alarm
		// semantics are "peak in the last 30 minutes" — the attack context
		// lives near the trigger point.
		qEnd := incident.EndTime
		if maxEnd := incident.StartTime.Add(90 * time.Minute); qEnd.After(maxEnd) {
			qEnd = maxEnd
		}
		facts, err := e.wafSource.QueryAttackFacts(ctx, domain,
			incident.StartTime.Add(-30*time.Minute), qEnd)
		if err == nil {
			signals.WAFFacts = facts
		}
	}()
	select {
	case <-wafDone:
	case <-ctx.Done():
		return signals, ctx.Err()
	}

	return signals, nil
}

// buildWarnings converts source errors into a stable-ordered, human-readable
// warning list. A nil result is normalized to an empty slice for the HTTP DTO.
func buildWarnings(sourceErrors map[string]string) []string {
	if len(sourceErrors) == 0 {
		return []string{}
	}

	names := make([]string, 0, len(sourceErrors))
	for name := range sourceErrors {
		names = append(names, name)
	}
	sort.Strings(names)

	warnings := make([]string, 0, len(names))
	for _, name := range names {
		warnings = append(warnings, fmt.Sprintf("%s: %s", name, sourceErrors[name]))
	}
	return warnings
}

// normalizeTimeWindows performs trend-based time alignment.
func (e *Engine) normalizeTimeWindows(signals *models.Signals, incident *models.Incident) *models.NormalizedSignals {
	normalized := &models.NormalizedSignals{
		TimeWindows: generateTimeWindows(incident, e.windowSize),
	}
	normalized.LogByWindow = bucketLogsByTime(signals.Logs, normalized.TimeWindows)
	normalized.MetricTrends = calculateMetricTrends(signals.Metrics, normalized.TimeWindows)
	normalized.TraceByWindow = bucketTracesByTime(signals.Traces, normalized.TimeWindows)
	normalized.Changes = signals.Changes
	normalized.Topology = signals.Topology
	normalized.WAFFacts = signals.WAFFacts
	return normalized
}

func generateTimeWindows(incident *models.Incident, windowSize time.Duration) []models.TimeWindow {
	windows := []models.TimeWindow{}
	subWindow := windowSize / 5
	start := incident.StartTime.Add(-windowSize)
	end := incident.EndTime

	for t := start; t.Before(end); t = t.Add(subWindow) {
		windows = append(windows, models.TimeWindow{
			Start: t,
			End:   t.Add(subWindow),
		})
	}
	return windows
}

func bucketLogsByTime(logs []models.LogEntry, windows []models.TimeWindow) map[int][]models.LogEntry {
	result := make(map[int][]models.LogEntry)
	for _, log := range logs {
		for i, window := range windows {
			if (log.Timestamp.Equal(window.Start) || log.Timestamp.After(window.Start)) &&
				log.Timestamp.Before(window.End) {
				result[i] = append(result[i], log)
				break
			}
		}
	}
	return result
}

func bucketTracesByTime(traces []models.TraceSummary, windows []models.TimeWindow) map[int][]models.TraceSummary {
	result := make(map[int][]models.TraceSummary)
	for _, trace := range traces {
		for i, window := range windows {
			if (trace.StartTime.Equal(window.Start) || trace.StartTime.After(window.Start)) &&
				trace.StartTime.Before(window.End) {
				result[i] = append(result[i], trace)
				break
			}
		}
	}
	return result
}

// metricTrendKeySeparator joins a service and metric name in the MetricTrends
// map key. The NUL byte cannot appear in either component, so it is a safe,
// unambiguous delimiter that also sorts deterministically.
const metricTrendKeySeparator = "\x00"

// metricTrendKey builds a MetricTrends key that distinguishes the same metric
// name across services (service is empty for the global fallback).
func metricTrendKey(service, metricName string) string {
	return service + metricTrendKeySeparator + metricName
}

// splitMetricTrendKey splits a MetricTrends key back into service and metric
// name. Legacy/bare-name keys (no separator) yield an empty service.
func splitMetricTrendKey(key string) (service, metricName string) {
	if idx := strings.Index(key, metricTrendKeySeparator); idx >= 0 {
		return key[:idx], key[idx+len(metricTrendKeySeparator):]
	}
	return "", key
}

func calculateMetricTrends(metrics []models.MetricSeries, windows []models.TimeWindow) map[string]map[int]models.Trend {
	result := make(map[string]map[int]models.Trend)
	for _, metric := range metrics {
		trends := make(map[int]models.Trend)
		result[metricTrendKey(metric.Labels["service"], metric.Name)] = trends
		for i := 0; i < len(windows)-1; i++ {
			avg1 := getAverageMetricValue(metric, windows[i])
			avg2 := getAverageMetricValue(metric, windows[i+1])
			change := 0.0
			if avg1 != 0 {
				change = (avg2 - avg1) / avg1
			}
			switch {
			case change > 0.1:
				trends[i] = models.TrendUp
			case change < -0.1:
				trends[i] = models.TrendDown
			default:
				trends[i] = models.TrendStable
			}
		}
	}
	return result
}

func getAverageMetricValue(metric models.MetricSeries, window models.TimeWindow) float64 {
	sum := 0.0
	count := 0
	for _, point := range metric.DataPoints {
		if (point.Timestamp.Equal(window.Start) || point.Timestamp.After(window.Start)) &&
			point.Timestamp.Before(window.End) {
			sum += point.Value
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// generateHypotheses creates candidate root cause hypotheses.
func (e *Engine) generateHypotheses(ctx context.Context, signals *models.Signals, normalized *models.NormalizedSignals, incident *models.Incident) []models.Hypothesis {
	candidates := []models.Hypothesis{}

	// Error spike hypotheses — diagnostic, not just "N errors": the description
	// carries the spike ratio and the evidence carries distinct sample messages.
	// When a resource failure (k8s event) shares a service name with the failing
	// service, surface the causal link ("correlates with ...") — an error spike
	// is a symptom, the resource failure is a candidate cause.
	for service, logs := range groupLogsByService(normalized.LogByWindow) {
		ok, desc, samples := describeErrorSpike(logs, normalized, incident)
		if !ok {
			continue
		}
		if corr := correlatedResourceFailures(service, normalized.Changes); len(corr) > 0 {
			desc += fmt.Sprintf(" — correlates with Kubernetes resource failure: %s", corr[0].Description)
		}
		evidence := []string{desc}
		evidence = append(evidence, samples...)
		candidates = append(candidates, models.Hypothesis{
			Type:        models.HypothesisTypeErrorSpike,
			Service:     service,
			Description: fmt.Sprintf("Service %s: %s", service, desc),
			Evidence:    evidence,
		})
	}

	// Metric anomaly hypotheses — diagnostic: real timestamps, direction
	// counts and window averages instead of opaque "Window N" indices (whose
	// order was also map-random, so evidence lines shuffled between runs).
	// Iterate in sorted key order so candidate generation (and therefore the
	// evidence order) is deterministic across runs.
	metricKeys := make([]string, 0, len(normalized.MetricTrends))
	for key := range normalized.MetricTrends {
		metricKeys = append(metricKeys, key)
	}
	sort.Strings(metricKeys)
	for _, key := range metricKeys {
		trends := normalized.MetricTrends[key]
		if !hasAnomalousTrend(trends, normalized, incident) {
			continue
		}
		service, metricName := splitMetricTrendKey(key)
		desc, evidence := describeMetricAnomaly(service, metricName, trends, normalized.TimeWindows, signals.Metrics)
		candidates = append(candidates, models.Hypothesis{
			Type:        models.HypothesisTypeMetricAnomaly,
			Service:     service,
			Description: desc,
			Metric:      metricName,
			Evidence:    evidence,
		})
	}

	// WAF attack hypothesis: the alarm window's own WAF logs are direct
	// evidence of the alarmed phenomenon (CC/Web attack in progress) — the
	// only dimension that can actually explain a WAF alarm.
	if normalized.WAFFacts != nil && normalized.WAFFacts.Total > 0 {
		f := normalized.WAFFacts
		candidates = append(candidates, models.Hypothesis{
			Type:        models.HypothesisTypeWAFAttack,
			Service:     f.Domain,
			Description: f.SummaryLine(),
			Evidence:    f.EvidenceLines(),
		})
	}

	// Change hypotheses (deployments/config/scale — NOT k8s events)
	for _, change := range normalized.Changes {
		if change.Type == "k8s_event" {
			continue
		}
		if isChangeRelevant(change, incident) {
			candidates = append(candidates, models.Hypothesis{
				Type:        models.HypothesisTypeChange,
				Service:     change.Service,
				Description: fmt.Sprintf("Recent change detected: %s", change.Description),
				ChangeID:    change.ID,
				Evidence:    []string{change.Description, change.Timestamp.Format(time.RFC3339)},
			})
		}
	}

	// K8s resource-failure hypotheses — aggregated by (namespace, kind, reason)
	// so N near-identical pod events collapse into ONE hypothesis (count + a
	// sample object) instead of flooding the candidate list per pod.
	type resourceGroupKey struct{ namespace, kind, reason string }
	resourceGroups := map[resourceGroupKey][]models.Change{}
	// Downstream scope: the resolved entry service(s) PLUS the domain's
	// business-unit token (e.g. "smt" for jlc-smt.com). The token captures the
	// gateway's downstream services without a per-service log fan-out, and is
	// only used when specific (>=3 chars) — "gw" from cut.jlc-gw.com is too
	// generic and skipped.
	buToken := models.ExtractBusinessUnitToken(incident.Description)
	scopeToServices := len(incident.AffectedServices) > 0 || len(buToken) >= 3
	for _, change := range normalized.Changes {
		if change.Type != "k8s_event" || !isResourceFailure(change) {
			continue
		}
		if !isChangeRelevant(change, incident) {
			continue
		}
		// Chronic conditions (a DaemonSet pod with a broken probe refreshes
		// last_seen forever) are background noise, not incident triggers.
		if isChronicEvent(change, incident, e.windowSize) {
			continue
		}
		// When the incident names its backend (a domain resolved via SLB, or an
		// explicit service list), only surface k8s failures of THOSE services or
		// their business-unit downstream — unrelated namespaces' Pod failures
		// are fresh-but-irrelevant.
		if scopeToServices && !eventMatchesScope(change, incident.AffectedServices, buToken) {
			continue
		}
		key := resourceGroupKey{
			namespace: change.Details["namespace"],
			kind:      change.Details["kind"],
			reason:    change.Details["reason"],
		}
		resourceGroups[key] = append(resourceGroups[key], change)
	}

	// A namespace with teardown/startup events alongside the failure is mid-
	// rollout: say so directly instead of the generic "resource failure", which
	// on-call reads as an outage when it is actually a redeploy.
	rolloutNamespaces := map[string]bool{}
	for _, change := range normalized.Changes {
		if change.Type == "k8s_event" && rolloutReasons[change.Details["reason"]] {
			rolloutNamespaces[change.Details["namespace"]] = true
		}
	}

	for key, changes := range resourceGroups {
		rep := changes[0]
		label := "Kubernetes resource failure"
		if rolloutNamespaces[key.namespace] {
			label = "滚动发布/重启"
		}
		desc := fmt.Sprintf("%s: %d %s %s in %s (e.g. %s)",
			label, len(changes), key.kind, key.reason, key.namespace, rep.Service)
		candidates = append(candidates, models.Hypothesis{
			Type:        models.HypothesisTypeResource,
			Service:     rep.Service,
			Description: desc,
			ChangeID:    rep.ID,
			Evidence:    []string{desc, rep.Timestamp.Format(time.RFC3339)},
		})
	}

	return candidates
}

func groupLogsByService(logByWindow map[int][]models.LogEntry) map[string][]models.LogEntry {
	result := make(map[string][]models.LogEntry)
	for _, logs := range logByWindow {
		for _, log := range logs {
			result[log.Service] = append(result[log.Service], log)
		}
	}
	return result
}

// describeErrorSpike reports whether a service had a significant error spike in
// the incident window, and if so, returns a diagnostic description (spike ratio
// + incident/baseline counts) plus up to 3 distinct sample error messages — so
// the hypothesis says WHAT failed, not just "N errors".
//
// Detection uses THIS service's logs (per-service attribution); reading the
// shared LogByWindow made one noisy service raise spikes for every other service.
func describeErrorSpike(logs []models.LogEntry, normalized *models.NormalizedSignals, incident *models.Incident) (bool, string, []string) {
	byWindow := bucketLogsByTime(logs, normalized.TimeWindows)

	incidentCount := 0
	baselineCount := 0
	var incidentLogs []models.LogEntry
	for i, window := range normalized.TimeWindows {
		isIncidentWindow := window.End.After(incident.StartTime) && window.Start.Before(incident.EndTime)
		if isIncidentWindow {
			incidentCount += len(byWindow[i])
			incidentLogs = append(incidentLogs, byWindow[i]...)
		} else {
			baselineCount += len(byWindow[i])
		}
	}
	if incidentCount <= baselineCount*2 || incidentCount <= 10 {
		return false, "", nil
	}

	var desc string
	if baselineCount > 0 {
		ratio := float64(incidentCount) / float64(baselineCount)
		desc = fmt.Sprintf("error rate spiked %.1f× (baseline %d → %d errors in incident window)",
			ratio, baselineCount, incidentCount)
	} else {
		// Zero baseline: a finite ratio is meaningless (and would lie as "1.0×").
		desc = fmt.Sprintf("errors appeared in the incident window (baseline 0 → %d errors)", incidentCount)
	}
	return true, desc, distinctMessages(incidentLogs, 3)
}

// distinctMessages extracts up to n distinct, truncated non-empty log messages.
func distinctMessages(logs []models.LogEntry, n int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, n)
	for _, l := range logs {
		msg := strings.TrimSpace(l.Message)
		if msg == "" || seen[msg] {
			continue
		}
		seen[msg] = true
		if len(msg) > 120 {
			msg = msg[:120] + "…"
		}
		out = append(out, msg)
		if len(out) >= n {
			break
		}
	}
	return out
}

func hasAnomalousTrend(trends map[int]models.Trend, normalized *models.NormalizedSignals, incident *models.Incident) bool {
	startIdx := len(normalized.TimeWindows) - 3
	if startIdx < 0 {
		startIdx = 0
	}
	for i := startIdx; i < len(normalized.TimeWindows); i++ {
		if trend, ok := trends[i]; ok && (trend == models.TrendUp || trend == models.TrendDown) {
			return true
		}
	}
	return false
}

// describeMetricAnomaly builds a diagnostic description + evidence for one
// metric's anomalous trend: direction counts in the description, and the most
// recent anomalous windows as evidence lines with real timestamps and average
// values (capped at 5, chronological — the old "Window 44: trending up" was
// opaque to on-call and shuffled between runs).
func describeMetricAnomaly(service, metricName string, trends map[int]models.Trend, windows []models.TimeWindow, series []models.MetricSeries) (string, []string) {
	var ms *models.MetricSeries
	for i := range series {
		if series[i].Name != metricName {
			continue
		}
		// When the candidate is service-scoped, join on the owning service so
		// the evidence averages come from the right series. The global fallback
		// (empty service) keeps the original name-only join.
		if service == "" || series[i].Labels["service"] == service {
			ms = &series[i]
			break
		}
	}

	idxs := make([]int, 0, len(trends))
	ups, downs := 0, 0
	for i, t := range trends {
		switch t {
		case models.TrendUp:
			ups++
			idxs = append(idxs, i)
		case models.TrendDown:
			downs++
			idxs = append(idxs, i)
		}
	}
	sort.Ints(idxs)

	desc := fmt.Sprintf("Metric %s anomalous: %d rising / %d falling windows", metricName, ups, downs)

	const maxLines = 5
	evidence := make([]string, 0, maxLines+1)
	if len(idxs) > maxLines {
		evidence = append(evidence, fmt.Sprintf("(另有 %d 个更早的异常窗口)", len(idxs)-maxLines))
		idxs = idxs[len(idxs)-maxLines:]
	}
	unit := ""
	if ms != nil {
		unit = ms.Unit
	}
	for _, i := range idxs {
		if i < 0 || i >= len(windows) {
			continue
		}
		w := windows[i]
		dir := "rising"
		if trends[i] == models.TrendDown {
			dir = "falling"
		}
		line := fmt.Sprintf("%s~%s %s", w.Start.Format("01-02 15:04"), w.End.Format("01-02 15:04"), dir)
		if ms != nil {
			if avg := getAverageMetricValue(*ms, w); avg != 0 {
				line += fmt.Sprintf(" (avg %.2f%s)", avg, unit)
			}
		}
		evidence = append(evidence, line)
	}
	if len(evidence) == 0 {
		evidence = append(evidence, desc)
	}
	return desc, evidence
}

func isChangeRelevant(change models.Change, incident *models.Incident) bool {
	return !change.Timestamp.Before(incident.StartTime.Add(-15*time.Minute)) &&
		!change.Timestamp.After(incident.EndTime)
}

// resourceFailureReasons are K8s event reasons that indicate an infrastructure
// resource failure — a distinct root-cause class from deployment/config changes.
var resourceFailureReasons = map[string]bool{
	"OOMKilled":              true,
	"Unhealthy":              true,
	"Evicted":                true,
	"FailedScheduling":       true,
	"CrashLoopBackOff":       true,
	"BackOff":                true,
	"NodeNotReady":           true,
	"NodePressure":           true,
	"ContainerStatusUnknown": true,
}

// rolloutReasons are K8s event reasons that mark a deployment/rollout (pod
// lifecycle transitions): Killing/SuccessfulDelete tear old pods down while
// Created/Pulled/Started bring new ones up. A namespace carrying these
// alongside Unhealthy is a rolling restart, not a spontaneous failure.
var rolloutReasons = map[string]bool{
	"Killing":           true,
	"SuccessfulDelete":  true,
	"SuccessfulRescale": true,
	"ScalingReplicaSet": true,
	"Created":           true,
	"Pulled":            true,
	"Started":           true,
}

// isResourceFailure reports whether a k8s_event change carries a resource-failure reason.
func isResourceFailure(change models.Change) bool {
	return resourceFailureReasons[change.Details["reason"]]
}

// isChronicEvent reports whether a k8s event is a long-standing condition
// rather than a fresh trigger. Repeating events keep their last_seen fresh
// forever, so EVERY alarm window "contains" them — without this check a
// permanent background noise (e.g. cmonitor-agent pods failing the same
// readiness probe for months) becomes the top root-cause candidate of every
// incident. first_seen is the discriminator: a condition that first appeared
// before the analysis window predates the incident and cannot have triggered
// it. Events without first_seen keep the old last_seen behavior.
func isChronicEvent(change models.Change, incident *models.Incident, lookback time.Duration) bool {
	ms, err := strconv.ParseInt(change.Details["first_seen"], 10, 64)
	if err != nil || ms <= 0 {
		return false
	}
	return time.UnixMilli(ms).Before(incident.StartTime.Add(-lookback))
}

// nameMatches reports whether a log `service` name is the "-"-bounded prefix of
// a k8s involved object name (e.g. "efs-receivable" matches
// "efs-receivable-2193-798986df65-tqjqn"). This is the pragmatic link between a
// service identity and its pod/deployment name, avoiding loose substring hits
// (e.g. "machine" must NOT match "machine-manufacturing-service" unless the full
// service name is the prefix).
func nameMatches(service, object string) bool {
	service = strings.ToLower(strings.TrimSpace(service))
	object = strings.ToLower(strings.TrimSpace(object))
	if service == "" || object == "" {
		return false
	}
	return object == service || strings.HasPrefix(object, service+"-")
}

// hasBackendSignal reports whether any surviving candidate is a backend signal
// (error spike / resource failure / change) that localizes the failure. Metric
// anomalies are infra-wide and do not identify a specific backend.
func hasBackendSignal(ranked []models.ScoredHypothesis) bool {
	for _, s := range ranked {
		switch s.Hypothesis.Type {
		case models.HypothesisTypeErrorSpike, models.HypothesisTypeResource, models.HypothesisTypeChange:
			return true
		}
	}
	return false
}

// eventMatchesScope reports whether a k8s event belongs to the scoped services
// OR the domain's business-unit token (the gateway's downstream services, e.g.
// "smt" -> "smt-search-system"). The token is the pragmatic downstream link when
// the topology does not cover the entry gateway.
func eventMatchesScope(change models.Change, services []string, buToken string) bool {
	if eventMatchesAffected(change, services) {
		return true
	}
	if len(buToken) < 3 {
		return false
	}
	haystack := strings.ToLower(change.Service + " " + change.Details["namespace"] + " " + change.Details["object"])
	return strings.Contains(haystack, strings.ToLower(buToken))
}

// eventMatchesAffected reports whether a k8s event's involved object is a
// "-"-bounded prefix match of any affected service (e.g. "electronic-access-
// gateway" matches "electronic-access-gateway-26-788bc57f4f").
func eventMatchesAffected(change models.Change, services []string) bool {
	for _, svc := range services {
		if svc == "" {
			continue
		}
		if nameMatches(svc, change.Service) || nameMatches(svc, change.Details["object"]) {
			return true
		}
	}
	return false
}

// correlatedResourceFailures returns resource-failure k8s events whose involved
// object matches the given service name, most-specific (longest object) first.
func correlatedResourceFailures(service string, changes []models.Change) []models.Change {
	var out []models.Change
	for _, c := range changes {
		if c.Type != "k8s_event" || !isResourceFailure(c) {
			continue
		}
		if nameMatches(service, c.Service) || nameMatches(service, c.Details["object"]) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return len(out[i].Service) > len(out[j].Service)
	})
	return out
}

// scoreCandidates applies multi-dimensional scoring.
func (e *Engine) scoreCandidates(ctx context.Context, candidates []models.Hypothesis, normalized *models.NormalizedSignals, incident *models.Incident) []models.ScoredHypothesis {
	scorer := scoring.NewMultiDimensionalScorer(e.weights)
	scored := make([]models.ScoredHypothesis, 0, len(candidates))
	for _, candidate := range candidates {
		score := scorer.Score(ctx, &candidate, normalized, incident)
		scored = append(scored, models.ScoredHypothesis{
			Hypothesis: candidate,
			Score:      score.Total,
			Confidence: score.Confidence,
			Breakdown:  score.Breakdown,
		})
	}
	return scored
}

// rankAndFilter sorts candidates by score and filters by confidence threshold.
// It also applies a SOFT business-unit scope: only narrow to the domain's BU
// when the token is specific AND actually matches a surviving candidate.
// A short/generic token ("gw" out of cut.jlc-gw.com) matches nothing, so
// narrowing then would empty the report — the full list is kept instead.
func (e *Engine) rankAndFilter(scored []models.ScoredHypothesis, minConfidence float64, incident *models.Incident) []models.ScoredHypothesis {
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	filtered := make([]models.ScoredHypothesis, 0)
	for _, s := range scored {
		if s.Confidence >= minConfidence {
			filtered = append(filtered, s)
		}
	}

	if token := models.ExtractBusinessUnitToken(incident.Description); token != "" &&
		len(token) >= 3 && !models.IsWAFAlarm(incident.Description) {
		scoped := make([]models.ScoredHypothesis, 0, len(filtered))
		for _, s := range filtered {
			haystack := strings.ToLower(s.Hypothesis.Service + " " + s.Hypothesis.Description)
			if strings.Contains(haystack, token) {
				scoped = append(scoped, s)
			}
		}
		if len(scoped) > 0 {
			filtered = scoped
		}
	}

	return filtered
}

func getTopCandidate(ranked []models.ScoredHypothesis) *models.ScoredHypothesis {
	if len(ranked) == 0 {
		return nil
	}
	return &ranked[0]
}

func getOverallConfidence(ranked []models.ScoredHypothesis) float64 {
	if len(ranked) == 0 {
		return 0
	}
	if len(ranked) >= 2 {
		return ranked[0].Confidence*0.7 + ranked[1].Confidence*0.3
	}
	return ranked[0].Confidence
}

func getAnalysisWindow(incident *models.Incident, windowSize time.Duration) models.TimeWindow {
	return models.TimeWindow{
		Start: incident.StartTime.Add(-windowSize),
		End:   incident.EndTime,
	}
}
