package interfaces

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/agent"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/rca/models"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
	"github.com/Havens-blog/OpsAgent/internal/tool"
)

// wafQuerier wraps the shared e-cam WAF source for the chat tool and the
// WatchAlert webhook. Query + aggregation live in the sources layer
// (ECamWAFSource) so the RCA engine and these paths read the same facts.
type wafQuerier struct {
	src *sources.ECamWAFSource
}

// newWAFQuerier builds a WAF querier from the same e-cam config that feeds the
// log dimension. nil when e-cam is not configured (tool stays unavailable).
func newWAFQuerier(cfg *agent.DataSourceConfig) *wafQuerier {
	if cfg == nil || cfg.ECamEndpoint == "" {
		return nil
	}
	timeout := 30 * time.Second
	if cfg.ECamTimeout != "" {
		if d, err := time.ParseDuration(cfg.ECamTimeout); err == nil && d > 0 {
			timeout = d
		}
	}
	src := sources.NewECamWAFSource(cfg.ECamEndpoint, cfg.ECamToken, timeout)
	if cfg.ECamTokenURL != "" {
		src.WithTokenProvider(sources.NewServiceTokenProvider(
			cfg.ECamTokenURL, cfg.ECamClientID, cfg.ECamClientSecret, cfg.ECamTenantID,
		))
	}
	return &wafQuerier{src: src}
}

// query returns the factual attack summary for one WAF domain.
func (q *wafQuerier) query(ctx context.Context, domain string, start, end time.Time) (*models.WAFAttackFacts, error) {
	if q == nil || q.src == nil {
		return nil, fmt.Errorf("WAF 日志查询未配置(e-cam 未接入)")
	}
	return q.src.QueryAttackFacts(ctx, domain, start, end)
}

// createWAFLogQueryTool registers the factual WAF query tool. q may be nil — the
// tool then reports unavailable rather than fabricating.
func createWAFLogQueryTool(q *wafQuerier) *tool.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"domain": {"type": "string", "description": "WAF 防护的域名/主机, 例如 www.zxhgroup.com"},
			"start_time": {"type": "string", "description": "开始时间 RFC3339"},
			"end_time": {"type": "string", "description": "结束时间 RFC3339"}
		},
		"required": ["domain", "start_time", "end_time"]
	}`
	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "query_waf_logs",
			Description: "查询指定域名在时间窗内的 WAF 拦截日志，返回攻击类型/来源IP/目标URI/动作的事实分布",
			Source:      tool.DataSourceTypeSLS,
			InputSchema: []byte(schema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "logs",
		},
		IsAvailable: func(map[string]interface{}) bool { return q != nil },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			domain, _ := params["domain"].(string)
			start, err := parseRFC3339(params["start_time"])
			if err != nil {
				return nil, fmt.Errorf("start_time 解析失败: %w", err)
			}
			end, err := parseRFC3339(params["end_time"])
			if err != nil {
				return nil, fmt.Errorf("end_time 解析失败: %w", err)
			}
			return q.query(ctx, domain, start, end)
		},
	}
}

func parseRFC3339(v interface{}) (time.Time, error) {
	s, _ := v.(string)
	return time.Parse(time.RFC3339, s)
}
