package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/observability"
	"github.com/Havens-blog/OpsAgent/internal/rca/sources"
	"github.com/Havens-blog/OpsAgent/internal/tool"
)

// TopologyAgent specializes in resource topology: it queries the e-cmdb for
// model inventory, cloud-resource instances, and domain->origin mappings. It
// is the honest answer to "what serves cut.jlc-gw.com" — real CMDB data when
// present, an explicit "not found" otherwise (never a guessed mapping).
type TopologyAgent struct {
	*BaseAgent
	cmdb     *sources.CMDBDataSource
	slb      *sources.ECamLogSource
	tenantID int64
}

// NewTopologyAgent creates a resource-topology agent over the CMDB source and
// the SLB access-log source (domain -> backend service). tenantID is the
// tenant to scope queries to (the S2S path carries no session tenant).
func NewTopologyAgent(settings *config.Settings, cmdb *sources.CMDBDataSource, slb *sources.ECamLogSource, tenantID int64) (*TopologyAgent, error) {
	cfg := &AgentConfig{
		Name:        "topology-agent",
		Type:        AgentTypeTopology,
		Description: "Query the CMDB resource topology: models, cloud-resource inventory, and domain origins",
		Model:       "claude-sonnet-4-20250514",
		MaxTurns:    10,
		Timeout:     5 * time.Minute,
		SystemPrompt: `You are a resource-topology agent specialized in CMDB queries.

Your capabilities include:
- Listing CMDB models (asset types)
- Listing cloud-resource instances of a model
- Resolving a domain to its recorded origin (CDN/WAF origin_host)

When answering:
1. Query the real CMDB data; never invent resources or mappings
2. When a domain has no recorded mapping, say so plainly — do not guess
3. Report the origin_host and origin_type when present`,
		Capabilities: []string{"cmdb", "resource_topology", "domain_origin", "inventory"},
	}

	base, err := NewBaseAgent(cfg, settings)
	if err != nil {
		return nil, err
	}

	return &TopologyAgent{
		BaseAgent: base,
		cmdb:      cmdb,
		slb:       slb,
		tenantID:  tenantID,
	}, nil
}

// BoundTools returns the CMDB + SLB tools bound to the real data sources.
func (t *TopologyAgent) BoundTools() []*tool.Tool {
	return []*tool.Tool{
		t.resourceSummaryTool(),
		t.listModelsTool(),
		t.listInstancesTool(),
		t.lookupDomainTool(),
		t.lookupDomainBackendTool(),
	}
}

// resourceSummaryTool answers "现在的云资源概况" in one call: total instances
// plus per-asset-type and per-provider distributions from the CMDB stats.
func (t *TopologyAgent) resourceSummaryTool() *tool.Tool {
	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "resource_summary",
			Description: "Get the cloud resource overview: total instances plus per-asset-type and per-provider counts",
			Source:      tool.DataSourceTypeGeneric,
			InputSchema: []byte(`{"type":"object","properties":{}}`),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "cmdb",
		},
		IsAvailable: func(map[string]interface{}) bool { return t.available() },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			if !t.available() {
				return nil, fmt.Errorf("CMDB source not configured")
			}
			return t.cmdb.StatsAllNormalized(ctx, t.tenantID)
		},
	}
}

func (t *TopologyAgent) available() bool { return t.cmdb != nil }

func (t *TopologyAgent) listModelsTool() *tool.Tool {
	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "list_cmdb_models",
			Description: "List CMDB asset models (host, cdn, rds, k8s_deployment, waf, ...)",
			Source:      tool.DataSourceTypeGeneric,
			InputSchema: []byte(`{"type":"object","properties":{}}`),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "cmdb",
		},
		IsAvailable: func(map[string]interface{}) bool { return t.available() },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			if !t.available() {
				return nil, fmt.Errorf("CMDB source not configured")
			}
			return t.cmdb.ListModels(ctx)
		},
	}
}

func (t *TopologyAgent) listInstancesTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"model_uid": {"type": "string", "description": "Model UID, e.g. cdn / host / rds / k8s_deployment"},
			"asset_name": {"type": "string", "description": "Optional asset name filter (exact)"},
			"provider": {"type": "string", "description": "Optional cloud provider filter: aliyun / aws / azure / tencent / huawei / volcano (火山云用 volcano)"},
			"limit": {"type": "integer", "description": "Max instances to return", "default": 50}
		},
		"required": ["model_uid"]
	}`
	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "list_cmdb_instances",
			Description: "List CMDB resource instances of a model, optionally filtered by cloud provider (e.g. provider=volcano + model_uid=cdn for 火山云 CDN)",
			Source:      tool.DataSourceTypeGeneric,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "cmdb",
		},
		IsAvailable: func(map[string]interface{}) bool { return t.available() },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			if !t.available() {
				return nil, fmt.Errorf("CMDB source not configured")
			}
			modelUID := getString(params, "model_uid")
			assetName := getString(params, "asset_name")
			provider := getString(params, "provider")
			limit := 50
			if v, ok := params["limit"].(float64); ok && int(v) > 0 {
				limit = int(v)
			}
			instances, total, err := t.cmdb.ListInstances(ctx, t.tenantID, modelUID, assetName, provider, limit)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"instances": instances,
				"total":     total,
				"count":     len(instances),
			}, nil
		},
	}
}

func (t *TopologyAgent) lookupDomainTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"domain": {"type": "string", "description": "Host to resolve, e.g. cut.jlc-gw.com"}
		},
		"required": ["domain"]
	}`
	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "lookup_cmdb_domain",
			Description: "Resolve a domain to its recorded origin (origin_host/origin_type) in the CMDB CDN inventory",
			Source:      tool.DataSourceTypeGeneric,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "cmdb",
		},
		IsAvailable: func(map[string]interface{}) bool { return t.available() },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			if !t.available() {
				return nil, fmt.Errorf("CMDB source not configured")
			}
			domain := getString(params, "domain")
			facts, err := t.cmdb.LookupDomain(ctx, t.tenantID, domain)
			if err != nil {
				return nil, err
			}
			if facts == nil {
				return map[string]interface{}{"domain": domain, "found": false}, nil
			}
			return map[string]interface{}{
				"domain":      facts.Domain,
				"found":       true,
				"origin_host": facts.OriginHost,
				"origin_type": facts.OriginType,
			}, nil
		},
	}
}

