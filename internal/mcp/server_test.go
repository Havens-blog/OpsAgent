package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
)

// fakeTopologySource is a test double for sources.TopologySource that returns
// exactly what the test hands it — no implicit data.
type fakeTopologySource struct {
	topo *models.Topology
	err  error
}

func (f *fakeTopologySource) GetTopology(ctx context.Context) (*models.Topology, error) {
	return f.topo, f.err
}

func (f *fakeTopologySource) GetServiceNeighbors(ctx context.Context, service string) ([]string, []string, error) {
	return nil, nil, f.err
}

func (f *fakeTopologySource) GetServiceHealth(ctx context.Context) (map[string]string, error) {
	return nil, f.err
}

// fabricatedNames are the node names the old implementation hard-coded. No
// reachable code path may ever emit them again.
var fabricatedNames = []string{
	"api-gateway", "order-service", "user-service",
	"payment-service", "postgres-order", "redis-cache",
}

// forbiddenPhrases are the fake-success / placeholder phrases the old
// metrics/logs/RCA handlers emitted. No tool output may contain them again:
// a call either reports real data or reports honestly that it has none.
var forbiddenPhrases = []string{
	"executed successfully",
	"analysis in progress",
	"found 0 log entries",
	"log entries matching the query",
	"hypotheses analysis",
}

// testTopology is a small but non-trivial graph:
//
//	checkout-api -> billing-svc -> ledger-db
//	       \-> session-cache
//	edge-svc     -> checkout-api
func testTopology() *models.Topology {
	return &models.Topology{
		Nodes: []models.ServiceNode{
			{Name: "ledger-db", Type: "database"},
			{Name: "checkout-api", Type: "application"},
			{Name: "session-cache", Type: "cache"},
			{Name: "billing-svc", Type: "application"},
			{Name: "edge-svc", Type: "application"},
		},
		Edges: []models.DependencyEdge{
			{From: "checkout-api", To: "billing-svc", Type: "rpc"},
			{From: "checkout-api", To: "session-cache", Type: "cache"},
			{From: "billing-svc", To: "ledger-db", Type: "database"},
			{From: "edge-svc", To: "checkout-api", Type: "http"},
		},
	}
}

// responseText extracts the text content of a tool response.
func responseText(t *testing.T, resp *ToolResponse) string {
	t.Helper()
	if resp == nil {
		t.Fatal("nil tool response")
	}
	if len(resp.Content) != 1 {
		t.Fatalf("expected exactly 1 content block, got %d", len(resp.Content))
	}
	block, ok := resp.Content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("content block is not a map: %T", resp.Content[0])
	}
	text, ok := block["text"].(string)
	if !ok {
		t.Fatalf("content block has no text: %v", block)
	}
	return text
}

// assertNoFabricatedData fails when the output contains any hard-coded node
// name from the old fake tree, or a fake-success / placeholder phrase.
func assertNoFabricatedData(t *testing.T, text string) {
	t.Helper()
	for _, name := range fabricatedNames {
		if strings.Contains(text, name) {
			t.Errorf("output must not contain fabricated node %q:\n%s", name, text)
		}
	}
	lower := strings.ToLower(text)
	for _, phrase := range forbiddenPhrases {
		if strings.Contains(lower, phrase) {
			t.Errorf("output must not contain fake-success phrase %q:\n%s", phrase, text)
		}
	}
	if strings.Contains(lower, "successfully") {
		t.Errorf("output must not claim success:\n%s", text)
	}
}

func TestHandleTopologyGet(t *testing.T) {
	topoErr := errors.New("arms apm unreachable: dial tcp 10.0.0.1:443")

	tests := []struct {
		name     string
		source   func() (sources.TopologySource, bool)
		args     map[string]interface{}
		want     []string
		notWant  []string
		isError  bool
		noFabric bool
	}{
		{
			name: "real source, service neighbourhood is one-hop and directional",
			source: func() (sources.TopologySource, bool) {
				return &fakeTopologySource{topo: testTopology()}, true
			},
			args: map[string]interface{}{"service": "checkout-api"},
			want: []string{
				"服务拓扑（一跳直接关系）：checkout-api",
				"上游调用方（调用 checkout-api，共 1 个）",
				"- edge-svc (http)",
				"下游依赖（checkout-api 调用，共 2 个）",
				"- billing-svc (rpc)",
				"- session-cache (cache)",
			},
			// Two-hop nodes must not leak into a one-hop answer.
			notWant:  []string{"ledger-db"},
			noFabric: true,
		},
		{
			name: "real source, service with no relations reports empty directions",
			source: func() (sources.TopologySource, bool) {
				return &fakeTopologySource{topo: testTopology()}, true
			},
			args: map[string]interface{}{"service": "edge-svc"},
			want: []string{
				"服务拓扑（一跳直接关系）：edge-svc",
				"上游调用方（调用 edge-svc，共 0 个）",
				"下游依赖（edge-svc 调用，共 1 个）",
				"- checkout-api (http)",
			},
			noFabric: true,
		},
		{
			name: "real source, whole-graph summary counts",
			source: func() (sources.TopologySource, bool) {
				return &fakeTopologySource{topo: testTopology()}, true
			},
			args: map[string]interface{}{},
			want: []string{
				"节点总数: 5",
				"节点类型分布:",
				"application: 3",
				"cache: 1",
				"database: 1",
				"边总数: 4",
				"checkout-api -> billing-svc (rpc)",
				"billing-svc -> ledger-db (database)",
				"checkout-api -> session-cache (cache)",
				"edge-svc -> checkout-api (http)",
			},
			noFabric: true,
		},
		{
			name: "real source, unknown service is reported as not found",
			source: func() (sources.TopologySource, bool) {
				return &fakeTopologySource{topo: testTopology()}, true
			},
			args:     map[string]interface{}{"service": "no-such-svc"},
			want:     []string{`未找到服务 "no-such-svc" 的拓扑数据`},
			noFabric: true,
		},
		{
			name: "unconfigured source (nil)",
			source: func() (sources.TopologySource, bool) {
				return nil, false
			},
			args:     map[string]interface{}{"service": "checkout-api"},
			want:     []string{"未配置", "ARMS_APM_ENDPOINT", "ARMS_APM_TOKEN", "GET /api/v1/topology"},
			noFabric: true,
		},
		{
			name: "unconfigured source (empty fallback, real=false)",
			source: func() (sources.TopologySource, bool) {
				return &fakeTopologySource{topo: &models.Topology{}}, false
			},
			args:     map[string]interface{}{},
			want:     []string{"未配置"},
			noFabric: true,
		},
		{
			name: "source error is surfaced honestly",
			source: func() (sources.TopologySource, bool) {
				return &fakeTopologySource{err: topoErr}, true
			},
			args:     map[string]interface{}{},
			want:     []string{"拓扑查询失败", topoErr.Error()},
			notWant:  []string{"节点总数", "边总数"},
			isError:  true,
			noFabric: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, real := tc.source()
			srv := &Server{}
			if src != nil {
				srv.SetTopologySource(src, real)
			}

			resp, err := srv.CallTool(context.Background(), ToolCall{
				Name:      "opsagent_topology_get",
				Arguments: tc.args,
			})
			if err != nil {
				t.Fatalf("CallTool returned error: %v", err)
			}
			if resp.IsError != tc.isError {
				t.Errorf("IsError = %v, want %v", resp.IsError, tc.isError)
			}

			text := responseText(t, resp)
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("output missing %q:\n%s", want, text)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(text, notWant) {
					t.Errorf("output must not contain %q:\n%s", notWant, text)
				}
			}
			if tc.noFabric {
				assertNoFabricatedData(t, text)
			}
		})
	}
}

