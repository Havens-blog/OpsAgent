// Package interfaces provides HTTP handlers for the OpsAgent API.
package interfaces

import (
	"context"

	"github.com/Havens-blog/OpsAgent/internal/agent"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/llm"
	"github.com/Havens-blog/OpsAgent/internal/tool"
)

// sessionIDKey carries the conversation id through a chat tool-execution
// context so approvals can be scoped to a conversation.
type sessionIDKey struct{}

func withSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, id)
}

func sessionIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(sessionIDKey{}).(string)
	return id
}

// registryToolProvider adapts a tool.Registry to the chat.ToolProvider
// interface and enforces approval before executing confirmation-required tools.
type registryToolProvider struct {
	registry    *tool.Registry
	config      map[string]interface{}
	approvalMgr *guardrails.ApprovalManager
}

func newRegistryToolProvider(reg *tool.Registry, config map[string]interface{}, approvalMgr *guardrails.ApprovalManager) *registryToolProvider {
	return &registryToolProvider{registry: reg, config: config, approvalMgr: approvalMgr}
}

func (p *registryToolProvider) ToolSpecsForLLM() ([]llm.Tool, error) {
	specs, err := p.registry.GetToolSpecsForLLM(p.config)
	if err != nil {
		return nil, err
	}

	tools := make([]llm.Tool, 0, len(specs))
	for _, spec := range specs {
		name, _ := spec["name"].(string)
		description, _ := spec["description"].(string)
		params, _ := spec["input_schema"].(map[string]interface{})

		tools = append(tools, llm.Tool{
			Type: "function",
			Function: llm.FunctionDefinition{
				Name:        name,
				Description: description,
				Parameters:  params,
			},
		})
	}
	return tools, nil
}

func (p *registryToolProvider) ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	t, err := p.registry.Get(name)
	if err != nil {
		return nil, err
	}

	if t.Metadata.RequiresConfirmation() {
		req := guardrails.ApprovalRequest{
			ToolName:  name,
			Operation: "execute",
			Params:    args,
			Reason:    t.Metadata.ApprovalReason,
			Scope:     approvalScope(t.Metadata.ApprovalScope),
			SessionID: sessionIDFromCtx(ctx),
		}
		if t.Metadata.ApprovalExpirySec > 0 {
			req.ExpirySeconds = t.Metadata.ApprovalExpirySec
		}
		if err := p.approvalMgr.RequireApproval(req); err != nil {
			return nil, err
		}
	}

	return t.Execute(ctx, args, p.config)
}

func approvalScope(s string) guardrails.ApprovalScope {
	if s == string(guardrails.ScopeSession) {
		return guardrails.ScopeSession
	}
	return guardrails.ScopeOneShot
}

// buildChatToolRegistry registers the read-only specialist tools plus a mock
// confirmation-required tool so the approval path is reachable end-to-end.
func buildChatToolRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	for _, t := range agent.CreateLogAnalystTools() {
		_ = reg.Register(t)
	}
	for _, t := range agent.CreateMonitorTools() {
		_ = reg.Register(t)
	}
	for _, t := range agent.CreateInspectorTools() {
		_ = reg.Register(t)
	}
	_ = reg.Register(mockRestartServiceTool())
	return reg
}

func mockRestartServiceTool() *tool.Tool {
	inputSchema := `{
		"type": "object",
		"properties": {
			"service": {"type": "string", "description": "Service name to restart"}
		},
		"required": ["service"]
	}`

	return &tool.Tool{
		Metadata: &tool.ToolMetadata{
			Name:              "restart_service",
			Description:       "Restart a production service (requires approval)",
			Source:            tool.DataSourceTypeGeneric,
			InputSchema:       []byte(inputSchema),
			Tier:              guardrails.TierElevated,
			SideEffect:        guardrails.SideEffectMutating,
			RequiresApproval:  true,
			ApprovalReason:    "重启服务可能影响线上流量",
			ApprovalExpirySec: 300,
			ApprovalScope:     string(guardrails.ScopeOneShot),
			Category:          "operations",
		},
		IsAvailable: func(config map[string]interface{}) bool { return true },
		Run: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{
				"status":  "restarted",
				"service": params["service"],
			}, nil
		},
	}
}