func (t *TopologyAgent) lookupDomainBackendTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"domain": {"type": "string", "description": "Host to resolve, e.g. www.jlc-smt.com"},
			"start_time": {"type": "string", "description": "RFC3339, default 1h ago"},
			"end_time": {"type": "string", "description": "RFC3339, default now"}
		},
		"required": ["domain"]
	}`
	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:        "lookup_domain_backend",
			Description: "Resolve a domain to its backend k8s services via the SLB access log (host -> server_group_name/target_ip)",
			Source:      tool.DataSourceTypeGeneric,
			InputSchema: []byte(inputSchema),
			Tier:        guardrails.TierSafe,
			SideEffect:  guardrails.SideEffectReadOnly,
			Category:    "cmdb",
		},
		IsAvailable: func(map[string]interface{}) bool { return t.slb != nil },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			if t.slb == nil {
				return nil, fmt.Errorf("SLB log source not configured")
			}
			domain := getString(params, "domain")
			start, end := timeRange(params)
			backends, err := t.slb.QueryDomainBackends(ctx, domain, start, end)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"domain": domain, "backends": backends, "count": len(backends)}, nil
		},
	}
}

// Execute overrides BaseAgent.Execute to route typed topology tasks.
func (t *TopologyAgent) Execute(ctx context.Context, task *Task) (*ExecutionResult, error) {
	start := time.Now()

	if _, hasDeadline := ctx.Deadline(); !hasDeadline && t.BaseAgent.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.BaseAgent.config.Timeout)
		defer cancel()
	}

	t.BaseAgent.conversation.AddMessage(Message{
		Role:      "user",
		Content:   task.Description,
		Timestamp: time.Now(),
	})

	t.logger.Info("TopologyAgent execution started", observability.String("task_id", task.ID))

	if taskType, ok := task.Params["type"].(string); ok && (taskType == "topology" || taskType == "cmdb") {
		return t.executeTopologyTask(ctx, task)
	}

	result, err := t.executeTask(ctx, task)
	result.Duration = time.Since(start)
	return result, err
}

// executeTopologyTask runs a typed resource-topology query.
func (t *TopologyAgent) executeTopologyTask(ctx context.Context, task *Task) (*ExecutionResult, error) {
	if !t.available() {
		return &ExecutionResult{Success: false, Error: fmt.Errorf("CMDB source not configured")}, nil
	}

	if domain := getString(task.Params, "domain"); domain != "" {
		data := map[string]interface{}{"domain": domain}

		// CMDB origin (CDN -> WAF CNAME).
		if t.cmdb != nil {
			if facts, err := t.cmdb.LookupDomain(ctx, t.tenantID, domain); err == nil && facts != nil {
				data["origin_host"] = facts.OriginHost
				data["origin_type"] = facts.OriginType
			}
		}

		// SLB backend (domain -> k8s service via server_group_name).
		var backends []sources.SLBBackend
		if t.slb != nil {
			start, end := timeRange(task.Params)
			backends, _ = t.slb.QueryDomainBackends(ctx, domain, start, end)
		}
		data["backends"] = backends

		var response string
		if len(backends) == 0 {
			response = fmt.Sprintf("域名 %s: 未在 SLB 访问日志中解析到后端服务", domain)
		} else {
			names := make([]string, 0, len(backends))
			for _, b := range backends {
				if b.Service != "" {
					names = append(names, b.Service)
				} else {
					names = append(names, b.TargetIP)
				}
			}
			response = fmt.Sprintf("域名 %s 的后端服务: %s", domain, strings.Join(names, ", "))
		}
		return &ExecutionResult{Success: true, Response: response, Data: data}, nil
	}

	if modelUID := getString(task.Params, "model_uid"); modelUID != "" {
		limit := 50
		if v, ok := task.Params["limit"].(float64); ok && int(v) > 0 {
			limit = int(v)
		}
		instances, total, err := t.cmdb.ListInstances(ctx, t.tenantID, modelUID, getString(task.Params, "asset_name"), getString(task.Params, "provider"), limit)
		if err != nil {
			return &ExecutionResult{Success: false, Error: err}, nil
		}
		return &ExecutionResult{
			Success:  true,
			Response: fmt.Sprintf("模型 %s 共 %d 个实例(返回 %d 个)", modelUID, total, len(instances)),
			Data:     map[string]interface{}{"instances": instances, "total": total, "count": len(instances)},
		}, nil
	}

	models, err := t.cmdb.ListModels(ctx)
	if err != nil {
		return &ExecutionResult{Success: false, Error: err}, nil
	}
	return &ExecutionResult{
		Success:  true,
		Response: fmt.Sprintf("CMDB 模型清单 %d 个", len(models)),
		Data:     map[string]interface{}{"models": models, "count": len(models)},
	}, nil
}
