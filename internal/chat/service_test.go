package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Havens-blog/OpsAgent/internal/llm"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

type fakeStreamClient struct {
	chunks []llm.ChatChunk
	err    error
}

func (f *fakeStreamClient) Chat(ctx context.Context, messages []llm.Message) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (f *fakeStreamClient) ChatWithTools(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (f *fakeStreamClient) ChatStream(ctx context.Context, messages []llm.Message, handler func(llm.ChatChunk)) (*llm.ChatResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, c := range f.chunks {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		handler(c)
	}
	return &llm.ChatResponse{}, nil
}

type blockingClient struct{}

func (b *blockingClient) Chat(ctx context.Context, messages []llm.Message) (*llm.ChatResponse, error) {
	return nil, nil
}

func (b *blockingClient) ChatWithTools(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	return nil, nil
}

func (b *blockingClient) ChatStream(ctx context.Context, messages []llm.Message, handler func(llm.ChatChunk)) (*llm.ChatResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestServiceChatStreamDeltas(t *testing.T) {
	svc := NewServiceWithClient(&fakeStreamClient{
		chunks: []llm.ChatChunk{
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "你"}}}},
			{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "好"}}}},
		},
	}, "test")

	var got string
	err := svc.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(d string) error {
		got += d
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if got != "你好" {
		t.Fatalf("got %q, want %q", got, "你好")
	}
}

func TestServiceChatStreamError(t *testing.T) {
	wantErr := errors.New("boom")
	svc := NewServiceWithClient(&fakeStreamClient{err: wantErr}, "test")

	err := svc.ChatStream(context.Background(), nil, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestServiceChatStreamCancel(t *testing.T) {
	svc := NewServiceWithClient(&blockingClient{}, "test")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.ChatStream(ctx, nil, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected ctx error, got nil")
	}
}

type fakeToolProvider struct {
	specs   []llm.Tool
	source  map[string]string
	results map[string]fakeToolResult
}

type fakeToolResult struct {
	result interface{}
	err    error
}

func (f *fakeToolProvider) ToolSpecsForLLM() ([]llm.Tool, error) {
	return f.specs, nil
}

func (f *fakeToolProvider) ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	r, ok := f.results[name]
	if !ok {
		return nil, fmt.Errorf("tool %s not found", name)
	}
	return r.result, r.err
}

func (f *fakeToolProvider) ToolSource(name string) string {
	if f.source == nil {
		return ""
	}
	return f.source[name]
}

type toolLoopClient struct {
	toolCalls []llm.ToolCall
	chunks    []llm.ChatChunk
}

func (c *toolLoopClient) Chat(ctx context.Context, messages []llm.Message) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (c *toolLoopClient) ChatWithTools(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	for _, m := range messages {
		if m.Role == "tool" {
			return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.ResponseMessage{Role: "assistant"}}}}, nil
		}
	}
	if c.toolCalls == nil {
		return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.ResponseMessage{Role: "assistant"}}}}, nil
	}
	return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.ResponseMessage{Role: "assistant", ToolCalls: c.toolCalls}}}}, nil
}

func (c *toolLoopClient) ChatStream(ctx context.Context, messages []llm.Message, handler func(llm.ChatChunk)) (*llm.ChatResponse, error) {
	for _, ch := range c.chunks {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		handler(ch)
	}
	return &llm.ChatResponse{}, nil
}

func newStreamHandler() (StreamHandler, *strings.Builder) {
	var got strings.Builder
	h := StreamHandler{OnDelta: func(d string) error {
		got.WriteString(d)
		return nil
	}}
	return h, &got
}

