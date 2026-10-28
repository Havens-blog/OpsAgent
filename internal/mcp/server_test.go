package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

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
// name from the old fake tree, or a fake-success phrase.
func assertNoFabricatedData(t *testing.T, text string) {
	t.Helper()
	for _, name := range fabricatedNames {
		if strings.Contains(text, name) {
			t.Errorf("output must not contain fabricated node %q:\n%s", name, text)
		}
	}
	if strings.Contains(strings.ToLower(text), "successfully") {
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