// TestFormatTopologySummaryIsBounded pins the truncation behaviour so a large
// graph cannot dump thousands of edges into the tool result.
func TestFormatTopologySummaryIsBounded(t *testing.T) {
	topo := &models.Topology{}
	for i := 0; i < 60; i++ {
		topo.Nodes = append(topo.Nodes, models.ServiceNode{
			Name: fmt.Sprintf("svc-%02d", i),
			Type: "application",
		})
		topo.Edges = append(topo.Edges, models.DependencyEdge{
			From: fmt.Sprintf("svc-%02d", i),
			To:   fmt.Sprintf("svc-%02d", (i+1)%60),
			Type: "rpc",
		})
	}

	text := formatTopology(topo, "")
	if !strings.Contains(text, "节点总数: 60") || !strings.Contains(text, "边总数: 60") {
		t.Fatalf("summary counts wrong:\n%s", text)
	}
	if !strings.Contains(text, "... 其余 40 条已省略") {
		t.Fatalf("truncation notice missing:\n%s", text)
	}
	if got := strings.Count(text, "->"); got != maxTopologyEdgesShown {
		t.Fatalf("rendered %d edges, want %d", got, maxTopologyEdgesShown)
	}
}

// TestHandleTopologyGetEmptyRealGraph keeps the "configured but no data" case
// distinct from "not configured".
func TestHandleTopologyGetEmptyRealGraph(t *testing.T) {
	srv := &Server{}
	srv.SetTopologySource(&fakeTopologySource{topo: &models.Topology{}}, true)

	resp, err := srv.CallTool(context.Background(), ToolCall{Name: "opsagent_topology_get"})
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	text := responseText(t, resp)
	if !strings.Contains(text, "空拓扑图") {
		t.Errorf("expected an explicit empty-graph notice:\n%s", text)
	}
	if strings.Contains(text, "服务拓扑源未配置") || strings.Contains(text, "ARMS_APM_ENDPOINT") {
		t.Errorf("configured-but-empty must not be reported as unconfigured:\n%s", text)
	}
	assertNoFabricatedData(t, text)
}