func TestMustQueryTools(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"WAF 拦截情况", []string{"query_waf_logs"}},
		{"查询服务日志", []string{"analyze_logs", "correlate_logs", "query_logs", "query_logs_advanced"}},
		{"CPU 内存指标监控", []string{"analyze_incident", "query_metrics"}},
		{"链路 trace 调用链", []string{"query_traces"}},
		{"拓扑依赖上下游", []string{"get_topology", "lookup_domain_backend"}},
		{"CDN 云资源实例清单", []string{"list_cmdb_instances", "resource_summary"}},
		{"域名解析回源指向", []string{"lookup_cmdb_domain", "lookup_domain_backend"}},
		{"WAF 和 CDN 资源概况", []string{"list_cmdb_instances", "query_waf_logs", "resource_summary"}},
		{"今天天气怎么样", nil},
	}

	for _, tc := range cases {
		got := mustQueryTools(tc.text)
		if !equalStrings(got, tc.want) {
			t.Fatalf("mustQueryTools(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestComputeGrounding(t *testing.T) {
	success := ToolEvidence{ToolName: "query_waf_logs", Status: "success"}
	failed := ToolEvidence{ToolName: "query_waf_logs", Status: "failed"}
	other := ToolEvidence{ToolName: "query_metrics", Status: "success"}

	cases := []struct {
		name     string
		required []string
		evidence []ToolEvidence
		want     string
	}{
		{"no requirement", nil, nil, "n/a"},
		{"grounded", []string{"query_waf_logs"}, []ToolEvidence{success}, "grounded"},
		{"unsupported no evidence", []string{"query_waf_logs"}, nil, "unsupported"},
		{"unsupported failed", []string{"query_waf_logs"}, []ToolEvidence{failed}, "unsupported"},
		{"unsupported wrong tool", []string{"query_waf_logs"}, []ToolEvidence{other}, "unsupported"},
	}

	for _, tc := range cases {
		if got := computeGrounding(tc.required, tc.evidence); got != tc.want {
			t.Fatalf("%s: computeGrounding() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

type instanceLike struct {
	ModelUID  string `json:"model_uid"`
	AssetName string `json:"asset_name"`
}

type statsLike struct {
	Total       int64              `json:"total"`
	ByAssetType []models.FactCount `json:"by_asset_type"`
}

func TestToolDigest(t *testing.T) {
	waf := toolDigest("query_waf_logs", &models.WAFAttackFacts{
		Domain:       "a.com",
		Matched:      120,
		SampleCapped: true,
		AttackRules:  []models.FactCount{{Key: "xss", Count: 3}},
	})
	if waf["domain"] != "a.com" || waf["matched"] != int64(120) || waf["sample_capped"] != true || waf["top_rules"] == nil {
		t.Fatalf("waf digest = %+v", waf)
	}

	instances := toolDigest("list_cmdb_instances", map[string]interface{}{
		"instances": []instanceLike{{ModelUID: "cdn", AssetName: "a.com"}, {ModelUID: "cdn", AssetName: "b.com"}},
		"total":     int64(5),
		"count":     2,
	})
	if instances["total"] != int64(5) || instances["count"] != 2 || instances["model_uid"] != "cdn" {
		t.Fatalf("instances digest = %+v", instances)
	}
	names, _ := instances["asset_names"].([]string)
	if len(names) != 2 || names[0] != "a.com" || names[1] != "b.com" {
		t.Fatalf("instances asset_names = %+v", instances["asset_names"])
	}

	summary := toolDigest("resource_summary", &statsLike{Total: 9, ByAssetType: []models.FactCount{{Key: "ecs", Count: 4}}})
	if summary["total"] != float64(9) || summary["by_asset_type"] == nil {
		t.Fatalf("summary digest = %+v", summary)
	}

	domain := toolDigest("lookup_cmdb_domain", map[string]interface{}{"domain": "a.com", "found": true, "origin_host": "h", "origin_type": "cdn"})
	if domain["domain"] != "a.com" || domain["found"] != true || domain["origin_host"] != "h" {
		t.Fatalf("domain digest = %+v", domain)
	}

	backend := toolDigest("lookup_domain_backend", map[string]interface{}{"domain": "a.com", "count": 1, "backends": []string{"svc"}})
	if backend["domain"] != "a.com" || backend["count"] != 1 || backend["backends"] == nil {
		t.Fatalf("backend digest = %+v", backend)
	}

	long := strings.Repeat("x", 3000)
	fallback := toolDigest("unknown_tool", long)
	if v, ok := fallback["value"].(string); !ok || len(v) != 2048 {
		t.Fatalf("fallback digest value length = %d, want 2048", len(fallback["value"].(string)))
	}
}

func TestStreamWithToolsGroundingGrounded(t *testing.T) {
	args := `{"domain":"a.com"}`
	client := &toolLoopClient{
		toolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "query_waf_logs", Arguments: args}}},
		chunks:    []llm.ChatChunk{{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "已拦截"}}}}},
	}
	svc := NewServiceWithClient(client, "test")
	svc.SetToolProvider(&fakeToolProvider{
		specs:  []llm.Tool{{Type: "function", Function: llm.FunctionDefinition{Name: "query_waf_logs"}}},
		source: map[string]string{"query_waf_logs": "aliyun_sls"},
		results: map[string]fakeToolResult{
			"query_waf_logs": {result: &models.WAFAttackFacts{Domain: "a.com", Matched: 3}},
		},
	})

	h, got := newStreamHandler()
	text, evidence, grounding, err := svc.StreamWithTools(context.Background(), []Message{{Role: "user", Content: "查询 WAF 日志"}}, h)
	if err != nil {
		t.Fatalf("StreamWithTools: %v", err)
	}
	if grounding != "grounded" {
		t.Fatalf("grounding = %q, want grounded", grounding)
	}
	if text != "已拦截" {
		t.Fatalf("text = %q, want no unsupported suffix", text)
	}
	if len(evidence) != 1 || evidence[0].Status != "success" || evidence[0].ToolName != "query_waf_logs" {
		t.Fatalf("evidence = %+v", evidence)
	}
	if got.String() != "已拦截" {
		t.Fatalf("delta = %q", got.String())
	}
}

