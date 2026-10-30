package chat

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Havens-blog/OpsAgent/internal/rca/models"
)

// ToolEvidence is a structured, persisted record of one executed tool call.
type ToolEvidence struct {
	EvidenceID   string                 `json:"evidence_id"`
	ToolName     string                 `json:"tool_name"`
	Status       string                 `json:"status"` // "success" | "failed" | "unavailable"
	Source       string                 `json:"source"`
	Params       map[string]interface{} `json:"params,omitempty"`
	ResultDigest map[string]interface{} `json:"result_digest,omitempty"`
	Error        string                 `json:"error,omitempty"`
	At           time.Time              `json:"at"`
}

// intentRule maps a set of user-intent keywords to the tools whose successful
// result is required before an answer can be considered grounded.
type intentRule struct {
	keywords []string
	tools    []string
}

var mustQueryRules = []intentRule{
	{keywords: []string{"WAF", "攻击", "拦截", "CC", "Web攻击"}, tools: []string{"query_waf_logs"}},
	{keywords: []string{"日志", "报错", "error", "异常"}, tools: []string{"query_logs", "query_logs_advanced", "analyze_logs", "correlate_logs"}},
	{keywords: []string{"指标", "metric", "监控", "内存", "CPU", "QPS"}, tools: []string{"query_metrics", "analyze_incident"}},
	{keywords: []string{"链路", "trace", "调用链"}, tools: []string{"query_traces"}},
	{keywords: []string{"拓扑", "依赖", "上下游"}, tools: []string{"get_topology", "lookup_domain_backend"}},
	{keywords: []string{"CDN", "云资源", "ECS", "RDS", "实例清单", "资源概况"}, tools: []string{"list_cmdb_instances", "resource_summary"}},
	{keywords: []string{"域名解析", "回源", "指向"}, tools: []string{"lookup_cmdb_domain", "lookup_domain_backend"}},
}

// mustQueryTools returns the union of tool sets whose successful result is
// required by the given user message. An empty slice means no grounding
// requirement applies.
func mustQueryTools(text string) []string {
	required := map[string]struct{}{}
	for _, rule := range mustQueryRules {
		if containsAny(text, rule.keywords...) {
			for _, name := range rule.tools {
				required[name] = struct{}{}
			}
		}
	}
	if len(required) == 0 {
		return nil
	}
	out := make([]string, 0, len(required))
	for name := range required {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func containsAny(text string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// computeGrounding decides whether the answer is grounded, unsupported, or has
// no grounding requirement (n/a).
func computeGrounding(required []string, evidence []ToolEvidence) string {
	if len(required) == 0 {
		return "n/a"
	}
	for _, ev := range evidence {
		if ev.Status != "success" {
			continue
		}
		for _, name := range required {
			if ev.ToolName == name {
				return "grounded"
			}
		}
	}
	return "unsupported"
}

func newEvidenceID() string {
	return uuid.New().String()
}

// toolDigest projects a tool result down to the key fields that prove an
// answer is grounded, keeping evidence small and deterministic.
func toolDigest(name string, result interface{}) map[string]interface{} {
	switch name {
	case "query_waf_logs":
		return wafDigest(result)
	case "list_cmdb_instances":
		return cmdbInstancesDigest(result)
	case "resource_summary":
		return projectMap(result, "total", "by_asset_type")
	case "lookup_cmdb_domain":
		return projectMap(result, "domain", "found", "origin_host", "origin_type")
	case "lookup_domain_backend":
		return projectMap(result, "domain", "count", "backends")
	default:
		return fallbackDigest(result)
	}
}

func wafDigest(result interface{}) map[string]interface{} {
	if facts, ok := result.(*models.WAFAttackFacts); ok {
		out := map[string]interface{}{
			"domain":        facts.Domain,
			"matched":       facts.Matched,
			"sample_capped": facts.SampleCapped,
		}
		if facts.AttackRules != nil {
			out["top_rules"] = facts.AttackRules
		}
		return out
	}

	m := toMap(result)
	out := projectMap(result, "domain", "matched", "sample_capped")
	if v, ok := m["top_rules"]; ok {
		out["top_rules"] = v
	} else if v, ok := m["attack_rules"]; ok {
		out["top_rules"] = v
	}
	return out
}

func cmdbInstancesDigest(result interface{}) map[string]interface{} {
	m := toMap(result)
	out := projectMap(result, "total", "count")

	modelUID := stringField(m, "model_uid")
	if modelUID == "" {
		modelUID = firstInstanceModelUID(m["instances"])
	}
	if modelUID != "" {
		out["model_uid"] = modelUID
	}
	if names := collectAssetNames(m["instances"], 20); len(names) > 0 {
		out["asset_names"] = names
	}
	return out
}

func projectMap(result interface{}, keys ...string) map[string]interface{} {
	m := toMap(result)
	out := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

func fallbackDigest(result interface{}) map[string]interface{} {
	var raw string
	if result == nil {
		raw = ""
	} else if b, err := json.Marshal(result); err == nil {
		raw = string(b)
	} else {
		raw = fmt.Sprintf("%v", result)
	}
	return map[string]interface{}{"value": truncateBytes(raw, 2048)}
}

func toMap(result interface{}) map[string]interface{} {
	if result == nil {
		return nil
	}
	if m, ok := result.(map[string]interface{}); ok {
		return m
	}
	b, err := json.Marshal(result)
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
}

func stringField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func collectAssetNames(instances interface{}, limit int) []string {
	items := normalizeInstances(instances)
	names := make([]string, 0, limit)
	for _, it := range items {
		if len(names) >= limit {
			break
		}
		if name := stringField(it, "asset_name"); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func firstInstanceModelUID(instances interface{}) string {
	for _, it := range normalizeInstances(instances) {
		if uid := stringField(it, "model_uid"); uid != "" {
			return uid
		}
	}
	return ""
}

func normalizeInstances(v interface{}) []map[string]interface{} {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(b, &items); err != nil {
		return nil
	}
	return items
}

func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