// TestFormatTopologyGolden pins the exact rendered text of the two real-source
// shapes, so the sample output in the change description cannot drift silently.
func TestFormatTopologyGolden(t *testing.T) {
	t.Run("one-hop neighbourhood", func(t *testing.T) {
		want := "服务拓扑（一跳直接关系）：checkout-api\n" +
			"数据来源：已配置的真实拓扑源；仅列出直接相邻节点，不做多跳推断。\n" +
			"\n" +
			"上游调用方（调用 checkout-api，共 1 个）:\n" +
			"  - edge-svc (http)\n" +
			"\n" +
			"下游依赖（checkout-api 调用，共 2 个）:\n" +
			"  - billing-svc (rpc)\n" +
			"  - session-cache (cache)\n"
		if got := formatTopology(testTopology(), "checkout-api"); got != want {
			t.Errorf("one-hop output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("whole-graph summary", func(t *testing.T) {
		want := "服务拓扑摘要（数据源：已配置的真实拓扑源）\n" +
			"节点总数: 5\n" +
			"节点类型分布:\n" +
			"  application: 3\n" +
			"  cache: 1\n" +
			"  database: 1\n" +
			"边总数: 4\n" +
			"依赖边（按 from/to/type 排序，显示前 4 条）:\n" +
			"  billing-svc -> ledger-db (database)\n" +
			"  checkout-api -> billing-svc (rpc)\n" +
			"  checkout-api -> session-cache (cache)\n" +
			"  edge-svc -> checkout-api (http)"
		if got := formatTopology(testTopology(), ""); got != want {
			t.Errorf("summary output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// Metrics / logs / RCA tool doubles
// ---------------------------------------------------------------------------

// testStart/testEnd are the fixed query window every new case uses, so the
// rendered output is stable and comparable.
var (
	testStart = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	testEnd   = testStart.Add(time.Hour)
)

// fakeMetricSource is a test double for sources.MetricSource. It records the
// expression it was handed, so a test can PROVE the PromQL really reached the
// source (and that unconfigured paths never call it).
type fakeMetricSource struct {
	series *models.MetricSeries
	err    error

	calls     int
	gotQuery  string
	gotStart  time.Time
	gotEnd    time.Time
	gotFilter map[string]string
}

func (f *fakeMetricSource) QueryMetric(ctx context.Context, metricName string, start, end time.Time, filters map[string]string) (*models.MetricSeries, error) {
	f.calls++
	f.gotQuery, f.gotStart, f.gotEnd, f.gotFilter = metricName, start, end, filters
	if f.err != nil {
		return nil, f.err
	}
	return f.series, nil
}

// The remaining MetricSource methods exist only to satisfy the interface; the
// metrics tool must never call them, so they return nothing.

func (f *fakeMetricSource) QueryAnomalies(ctx context.Context, start, end time.Time) ([]models.MetricSeries, error) {
	return nil, f.err
}

func (f *fakeMetricSource) QueryMetricsForService(ctx context.Context, service string, start, end time.Time) ([]models.MetricSeries, error) {
	return nil, f.err
}

func (f *fakeMetricSource) GetAlerts(ctx context.Context) ([]sources.Alert, error) {
	return nil, f.err
}

// fakeLogSource is a test double for sources.LogSource.
type fakeLogSource struct {
	entries []models.LogEntry
	err     error

	calls      int
	gotService string
	gotStart   time.Time
	gotEnd     time.Time
}

func (f *fakeLogSource) QueryLogsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	f.calls++
	f.gotService, f.gotStart, f.gotEnd = service, start, end
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

func (f *fakeLogSource) QueryErrors(ctx context.Context, start, end time.Time) ([]models.LogEntry, error) {
	return nil, f.err
}

func (f *fakeLogSource) QueryErrorsByService(ctx context.Context, service string, start, end time.Time) ([]models.LogEntry, error) {
	return nil, f.err
}

func (f *fakeLogSource) GetLogCount(ctx context.Context, service string, start, end time.Time) (int64, error) {
	return 0, f.err
}

// fakeRCAEngine is a test double for the narrow rcaAnalyzer contract.
type fakeRCAEngine struct {
	report *models.RCAReport
	err    error

	calls       int
	gotIncident *models.Incident
}

func (f *fakeRCAEngine) Analyze(ctx context.Context, incident *models.Incident) (*models.RCAReport, error) {
	f.calls++
	f.gotIncident = incident
	if f.err != nil {
		return nil, f.err
	}
	return f.report, nil
}

// The doubles must satisfy the exact interfaces the server injects.
var (
	_ sources.MetricSource = (*fakeMetricSource)(nil)
	_ sources.LogSource    = (*fakeLogSource)(nil)
	_ rcaAnalyzer          = (*fakeRCAEngine)(nil)
)

// windowArgs returns tool arguments carrying the fixed test window.
func windowArgs(extra map[string]interface{}) map[string]interface{} {
	args := map[string]interface{}{
		"start_time": testStart.Format(time.RFC3339),
		"end_time":   testEnd.Format(time.RFC3339),
	}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

// testMetricSeries is a real-shaped series: unordered data points, so the
// "latest value" rendering has to pick the newest one itself.
func testMetricSeries() *models.MetricSeries {
	return &models.MetricSeries{
		Name:   "arms_app_errors_per_sec",
		Labels: map[string]string{"service": "checkout-api", "cloud": "arms"},
		Unit:   "unknown",
		DataPoints: []models.DataPoint{
			{Timestamp: testStart.Add(2 * time.Minute), Value: 4.5},
			{Timestamp: testStart.Add(3 * time.Minute), Value: 9.75},
			{Timestamp: testStart.Add(time.Minute), Value: 1.25},
		},
	}
}

// testLogEntries is a small, unordered, real-shaped log batch.
func testLogEntries() []models.LogEntry {
	return []models.LogEntry{
		{
			Timestamp: testStart.Add(3 * time.Minute),
			Service:   "checkout-api",
			Level:     "ERROR",
			Message:   "upstream ledger-db connection refused",
			TraceID:   "trace-2",
			IsError:   true,
			Labels:    map[string]string{"pod": "checkout-7f"},
		},
		{
			Timestamp: testStart.Add(time.Minute),
			Service:   "checkout-api",
			Level:     "INFO",
			Message:   "request received",
			Labels:    map[string]string{"pod": "checkout-7f"},
		},
		{
			Timestamp: testStart.Add(2 * time.Minute),
			Service:   "checkout-api",
			Level:     "WARN",
			Message:   "slow response from ledger-db",
			Labels:    map[string]string{"pod": "checkout-9a"},
		},
	}
}

// testRCAReport is a real-shaped report whose candidates arrive in the WRONG
// order, so the deterministic ranking has to reorder them.
func testRCAReport() *models.RCAReport {
	candidates := []models.ScoredHypothesis{
		{
			Hypothesis: models.Hypothesis{
				Type:        models.HypothesisTypeChange,
				Service:     "ledger-db",
				Description: "ledger-db deployment correlates with the incident window",
				ChangeID:    "deploy-77",
				Evidence:    []string{"change deploy-77 recorded inside the window", "error rate rose 4x after it"},
			},
			Score:      0.62,
			Confidence: 0.55,
			Breakdown:  models.ScoreBreakdown{TimeCorrelation: 0.9, ChangeProximity: 0.8},
		},
		{
			Hypothesis: models.Hypothesis{
				Type:        models.HypothesisTypeErrorSpike,
				Service:     "checkout-api",
				Description: "error spike on checkout-api",
				Metric:      "arms_app_errors_per_sec",
				Evidence:    []string{"3 error logs from checkout-api"},
			},
			Score:      0.88,
			Confidence: 0.71,
			Breakdown:  models.ScoreBreakdown{TimeCorrelation: 0.7, ErrorFrequency: 0.6},
		},
	}

	return &models.RCAReport{
		Incident:       &models.Incident{ID: "inc-crashloop-1"},
		Candidates:     candidates,
		TopCandidate:   &candidates[1],
		Confidence:     0.71,
		AnalysisWindow: models.TimeWindow{Start: testStart, End: testEnd},
		Duration:       1500 * time.Millisecond,
		GeneratedAt:    testEnd,
	}
}

// ---------------------------------------------------------------------------
// opsagent_metrics_query
// ---------------------------------------------------------------------------

func TestHandleMetricsQuery(t *testing.T) {
	const promQL = `topk(2, arms_app_errors_per_sec{service="checkout-api"})`
	metricErr := errors.New("prometheus query failed: dial tcp 10.0.0.9:443: i/o timeout")

	tests := []struct {
		name    string
		src     func() *fakeMetricSource
		real    bool
		args    map[string]interface{}
		want    []string
		notWant []string
		isError bool
		// wantCalls pins whether the injected source was really queried.
		wantCalls int
		wantQuery string
	}{
		{
			name:      "real source executes the PromQL and renders the returned series",
			src:       func() *fakeMetricSource { return &fakeMetricSource{series: testMetricSeries()} },
			real:      true,
			args:      windowArgs(map[string]interface{}{"query": promQL, "step": "2m"}),
			wantCalls: 1,
			wantQuery: promQL,
			want: []string{
				"指标查询结果（数据源：已配置的真实指标源）",
				"PromQL: " + promQL,
				"时间窗: 2026-01-02T03:04:05Z ~ 2026-01-02T04:04:05Z",
				"step 参数: 2m",
				"序列数: 1",
				"name=arms_app_errors_per_sec",
				"标签={cloud=arms, service=checkout-api}",
				"点数=3",
				"最新值=9.75 @ 2026-01-02T03:07:05Z",
				"若表达式命中多条序列，其余序列本接口无法返回",
			},
			notWant: []string{"executed successfully", "Query executed"},
		},
		{
			name:      "real source, empty result is reported as zero series",
			src:       func() *fakeMetricSource { return &fakeMetricSource{series: nil} },
			real:      true,
			args:      windowArgs(map[string]interface{}{"query": promQL}),
			wantCalls: 1,
			wantQuery: promQL,
			want:      []string{"序列数: 0", "空序列"},
			notWant:   []string{"executed successfully"},
		},
		{
			name:    "no source injected is reported as unconfigured",
			src:     nil,
			real:    false,
			args:    windowArgs(map[string]interface{}{"query": promQL}),
			want:    []string{"指标源未配置", "PROMETHEUS_ENDPOINT", "ARMS_PROMETHEUS_BEARER_TOKEN", "ARMS APM"},
			notWant: []string{"序列数", "executed successfully"},
		},
		{
			name:      "empty fallback source (real=false) is reported as unconfigured and never queried",
			src:       func() *fakeMetricSource { return &fakeMetricSource{series: testMetricSeries()} },
			real:      false,
			args:      windowArgs(map[string]interface{}{"query": promQL}),
			wantCalls: 0,
			want:      []string{"指标源未配置", "不会编造任何指标序列"},
			notWant:   []string{"序列数", "arms_app_errors_per_sec"},
		},
		{
			name:      "source error is surfaced honestly",
			src:       func() *fakeMetricSource { return &fakeMetricSource{err: metricErr} },
			real:      true,
			args:      windowArgs(map[string]interface{}{"query": promQL}),
			wantCalls: 1,
			wantQuery: promQL,
			want:      []string{"指标查询失败", metricErr.Error(), "也可能是查询结果为空"},
			notWant:   []string{"序列数", "executed successfully"},
			isError:   true,
		},
		{
			name:      "missing query is refused instead of faked",
			src:       func() *fakeMetricSource { return &fakeMetricSource{series: testMetricSeries()} },
			real:      true,
			args:      windowArgs(nil),
			wantCalls: 0,
			want:      []string{"缺少 query", "未查询任何指标源"},
			isError:   true,
		},
		{
			name:      "missing window falls back to the last hour and says so",
			src:       func() *fakeMetricSource { return &fakeMetricSource{series: testMetricSeries()} },
			real:      true,
			args:      map[string]interface{}{"query": promQL},
			wantCalls: 1,
			wantQuery: promQL,
			want:      []string{"start_time 未提供", "end_time 未提供", "请求参数说明"},
		},
		{
			name:      "inverted window is refused",
			src:       func() *fakeMetricSource { return &fakeMetricSource{series: testMetricSeries()} },
			real:      true,
			args:      windowArgs(map[string]interface{}{"query": promQL, "start_time": testEnd.Format(time.RFC3339), "end_time": testStart.Format(time.RFC3339)}),
			wantCalls: 0,
			want:      []string{"时间窗非法"},
			isError:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := &Server{}
			var fake *fakeMetricSource
			if tc.src != nil {
				fake = tc.src()
				srv.SetMetricSource(fake, tc.real)
			}

			resp, err := srv.CallTool(context.Background(), ToolCall{
				Name:      "opsagent_metrics_query",
				Arguments: tc.args,
			})
			if err != nil {
				t.Fatalf("CallTool returned error: %v", err)
			}
			if resp.IsError != tc.isError {
				t.Errorf("IsError = %v, want %v", resp.IsError, tc.isError)
			}

			text := responseText(t, resp)
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("output missing %q:\n%s", want, text)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(text, notWant) {
					t.Errorf("output must not contain %q:\n%s", notWant, text)
				}
			}
			assertNoFabricatedData(t, text)

			if fake == nil {
				return
			}
			if fake.calls != tc.wantCalls {
				t.Errorf("metric source calls = %d, want %d", fake.calls, tc.wantCalls)
			}
			if tc.wantQuery != "" {
				if fake.gotQuery != tc.wantQuery {
					t.Errorf("source was handed %q, want %q", fake.gotQuery, tc.wantQuery)
				}
				// Cases that reach the source supply the fixed window, so it
				// must have been handed exactly that. (The defaulting case
				// deliberately omits start_time and is checked separately.)
				if start, _ := tc.args["start_time"].(string); start != "" &&
					(!fake.gotStart.Equal(testStart) || !fake.gotEnd.Equal(testEnd)) {
					t.Errorf("source window = %s ~ %s, want %s ~ %s",
						fake.gotStart.Format(time.RFC3339), fake.gotEnd.Format(time.RFC3339),
						testStart.Format(time.RFC3339), testEnd.Format(time.RFC3339))
				}
				if len(fake.gotFilter) != 0 {
					t.Errorf("source filters = %v, want none (the PromQL must pass through unchanged)", fake.gotFilter)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// opsagent_log_search
// ---------------------------------------------------------------------------

func TestHandleLogSearch(t *testing.T) {
	logErr := errors.New("sls query failed: HTTP 403 signature mismatch")

	tests := []struct {
		name      string
		src       func() *fakeLogSource
		real      bool
		args      map[string]interface{}
		want      []string
		notWant   []string
		isError   bool
		wantCalls int
	}{
		{
			name:      "real source, no query returns every entry the source returned",
			src:       func() *fakeLogSource { return &fakeLogSource{entries: testLogEntries()} },
			real:      true,
			args:      windowArgs(map[string]interface{}{"service": "checkout-api"}),
			wantCalls: 1,
			want: []string{
				"日志查询结果（数据源：已配置的真实日志源）",
				"Service: checkout-api",
				"时间窗: 2026-01-02T03:04:05Z ~ 2026-01-02T04:04:05Z",
				"本地过滤: 未提供 query，返回该服务在时间窗内的全部日志条目（未做任何过滤）。",
				"日志源返回条目数: 3",
				"参与展示条目数: 3",
				"upstream ledger-db connection refused",
				"request received",
				"slow response from ledger-db",
			},
			notWant: []string{"Found", "本地过滤后条目数"},
			isError: false,
		},
		{
			name:      "real source, query filters locally and says so",
			src:       func() *fakeLogSource { return &fakeLogSource{entries: testLogEntries()} },
			real:      true,
			args:      windowArgs(map[string]interface{}{"query": "LEDGER-DB", "service": "checkout-api"}),
			wantCalls: 1,
			want: []string{
				"本地过滤 query: LEDGER-DB",
				"query 为本地过滤，日志源不支持自由文本查询",
				"日志源返回条目数: 3",
				"本地过滤后条目数: 2",
				"upstream ledger-db connection refused",
				"slow response from ledger-db",
			},
			notWant: []string{"request received"},
		},
		{
			name:      "real source, no local match is honest and is not an error",
			src:       func() *fakeLogSource { return &fakeLogSource{entries: testLogEntries()} },
			real:      true,
			args:      windowArgs(map[string]interface{}{"query": "zzz-no-such-text", "service": "checkout-api"}),
			wantCalls: 1,
			want: []string{
				"日志源返回条目数: 3",
				"本地过滤后条目数: 0",
				"没有一条在本地匹配该 query",
				"不是查询失败",
			},
			notWant: []string{"Found 0 log entries"},
		},
		{
			name:      "real source, empty window reports zero from the source",
			src:       func() *fakeLogSource { return &fakeLogSource{} },
			real:      true,
			args:      windowArgs(map[string]interface{}{"service": "checkout-api"}),
			wantCalls: 1,
			want: []string{
				"日志源返回条目数: 0",
				"返回了 0 条日志（真实返回结果，不是省略显示）",
			},
		},
		{
			name:      "missing service is refused because the log source has no free-text query",
			src:       func() *fakeLogSource { return &fakeLogSource{entries: testLogEntries()} },
			real:      true,
			args:      windowArgs(map[string]interface{}{"query": "connection refused"}),
			wantCalls: 0,
			want:      []string{"缺少 service", "不支持自由文本查询", "未查询任何日志源"},
			notWant:   []string{"日志源返回条目数"},
			isError:   true,
		},
		{
			name:    "no source injected is reported as unconfigured",
			src:     nil,
			real:    false,
			args:    windowArgs(map[string]interface{}{"query": "x", "service": "checkout-api"}),
			want:    []string{"日志源未配置", "SLS_ENDPOINT", "ECAM_LOGQUERY_ENDPOINT"},
			notWant: []string{"日志源返回条目数", "Found"},
		},
		{
			name:      "empty fallback source (real=false) is reported as unconfigured and never queried",
			src:       func() *fakeLogSource { return &fakeLogSource{entries: testLogEntries()} },
			real:      false,
			args:      windowArgs(map[string]interface{}{"query": "x", "service": "checkout-api"}),
			wantCalls: 0,
			want:      []string{"日志源未配置", "不会编造任何日志条目"},
			notWant:   []string{"日志源返回条目数", "connection refused"},
		},
		{
			name:      "source error is surfaced honestly",
			src:       func() *fakeLogSource { return &fakeLogSource{err: logErr} },
			real:      true,
			args:      windowArgs(map[string]interface{}{"query": "x", "service": "checkout-api"}),
			wantCalls: 1,
			want:      []string{"日志查询失败", logErr.Error()},
			notWant:   []string{"日志源返回条目数", "Found"},
			isError:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := &Server{}
			var fake *fakeLogSource
			if tc.src != nil {
				fake = tc.src()
				srv.SetLogSource(fake, tc.real)
			}

			resp, err := srv.CallTool(context.Background(), ToolCall{
				Name:      "opsagent_log_search",
				Arguments: tc.args,
			})
			if err != nil {
				t.Fatalf("CallTool returned error: %v", err)
			}
			if resp.IsError != tc.isError {
				t.Errorf("IsError = %v, want %v", resp.IsError, tc.isError)
			}

			text := responseText(t, resp)
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("output missing %q:\n%s", want, text)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(text, notWant) {
					t.Errorf("output must not contain %q:\n%s", notWant, text)
				}
			}
			assertNoFabricatedData(t, text)

			if fake != nil && fake.calls != tc.wantCalls {
				t.Errorf("log source calls = %d, want %d", fake.calls, tc.wantCalls)
			}
		})
	}
}

// TestHandleLogSearchOrderingAndBound pins the deterministic ordering and the
// display cap of the real log batch.
func TestHandleLogSearchOrderingAndBound(t *testing.T) {
	entries := make([]models.LogEntry, 0, 60)
	for i := 0; i < 60; i++ {
		entries = append(entries, models.LogEntry{
			Timestamp: testStart.Add(time.Duration(60-i) * time.Second),
			Service:   "checkout-api",
			Level:     "INFO",
			Message:   fmt.Sprintf("entry-%02d", i),
		})
	}

	srv := &Server{}
	srv.SetLogSource(&fakeLogSource{entries: entries}, true)

	resp, err := srv.CallTool(context.Background(), ToolCall{
		Name: "opsagent_log_search",
		Arguments: windowArgs(map[string]interface{}{
			"service": "checkout-api",
			"limit":   float64(200),
		}),
	})
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	text := responseText(t, resp)
	assertNoFabricatedData(t, text)

	if !strings.Contains(text, "日志源返回条目数: 60") {
		t.Fatalf("expected the real returned count:\n%s", text)
	}
	if !strings.Contains(text, "... 其余 10 条已省略") {
		t.Fatalf("expected a truncation notice:\n%s", text)
	}
	if got := strings.Count(text, "\n  ["); got != maxLogEntriesShown {
		t.Fatalf("rendered %d entries, want %d:\n%s", got, maxLogEntriesShown, text)
	}
	// entry-59 is the newest (smallest offset subtracted), so it must be last.
	if last := strings.LastIndex(text, "entry-59"); last < 0 || strings.Index(text, "entry-09") > last {
		t.Fatalf("entries are not in timestamp order:\n%s", text)
	}
}

// TestHandleLogSearchGolden pins the exact rendered text of a real filtered
// batch, so the documented sample output cannot drift silently.
func TestHandleLogSearchGolden(t *testing.T) {
	srv := &Server{}
	srv.SetLogSource(&fakeLogSource{entries: testLogEntries()}, true)

	resp, err := srv.CallTool(context.Background(), ToolCall{
		Name: "opsagent_log_search",
		Arguments: windowArgs(map[string]interface{}{
			"query":   "ledger-db",
			"service": "checkout-api",
		}),
	})
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}

	want := "日志查询结果（数据源：已配置的真实日志源）\n" +
		"Service: checkout-api\n" +
		"时间窗: 2026-01-02T03:04:05Z ~ 2026-01-02T04:04:05Z\n" +
		"本地过滤 query: ledger-db\n" +
		"说明：query 为本地过滤，日志源不支持自由文本查询——真实查询是按 service + 时间窗发起的，query 只在这批已返回的条目上做大小写不敏感的子串匹配（匹配范围：消息正文、级别、标签值）。\n" +
		"日志源返回条目数: 3\n" +
		"本地过滤后条目数: 2\n" +
		"展示前 2 条（按 时间/服务/级别/正文 排序）:\n" +
		"  [1] 2026-01-02T03:06:05Z [WARN] service=checkout-api slow response from ledger-db labels={pod=checkout-9a}\n" +
		"  [2] 2026-01-02T03:07:05Z [ERROR] service=checkout-api upstream ledger-db connection refused (IsError=true) trace=trace-2 labels={pod=checkout-7f}"

	if got := responseText(t, resp); got != want {
		t.Errorf("log search output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// ---------------------------------------------------------------------------
// opsagent_rca_analyze
// ---------------------------------------------------------------------------

func TestHandleRCAAnalyze(t *testing.T) {
	analyzeErr := errors.New("failed to gather signals: metric source unreachable")

	tests := []struct {
		name      string
		engine    func() *fakeRCAEngine
		args      map[string]interface{}
		want      []string
		notWant   []string
		isError   bool
		wantCalls int
	}{
		{
			name:      "real engine report is rendered with the real candidates",
			engine:    func() *fakeRCAEngine { return &fakeRCAEngine{report: testRCAReport()} },
			args:      windowArgs(map[string]interface{}{"incident_id": "inc-crashloop-1", "affected_services": []interface{}{"checkout-api"}}),
			wantCalls: 1,
			want: []string{
				"RCA 分析报告（引擎：MCP 已接入的真实 RCA 引擎）",
				"Incident ID: inc-crashloop-1",
				"时间窗: 2026-01-02T03:04:05Z ~ 2026-01-02T04:04:05Z",
				"受影响服务: checkout-api（共 1 个）",
				"引擎结论: Root cause: error spike on checkout-api",
				"置信度: 0.71",
				"候选根因数: 2",
				"类型=error_spike 服务=checkout-api 置信度=0.71 得分=0.88 证据数=1",
				"类型=change 服务=ledger-db 置信度=0.55 得分=0.62 证据数=2",
				"变更 ID: deploy-77",
				"指标: arms_app_errors_per_sec",
				"评分构成: 时间相关性=",
				"报告元数据",
			},
			notWant: []string{"Analysis in progress", "in progress"},
		},
		{
			name:      "candidate ranking is deterministic (score desc)",
			engine:    func() *fakeRCAEngine { return &fakeRCAEngine{report: testRCAReport()} },
			args:      windowArgs(map[string]interface{}{"incident_id": "inc-crashloop-1"}),
			wantCalls: 1,
			want:      []string{"候选根因数: 2"},
		},
		{
			name:      "missing window is defaulted and the defaults are reported",
			engine:    func() *fakeRCAEngine { return &fakeRCAEngine{report: testRCAReport()} },
			args:      map[string]interface{}{"incident_id": "inc-crashloop-1"},
			wantCalls: 1,
			want: []string{
				"start_time 未提供，已默认使用",
				"end_time 未提供，已默认使用",
				"affected_services 未提供（或为空数组）",
				"DetectedAt 由 MCP 服务在本次调用时生成",
				"Severity 留空",
			},
		},
		{
			name: "empty candidate list is reported as no hypothesis, not hidden",
			engine: func() *fakeRCAEngine {
				return &fakeRCAEngine{report: &models.RCAReport{Incident: &models.Incident{ID: "inc-empty"}}}
			},
			args:      windowArgs(map[string]interface{}{"incident_id": "inc-empty"}),
			wantCalls: 1,
			want: []string{
				"候选根因数: 0",
				"引擎未产出任何候选根因（不是省略显示）",
				"Unable to determine root cause",
			},
			notWant: []string{"类型="},
		},
		{
			name:      "engine returning a nil report is reported honestly",
			engine:    func() *fakeRCAEngine { return &fakeRCAEngine{report: nil} },
			args:      windowArgs(map[string]interface{}{"incident_id": "inc-nil"}),
			wantCalls: 1,
			want:      []string{"未产出报告", "nil report", "没有任何可展示的根因"},
			notWant:   []string{"候选根因数"},
			isError:   true,
		},
		{
			name:      "engine error is surfaced honestly",
			engine:    func() *fakeRCAEngine { return &fakeRCAEngine{err: analyzeErr} },
			args:      windowArgs(map[string]interface{}{"incident_id": "inc-boom"}),
			wantCalls: 1,
			want:      []string{"RCA 分析失败，未产出任何报告", analyzeErr.Error()},
			notWant:   []string{"候选根因数", "Analysis in progress"},
			isError:   true,
		},
		{
			name:      "missing incident_id is refused instead of faked",
			engine:    func() *fakeRCAEngine { return &fakeRCAEngine{report: testRCAReport()} },
			args:      windowArgs(nil),
			wantCalls: 0,
			want:      []string{"缺少 incident_id", "未执行任何分析"},
			notWant:   []string{"候选根因数"},
			isError:   true,
		},
		{
			name:      "inverted window is refused",
			engine:    func() *fakeRCAEngine { return &fakeRCAEngine{report: testRCAReport()} },
			args:      windowArgs(map[string]interface{}{"incident_id": "inc-x", "start_time": testEnd.Format(time.RFC3339), "end_time": testStart.Format(time.RFC3339)}),
			wantCalls: 0,
			want:      []string{"时间窗非法"},
			isError:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := &Server{}
			var fake *fakeRCAEngine
			if tc.engine != nil {
				fake = tc.engine()
				srv.SetRCAEngine(fake)
			}

			resp, err := srv.CallTool(context.Background(), ToolCall{
				Name:      "opsagent_rca_analyze",
				Arguments: tc.args,
			})
			if err != nil {
				t.Fatalf("CallTool returned error: %v", err)
			}
			if resp.IsError != tc.isError {
				t.Errorf("IsError = %v, want %v", resp.IsError, tc.isError)
			}

			text := responseText(t, resp)
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("output missing %q:\n%s", want, text)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(text, notWant) {
					t.Errorf("output must not contain %q:\n%s", notWant, text)
				}
			}
			assertNoFabricatedData(t, text)

			if fake != nil && fake.calls != tc.wantCalls {
				t.Errorf("engine calls = %d, want %d", fake.calls, tc.wantCalls)
			}
		})
	}
}

// TestHandleRCAAnalyzeWithoutEngine pins the "not wired" contract: it is an
// error, it names the REST endpoint, and nothing else is emitted.
func TestHandleRCAAnalyzeWithoutEngine(t *testing.T) {
	srv := &Server{}

	resp, err := srv.CallTool(context.Background(), ToolCall{
		Name: "opsagent_rca_analyze",
		Arguments: windowArgs(map[string]interface{}{
			"incident_id":       "inc-not-wired",
			"affected_services": []interface{}{"checkout-api"},
		}),
	})
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if !resp.IsError {
		t.Errorf("IsError = false, want true when no engine is wired")
	}

	text := responseText(t, resp)
	for _, want := range []string{"RCA 引擎未接入 MCP", "POST /api/v1/rca/analyze", "不会返回任何分析结果"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
	for _, notWant := range []string{"候选根因数", "置信度:", "Analysis in progress"} {
		if strings.Contains(text, notWant) {
			t.Errorf("output must not contain %q:\n%s", notWant, text)
		}
	}
	assertNoFabricatedData(t, text)
}

// TestHandleRCAAnalyzeBuildsIncident pins the incident actually handed to the
// engine: every field must come from the arguments or an explicitly reported
// default — never invented.
func TestHandleRCAAnalyzeBuildsIncident(t *testing.T) {
	fake := &fakeRCAEngine{report: testRCAReport()}
	srv := &Server{}
	srv.SetRCAEngine(fake)

	before := time.Now()
	resp, err := srv.CallTool(context.Background(), ToolCall{
		Name: "opsagent_rca_analyze",
		Arguments: windowArgs(map[string]interface{}{
			"incident_id":       "inc-fields",
			"affected_services": []interface{}{"checkout-api", "", "ledger-db"},
		}),
	})
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected IsError:\n%s", responseText(t, resp))
	}

	if fake.gotIncident == nil {
		t.Fatal("engine was never handed an incident")
	}
	got := fake.gotIncident
	if got.ID != "inc-fields" {
		t.Errorf("Incident.ID = %q, want %q", got.ID, "inc-fields")
	}
	if !got.StartTime.Equal(testStart) || !got.EndTime.Equal(testEnd) {
		t.Errorf("Incident window = %s ~ %s, want %s ~ %s",
			got.StartTime.Format(time.RFC3339), got.EndTime.Format(time.RFC3339),
			testStart.Format(time.RFC3339), testEnd.Format(time.RFC3339))
	}
	if want := []string{"checkout-api", "ledger-db"}; strings.Join(got.AffectedServices, ",") != strings.Join(want, ",") {
		t.Errorf("AffectedServices = %v, want %v (empty strings must be dropped)", got.AffectedServices, want)
	}
	if got.Source != "mcp" {
		t.Errorf("Incident.Source = %q, want %q", got.Source, "mcp")
	}
	if got.Severity != "" {
		t.Errorf("Incident.Severity = %q, want empty (the tool has no severity argument)", got.Severity)
	}
	if got.Labels["status"] != "running" {
		t.Errorf("Incident.Labels = %v, want status=running", got.Labels)
	}
	if got.DetectedAt.Before(before) || got.DetectedAt.After(time.Now()) {
		t.Errorf("DetectedAt = %s, want a timestamp taken during this call", got.DetectedAt.Format(time.RFC3339))
	}
	if err := got.Validate(); err != nil {
		t.Errorf("constructed incident must be valid: %v", err)
	}
}

// TestHandleRCAAnalyzeRankingIsDeterministic pins the candidate order.
func TestHandleRCAAnalyzeRankingIsDeterministic(t *testing.T) {
	srv := &Server{}
	srv.SetRCAEngine(&fakeRCAEngine{report: testRCAReport()})

	resp, err := srv.CallTool(context.Background(), ToolCall{
		Name:      "opsagent_rca_analyze",
		Arguments: windowArgs(map[string]interface{}{"incident_id": "inc-order"}),
	})
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	text := responseText(t, resp)

	high := strings.Index(text, "类型=error_spike")
	low := strings.Index(text, "类型=change")
	if high < 0 || low < 0 {
		t.Fatalf("both candidates must be rendered:\n%s", text)
	}
	if high > low {
		t.Errorf("candidates are not ranked by score:\n%s", text)
	}
}

// TestToolDescriptionsAndNamesUnchanged keeps the MCP contract stable: the
// three tools must keep their exact names and stay free of new tools.
func TestToolDescriptionsAndNamesUnchanged(t *testing.T) {
	srv := &Server{}
	want := map[string]string{
		"opsagent_rca_analyze":   "Analyze incidents using logs, metrics, traces, and topology data",
		"opsagent_log_search":    "Search logs from SLS, Elasticsearch, or other log sources",
		"opsagent_metrics_query": "Query metrics from Prometheus or other metrics sources",
	}

	tools := srv.ListTools()
	seen := map[string]string{}
	for _, tool := range tools {
		seen[tool.Name] = tool.Description
	}
	for name, desc := range want {
		got, ok := seen[name]
		if !ok {
			t.Errorf("tool %q disappeared from the MCP tool list", name)
			continue
		}
		if got != desc {
			t.Errorf("tool %q description = %q, want %q", name, got, desc)
		}
	}
	if len(tools) != 5 {
		t.Errorf("tool count = %d, want 5 (no tool may be added or removed)", len(tools))
	}
}

// ---------------------------------------------------------------------------
// opsagent_changes_list
// ---------------------------------------------------------------------------

// fakeChangePhrases are the hard-coded sample rows and fake counters the old
// changes handler emitted. No call on any code path may ever produce them
// again: the tool either lists real records or honestly reports it has none.
var fakeChangePhrases = []string{
	"Changes found",
	"Changes found: 0",
	"09:15",
	"order-service deployment v2.3.1",
	"postgres-order config update",
	"api-gateway",
}

// fakeChangeSource is a test double for sources.ChangeSource. It returns
// exactly what the test hands it and records WHICH method was used, so a test
// can prove that the unconfigured path never touched the source at all.
type fakeChangeSource struct {
	changes []models.Change
	err     error

	calls           int
	callsByService  int
	callsLastChange int
	gotService      string
	gotStart        time.Time
	gotEnd          time.Time
}

var _ sources.ChangeSource = (*fakeChangeSource)(nil)

func (f *fakeChangeSource) QueryChanges(_ context.Context, start, end time.Time) ([]models.Change, error) {
	f.calls++
	f.gotStart, f.gotEnd = start, end
	if f.err != nil {
		return nil, f.err
	}
	return f.changes, nil
}

func (f *fakeChangeSource) QueryChangesByService(_ context.Context, service string, start, end time.Time) ([]models.Change, error) {
	f.callsByService++
	f.gotService, f.gotStart, f.gotEnd = service, start, end
	if f.err != nil {
		return nil, f.err
	}
	return f.changes, nil
}

func (f *fakeChangeSource) GetLastChange(_ context.Context, service string) (*models.Change, error) {
	f.callsLastChange++
	if f.err != nil {
		return nil, f.err
	}
	return nil, nil
}

// totalCalls spans every interface entry point, so "the source was never
// queried" is asserted across all three methods, not just one.
func (f *fakeChangeSource) totalCalls() int {
	return f.calls + f.callsByService + f.callsLastChange
}

// testChanges is a small, unordered, real-shaped change batch.
func testChanges() []models.Change {
	return []models.Change{
		{
			ID:          "chg-2",
			Timestamp:   testStart.Add(3 * time.Minute),
			Service:     "checkout-api",
			Type:        "config",
			Description: "configmap checkout-api retry policy updated",
		},
		{
			ID:          "chg-1",
			Timestamp:   testStart.Add(time.Minute),
			Service:     "checkout-api",
			Type:        "deployment",
			Description: "deploy checkout-api v9.9.9",
		},
		{
			ID:          "chg-3",
			Timestamp:   testStart.Add(2 * time.Minute),
			Service:     "ledger-db",
			Type:        "scale",
			Description: "ledger-db replicas 2 -> 4",
		},
	}
}

func TestHandleChangesList(t *testing.T) {
	changeErr := errors.New("K8s deployments request failed: connection refused")

	tests := []struct {
		name        string
		src         func() *fakeChangeSource
		real        bool
		args        map[string]interface{}
		want        []string
		notWant     []string
		isError     bool
		wantCalls   int
		wantBySvc   int
		wantService string
	}{
		{
			name:      "real source, no service lists the whole window newest first",
			src:       func() *fakeChangeSource { return &fakeChangeSource{changes: testChanges()} },
			real:      true,
			args:      windowArgs(nil),
			wantCalls: 1,
			want: []string{
				"变更查询结果（数据源：已配置的真实变更源）",
				"时间窗: 2026-01-02T03:04:05Z ~ 2026-01-02T04:04:05Z",
				"范围: 全部服务（QueryChanges，未按服务过滤）",
				"变更源返回条目数: 3",
				"去重后条目数: 3",
				"[1] 2026-01-02T03:07:05Z | checkout-api | config | configmap checkout-api retry policy updated",
				"[3] 2026-01-02T03:05:05Z | checkout-api | deployment | deploy checkout-api v9.9.9",
			},
		},
		{
			name:        "real source, service filter uses QueryChangesByService",
			src:         func() *fakeChangeSource { return &fakeChangeSource{changes: testChanges()} },
			real:        true,
			args:        windowArgs(map[string]interface{}{"service": "ledger-db"}),
			wantCalls:   0,
			wantBySvc:   1,
			wantService: "ledger-db",
			want: []string{
				"范围: 按服务过滤 service=ledger-db（QueryChangesByService）",
				"变更源返回条目数: 3",
				"ledger-db",
			},
		},
		{
			name:      "real source, empty result is honest and is not an error",
			src:       func() *fakeChangeSource { return &fakeChangeSource{} },
			real:      true,
			args:      windowArgs(nil),
			wantCalls: 1,
			want: []string{
				"变更源返回条目数: 0",
				"去重后条目数: 0",
				"返回了 0 条变更记录（真实返回结果，不是省略显示、也不是查询失败）",
			},
			notWant: []string{"其余", "单次展示上限"},
		},
		{
			name: "no source injected is reported as unconfigured and never queried",
			src:  nil,
			args: windowArgs(map[string]interface{}{"service": "checkout-api"}),
			want: []string{
				"变更源未配置",
				"K8S_ENDPOINT/K8S_TOKEN/K8S_NAMESPACE",
				"GET /api/v1/changes",
				"不会查询任何源",
			},
			notWant: []string{"变更源返回条目数", "去重后条目数"},
		},
		{
			name:      "empty fallback source (real=false) is unconfigured and never queried",
			src:       func() *fakeChangeSource { return &fakeChangeSource{changes: testChanges()} },
			real:      false,
			args:      windowArgs(nil),
			wantCalls: 0,
			want: []string{
				"变更源未配置",
				"不会编造任何变更条目或条数",
			},
			notWant: []string{"变更源返回条目数", "deploy checkout-api v9.9.9"}, // the records must not leak
		},
		{
			name:      "source error is surfaced honestly, with no fabricated records",
			src:       func() *fakeChangeSource { return &fakeChangeSource{err: changeErr} },
			real:      true,
			args:      windowArgs(nil),
			wantCalls: 1,
			want:      []string{"变更查询失败", "未返回任何变更记录", changeErr.Error()},
			notWant:   []string{"变更源返回条目数", "deploy checkout-api v9.9.9"},
			isError:   true,
		},
		{
			name:      "malformed start_time is refused without querying",
			src:       func() *fakeChangeSource { return &fakeChangeSource{changes: testChanges()} },
			real:      true,
			args:      map[string]interface{}{"start_time": "2026-01-02 03:04:05", "end_time": testEnd.Format(time.RFC3339)},
			wantCalls: 0,
			want:      []string{"start_time=\"2026-01-02 03:04:05\" 不是合法 RFC3339", "未查询任何变更源"},
			notWant:   []string{"变更源返回条目数"},
			isError:   true,
		},
		{
			name:      "malformed end_time is refused without querying",
			src:       func() *fakeChangeSource { return &fakeChangeSource{changes: testChanges()} },
			real:      true,
			args:      map[string]interface{}{"start_time": testStart.Format(time.RFC3339), "end_time": "not-a-time"},
			wantCalls: 0,
			want:      []string{"end_time=\"not-a-time\" 不是合法 RFC3339", "未查询任何变更源"},
			notWant:   []string{"变更源返回条目数"},
			isError:   true,
		},
		{
			name:      "inverted window is refused without querying",
			src:       func() *fakeChangeSource { return &fakeChangeSource{changes: testChanges()} },
			real:      true,
			args:      map[string]interface{}{"start_time": testEnd.Format(time.RFC3339), "end_time": testStart.Format(time.RFC3339)},
			wantCalls: 0,
			want:      []string{"时间窗非法", "未查询任何变更源"},
			notWant:   []string{"变更源返回条目数"},
			isError:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := &Server{}
			var fake *fakeChangeSource
			if tc.src != nil {
				fake = tc.src()
				srv.SetChangeSource(fake, tc.real)
			}

			resp, err := srv.CallTool(context.Background(), ToolCall{
				Name:      "opsagent_changes_list",
				Arguments: tc.args,
			})
			if err != nil {
				t.Fatalf("CallTool returned error: %v", err)
			}
			if resp.IsError != tc.isError {
				t.Errorf("IsError = %v, want %v", resp.IsError, tc.isError)
			}

			text := responseText(t, resp)
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("output missing %q:\n%s", want, text)
				}
			}
			notWant := append(append([]string{}, tc.notWant...), fakeChangePhrases...)
			for _, nw := range notWant {
				if strings.Contains(text, nw) {
					t.Errorf("output must not contain %q:\n%s", nw, text)
				}
			}
			assertNoFabricatedData(t, text)

			if fake != nil {
				if fake.calls != tc.wantCalls {
					t.Errorf("QueryChanges calls = %d, want %d", fake.calls, tc.wantCalls)
				}
				if fake.callsByService != tc.wantBySvc {
					t.Errorf("QueryChangesByService calls = %d, want %d", fake.callsByService, tc.wantBySvc)
				}
				if tc.wantService != "" && fake.gotService != tc.wantService {
					t.Errorf("service passed to source = %q, want %q", fake.gotService, tc.wantService)
				}
				if tc.wantCalls > 0 || tc.wantBySvc > 0 {
					if !fake.gotStart.Equal(testStart) || !fake.gotEnd.Equal(testEnd) {
						t.Errorf("source window = %s ~ %s, want %s ~ %s",
							fake.gotStart.Format(time.RFC3339), fake.gotEnd.Format(time.RFC3339),
							testStart.Format(time.RFC3339), testEnd.Format(time.RFC3339))
					}
				}
				if fake.callsLastChange != 0 {
					t.Errorf("GetLastChange was called %d times; the tool must not use it", fake.callsLastChange)
				}
			}
		})
	}
}

// TestHandleChangesListUnconfiguredNeverQueries proves the hard contract: with
// no real source the source is not touched AT ALL, on any interface method.
func TestHandleChangesListUnconfiguredNeverQueries(t *testing.T) {
	cases := []struct {
		name string
		wire func(srv *Server, fake *fakeChangeSource)
	}{
		{
			name: "nil source",
			wire: func(_ *Server, _ *fakeChangeSource) {},
		},
		{
			name: "injected source flagged as not real",
			wire: func(srv *Server, fake *fakeChangeSource) { srv.SetChangeSource(fake, false) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeChangeSource{changes: testChanges()}
			srv := &Server{}
			tc.wire(srv, fake)

			for _, args := range []map[string]interface{}{
				windowArgs(nil),
				windowArgs(map[string]interface{}{"service": "checkout-api"}),
				{}, // even a bad/absent window must not reach the source
			} {
				resp, err := srv.CallTool(context.Background(), ToolCall{
					Name:      "opsagent_changes_list",
					Arguments: args,
				})
				if err != nil {
					t.Fatalf("CallTool returned error: %v", err)
				}
				if resp.IsError {
					t.Errorf("unconfigured change source must not be an error:\n%s", responseText(t, resp))
				}
				text := responseText(t, resp)
				if !strings.Contains(text, "变更源未配置") {
					t.Errorf("expected the honest unconfigured text:\n%s", text)
				}
				for _, nw := range fakeChangePhrases {
					if strings.Contains(text, nw) {
						t.Errorf("output must not contain %q:\n%s", nw, text)
					}
				}
				assertNoFabricatedData(t, text)
			}

			if got := fake.totalCalls(); got != 0 {
				t.Errorf("change source was queried %d times while unconfigured, want 0", got)
			}
		})
	}
}

// TestHandleChangesListDefaultsWindowWhenAbsent pins the documented default:
// the shared lookback window, echoed in the output, really handed to the source.
func TestHandleChangesListDefaultsWindowWhenAbsent(t *testing.T) {
	fake := &fakeChangeSource{changes: testChanges()}
	srv := &Server{}
	srv.SetChangeSource(fake, true)

	before := time.Now()
	resp, err := srv.CallTool(context.Background(), ToolCall{
		Name:      "opsagent_changes_list",
		Arguments: map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	after := time.Now()

	text := responseText(t, resp)
	if !strings.Contains(text, "start_time 未提供") || !strings.Contains(text, "end_time 未提供") {
		t.Errorf("the applied defaults must be reported:\n%s", text)
	}
	if fake.calls != 1 {
		t.Fatalf("QueryChanges calls = %d, want 1", fake.calls)
	}
	if slack := 5 * time.Second; fake.gotStart.Before(before.Add(-defaultLookback).Add(-slack)) ||
		fake.gotStart.After(after.Add(-defaultLookback).Add(slack)) {
		t.Errorf("defaulted start = %s, want ~%s", fake.gotStart.Format(time.RFC3339), before.Add(-defaultLookback).Format(time.RFC3339))
	}
	if fake.gotEnd.Before(before.Add(-5*time.Second)) || fake.gotEnd.After(after.Add(5*time.Second)) {
		t.Errorf("defaulted end = %s, want the call time", fake.gotEnd.Format(time.RFC3339))
	}
}

// TestHandleChangesListOrderingDedupAndBound pins the deterministic newest-first
// order, the exact-duplicate removal and the display cap — and that every count
// in the output comes from the records the source really returned.
func TestHandleChangesListOrderingDedupAndBound(t *testing.T) {
	base := make([]models.Change, 0, 25)
	for i := 0; i < 25; i++ {
		base = append(base, models.Change{
			ID:          fmt.Sprintf("chg-%02d", i),
			Timestamp:   testStart.Add(time.Duration(i) * time.Minute),
			Service:     "checkout-api",
			Type:        "deployment",
			Description: fmt.Sprintf("change-%02d", i),
		})
	}
	// Exact duplicates are handed in at the FRONT of the batch too, so a
	// deduplicator that only looks at adjacent pairs after sorting still works.
	returned := append([]models.Change{}, base[7], base[0], base[7])
	returned = append(returned, base...)

	srv := &Server{}
	srv.SetChangeSource(&fakeChangeSource{changes: returned}, true)

	call := func() string {
		t.Helper()
		resp, err := srv.CallTool(context.Background(), ToolCall{
			Name:      "opsagent_changes_list",
			Arguments: windowArgs(nil),
		})
		if err != nil {
			t.Fatalf("CallTool returned error: %v", err)
		}
		return responseText(t, resp)
	}

	text := call()
	assertNoFabricatedData(t, text)

	if !strings.Contains(text, fmt.Sprintf("变更源返回条目数: %d", len(returned))) {
		t.Fatalf("expected the real returned count %d:\n%s", len(returned), text)
	}
	if !strings.Contains(text, "去重后条目数: 25") {
		t.Fatalf("expected the real deduplicated count:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("... 其余 %d 条已省略", 25-maxChangesShown)) {
		t.Fatalf("expected a truncation notice:\n%s", text)
	}
	if got := strings.Count(text, "\n  ["); got != maxChangesShown {
		t.Fatalf("rendered %d entries, want %d:\n%s", got, maxChangesShown, text)
	}

	// change-24 is the newest and change-05 the oldest still shown; change-04
	// fell past the cap.
	newest, lastShown, omitted := strings.Index(text, "change-24"),
		strings.Index(text, "change-05"), strings.Index(text, "change-04")
	if newest < 0 || lastShown < 0 {
		t.Fatalf("expected the 20 newest records to be rendered:\n%s", text)
	}
	if newest > lastShown {
		t.Errorf("records are not in newest-first order:\n%s", text)
	}
	if omitted >= 0 {
		t.Errorf("record past the display cap must not be rendered:\n%s", text)
	}

	// Deterministic: the same real records must render byte-identically.
	if again := call(); again != text {
		t.Errorf("output is not deterministic\n--- first ---\n%s\n--- second ---\n%s", text, again)
	}
}

// TestFormatChangesResultGolden pins the exact rendered text of a real batch,
// so the documented sample output cannot drift silently.
func TestFormatChangesResultGolden(t *testing.T) {
	srv := &Server{}
	srv.SetChangeSource(&fakeChangeSource{changes: testChanges()}, true)

	resp, err := srv.CallTool(context.Background(), ToolCall{
		Name:      "opsagent_changes_list",
		Arguments: windowArgs(nil),
	})
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}

	want := "变更查询结果（数据源：已配置的真实变更源）\n" +
		"时间窗: 2026-01-02T03:04:05Z ~ 2026-01-02T04:04:05Z\n" +
		"范围: 全部服务（QueryChanges，未按服务过滤）\n" +
		"变更源返回条目数: 3\n" +
		"去重后条目数: 3\n" +
		"展示前 3 条（按时间倒序，格式：时间 | service | 类型 | 描述）:\n" +
		"  [1] 2026-01-02T03:07:05Z | checkout-api | config | configmap checkout-api retry policy updated\n" +
		"  [2] 2026-01-02T03:06:05Z | ledger-db | scale | ledger-db replicas 2 -> 4\n" +
		"  [3] 2026-01-02T03:05:05Z | checkout-api | deployment | deploy checkout-api v9.9.9"

	if got := responseText(t, resp); got != want {
		t.Errorf("changes list output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