func TestStreamWithToolsGroundingUnsupported(t *testing.T) {
	args := `{"domain":"a.com"}`
	client := &toolLoopClient{
		toolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "query_waf_logs", Arguments: args}}},
		chunks:    []llm.ChatChunk{{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "暂无数据"}}}}},
	}
	svc := NewServiceWithClient(client, "test")
	svc.SetToolProvider(&fakeToolProvider{
		specs:  []llm.Tool{{Type: "function", Function: llm.FunctionDefinition{Name: "query_waf_logs"}}},
		source: map[string]string{"query_waf_logs": "aliyun_sls"},
		results: map[string]fakeToolResult{
			"query_waf_logs": {err: fmt.Errorf("tool query_waf_logs is not available: data source not configured")},
		},
	})

	h, _ := newStreamHandler()
	text, evidence, grounding, err := svc.StreamWithTools(context.Background(), []Message{{Role: "user", Content: "查询 WAF 日志"}}, h)
	if err != nil {
		t.Fatalf("StreamWithTools: %v", err)
	}
	if grounding != "unsupported" {
		t.Fatalf("grounding = %q, want unsupported", grounding)
	}
	if !strings.Contains(text, "本次回答未取得相关数据源的工具结果") {
		t.Fatalf("text missing unsupported suffix: %q", text)
	}
	if len(evidence) != 1 || evidence[0].Status != "unavailable" {
		t.Fatalf("evidence = %+v", evidence)
	}
}

func TestStreamWithToolsGroundingNA(t *testing.T) {
	svc := NewServiceWithClient(&fakeStreamClient{
		chunks: []llm.ChatChunk{{Choices: []llm.Choice{{Delta: &llm.Delta{Content: "你好"}}}}},
	}, "test")

	h, _ := newStreamHandler()
	text, evidence, grounding, err := svc.StreamWithTools(context.Background(), []Message{{Role: "user", Content: "你好"}}, h)
	if err != nil {
		t.Fatalf("StreamWithTools: %v", err)
	}
	if grounding != "n/a" {
		t.Fatalf("grounding = %q, want n/a", grounding)
	}
	if text != "你好" {
		t.Fatalf("text = %q, want 你好", text)
	}
	if evidence != nil {
		t.Fatalf("evidence = %+v, want nil", evidence)
	}
}
