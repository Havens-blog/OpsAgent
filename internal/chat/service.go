// Package chat provides the conversational assistant service for OpsAgent.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/guardrails"
	"github.com/Havens-blog/OpsAgent/internal/llm"
)

const (
	// DefaultSystemPrompt is the default persona for OpsAgent.
	DefaultSystemPrompt = `你是 OpsAgent 运维助手，一个多云运维智能助手。

你可以帮助用户：
- 分析告警与故障
- 梳理排查思路
- 理解服务拓扑与依赖关系
- 审查变更历史
- 查询实时数据（WAF 拦截日志、服务日志、指标、拓扑等）

重要规则：
- 当用户询问某个域名的 WAF 拦截、Web 攻击、异常访问时，必须先调用 query_waf_logs 工具查询该域名的真实拦截日志，再基于返回的攻击类型/来源IP/目标URI/动作给出事实性回答，禁止凭空给通用检查清单。
- 查询实时数据时优先调用对应工具获取事实，基于事实作答；工具查不到或未配置时才如实说明，不要编造数据。
- 查询云资源/CMDB 清单（CDN、ECS、RDS、主机等）时，先调用 list_cmdb_instances 或 resource_summary 工具。实例数量必须用工具返回的 total（总数）与 count（本次返回条数），严禁把查询参数 limit（默认 50）当作实例数量。只报告每个实例的 asset_name 及其 attributes 中实际出现的字段值；工具未返回的字段（如 status、cname、权重、https、service_area、business_type、origin_type 等）一律不得自行补全或编造。工具未配置或返回空时，必须如实说明“未配置/无数据”，不得编造实例。
请用简洁专业的中文回答。`
)

// ChatClient is the narrow LLM client surface the Service depends on.
type ChatClient interface {
	Chat(ctx context.Context, messages []llm.Message) (*llm.ChatResponse, error)
	ChatWithTools(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error)
	ChatStream(ctx context.Context, messages []llm.Message, handler func(llm.ChatChunk)) (*llm.ChatResponse, error)
}

// ToolProvider supplies the LLM-visible tool set and executes tool calls. The
// chat package depends on this narrow interface instead of internal/agent or
// internal/tool, keeping the dependency graph free of cycles.
type ToolProvider interface {
	ToolSpecsForLLM() ([]llm.Tool, error)
	ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error)
	// ToolSource returns the registered tool's data-source string, or "" when
	// the tool is not registered.
	ToolSource(name string) string
}

// StreamHandler receives lifecycle events emitted by StreamWithTools.
type StreamHandler struct {
	OnDelta            func(string) error
	OnToolCall         func(callID string, name string, params map[string]interface{}) error
	OnToolResult       func(callID string, name string, status string, result string, errMsg string, evidence ToolEvidence) error
	OnApprovalRequired func(req guardrails.ApprovalRequest) error
}

// Service provides conversational AI capabilities.
type Service struct {
	client       ChatClient
	toolProvider ToolProvider
	systemPrompt string
	available    bool
	model        string
}

// SetToolProvider injects the tool provider used by StreamWithTools.
func (s *Service) SetToolProvider(p ToolProvider) {
	s.toolProvider = p
}

// Message represents a chat message, both as LLM input and persisted history.
type Message struct {
	ID              string         `json:"id,omitempty"`
	ConversationID  string         `json:"conversation_id,omitempty"`
	Role            string         `json:"role"` // "user" or "assistant"
	Content         string         `json:"content"`
	ClientMessageID string         `json:"client_message_id,omitempty"`
	Evidence        []ToolEvidence `json:"evidence,omitempty"`
	Grounding       string         `json:"grounding,omitempty"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
}

// NewService creates a new chat service from LLM configuration.
// Returns nil service if LLM is not configured, allowing graceful fallback.
func NewService(cfg config.LLMConfig) (*Service, error) {
	// Validate API key
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("LLM_API_KEY not configured")
	}

	// Detect placeholder keys
	if isPlaceholderKey(cfg.APIKey) {
		return nil, fmt.Errorf("LLM_API_KEY is still a placeholder (sk-your-api-key-here)")
	}

	// Build llm.Config
	llmCfg := &llm.Config{
		Provider:    cfg.Provider,
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
	}

	// Apply provider defaults if base_url/model not set
	if llmCfg.BaseURL == "" || llmCfg.Model == "" {
		if provider, ok := llm.GetProvider(llmCfg.Provider); ok {
			if llmCfg.BaseURL == "" {
				llmCfg.BaseURL = provider.BaseURL
			}
			if llmCfg.Model == "" {
				llmCfg.Model = provider.DefaultModel
			}
		}
	}

	// Set timeout (seconds -> time.Duration)
	if cfg.Timeout > 0 {
		llmCfg.Timeout = time.Duration(cfg.Timeout) * time.Second
	} else {
		llmCfg.Timeout = 120 * time.Second
	}

	// Validate required fields
	if llmCfg.APIKey == "" {
		return nil, fmt.Errorf("LLM_API_KEY is required")
	}
	if llmCfg.Model == "" {
		return nil, fmt.Errorf("LLM_MODEL is required")
	}

	client := llm.NewClient(llmCfg)

	return &Service{
		client:       client,
		systemPrompt: DefaultSystemPrompt,
		available:    true,
		model:        llmCfg.Model,
	}, nil
}

// NewServiceWithClient builds a Service around an injected ChatClient. It is a
// test hook; production wiring uses NewService.
func NewServiceWithClient(client ChatClient, model string) *Service {
	return &Service{
		client:       client,
		systemPrompt: DefaultSystemPrompt,
		available:    true,
		model:        model,
	}
}

// IsAvailable returns whether the service is ready to handle requests.
func (s *Service) IsAvailable() bool {
	return s != nil && s.available
}

// GetModel returns the model being used.
func (s *Service) GetModel() string {
	if s == nil {
		return ""
	}
	return s.model
}

// Chat sends a conversation to the LLM and returns the response.
// Messages should include the conversation history (user + assistant roles).
// The service automatically prepends the system prompt.
func (s *Service) Chat(ctx context.Context, messages []Message) (string, error) {
	if !s.IsAvailable() {
		return "", fmt.Errorf("chat service not available")
	}

	// Convert to llm.Message format, prepending system prompt
	llmMessages := make([]llm.Message, 0, len(messages)+1)
	llmMessages = append(llmMessages, llm.Message{
		Role:    "system",
		Content: s.systemPrompt,
	})
	for _, m := range messages {
		llmMessages = append(llmMessages, llm.Message{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	response, err := s.client.Chat(ctx, llmMessages)
	if err != nil {
		return "", fmt.Errorf("LLM call failed: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("empty response from LLM")
	}

	return response.Choices[0].Message.Content, nil
}

// ChatStream streams a reply, invoking onDelta with each text delta. The
// system prompt is prepended to the supplied history. An error returned by
// onDelta aborts consumption; ctx cancellation is propagated as ctx.Err().
func (s *Service) ChatStream(ctx context.Context, messages []Message, onDelta func(string) error) error {
	if !s.IsAvailable() {
		return fmt.Errorf("chat service not available")
	}
	return s.streamLLM(ctx, s.buildLLMMessages(messages), onDelta)
}

// StreamWithTools runs a multi-round tool-calling loop and then streams the
// final answer. Each round asks the model (with tools) what to do next: if it
// requests tools, they are executed and their results are appended, then the
// loop asks again so the model can chain a second tool on the first result
// (e.g. list_cmdb_models -> list_cmdb_instances). When the model stops
// requesting tools, the accumulated context is streamed as the final answer.
// The returned evidence and grounding are persisted and exposed over SSE.
func (s *Service) StreamWithTools(ctx context.Context, messages []Message, h StreamHandler) (string, []ToolEvidence, string, error) {
	if !s.IsAvailable() {
		return "", nil, "", fmt.Errorf("chat service not available")
	}

	required := mustQueryTools(latestUserText(messages))
	var evidence []ToolEvidence
	llmMessages := s.buildLLMMessages(messages)

	var tools []llm.Tool
	if s.toolProvider != nil {
		var err error
		tools, err = s.toolProvider.ToolSpecsForLLM()
		if err != nil {
			return "", nil, "", fmt.Errorf("failed to load tools: %w", err)
		}
	}

	if len(tools) == 0 {
		return s.finishAnswer(ctx, llmMessages, h, required, evidence)
	}

	// Cap the rounds so a model that keeps calling tools can never loop forever.
	const maxRounds = 6
	for round := 0; round < maxRounds; round++ {
		resp, err := s.client.ChatWithTools(ctx, llmMessages, tools)
		if err != nil {
			if ctx.Err() != nil {
				return "", evidence, computeGrounding(required, evidence), ctx.Err()
			}
			return "", evidence, computeGrounding(required, evidence), fmt.Errorf("LLM call failed: %w", err)
		}
		if len(resp.Choices) == 0 {
			return "", evidence, computeGrounding(required, evidence), fmt.Errorf("empty response from LLM")
		}

		assistantMsg := resp.Choices[0].Message
		if len(assistantMsg.ToolCalls) == 0 {
			return s.finishAnswer(ctx, llmMessages, h, required, evidence)
		}

		// Record the assistant's tool-call request so the next round sees it.
		llmMessages = append(llmMessages, llm.Message{
			Role:      "assistant",
			Content:   assistantMsg.Content,
			ToolCalls: assistantMsg.ToolCalls,
		})

		for _, call := range assistantMsg.ToolCalls {
			args, err := parseToolArguments(call.Function.Arguments)
			if err != nil {
				if h.OnToolResult != nil {
					_ = h.OnToolResult(call.ID, call.Function.Name, "failed", "", err.Error(), ToolEvidence{})
				}
				llmMessages = append(llmMessages, llm.Message{
					Role:       "tool",
					Content:    fmt.Sprintf("error: %v", err),
					ToolCallID: call.ID,
				})
				continue
			}

			if h.OnToolCall != nil {
				if err := h.OnToolCall(call.ID, call.Function.Name, args); err != nil {
					return "", evidence, computeGrounding(required, evidence), err
				}
			}

			result, execErr := s.toolProvider.ExecuteTool(ctx, call.Function.Name, args)
			if execErr != nil {
				if guardrails.ApprovalRequired(execErr) {
					if h.OnApprovalRequired != nil {
						if ar, ok := execErr.(*guardrails.ApprovalRequiredError); ok {
							_ = h.OnApprovalRequired(ar.Request)
						}
					}
					return "", evidence, computeGrounding(required, evidence), fmt.Errorf("需要审批，请在前端批准后重试")
				}

				status := s.classifyToolError(call.Function.Name, execErr)
				ev := ToolEvidence{
					EvidenceID: newEvidenceID(),
					ToolName:   call.Function.Name,
					Status:     status,
					Source:     s.toolSource(call.Function.Name),
					Params:     args,
					Error:      execErr.Error(),
					At:         time.Now(),
				}
				evidence = append(evidence, ev)
				if h.OnToolResult != nil {
					_ = h.OnToolResult(call.ID, call.Function.Name, "failed", "", execErr.Error(), ev)
				}
				llmMessages = append(llmMessages, llm.Message{
					Role:       "tool",
					Content:    fmt.Sprintf("error: %v", execErr),
					ToolCallID: call.ID,
				})
				continue
			}

			ev := ToolEvidence{
				EvidenceID:   newEvidenceID(),
				ToolName:     call.Function.Name,
				Status:       "success",
				Source:       s.toolSource(call.Function.Name),
				Params:       args,
				ResultDigest: toolDigest(call.Function.Name, result),
				At:           time.Now(),
			}
			evidence = append(evidence, ev)

			resultStr := stringifyToolResult(result)
			if h.OnToolResult != nil {
				_ = h.OnToolResult(call.ID, call.Function.Name, "success", resultStr, "", ev)
			}
			llmMessages = append(llmMessages, llm.Message{
				Role:       "tool",
				Content:    resultStr,
				ToolCallID: call.ID,
			})
		}
	}

	// Exhausted the round budget: stream whatever the accumulated tool results
	// support rather than leaving the user with no answer.
	return s.finishAnswer(ctx, llmMessages, h, required, evidence)
}

// finishAnswer streams the final reply, then applies the unsupported suffix and
// grounding decision before returning to the caller.
func (s *Service) finishAnswer(ctx context.Context, llmMessages []llm.Message, h StreamHandler, required []string, evidence []ToolEvidence) (string, []ToolEvidence, string, error) {
	text, err := s.streamAnswer(ctx, llmMessages, h)
	if err != nil {
		return "", evidence, computeGrounding(required, evidence), err
	}
	grounding := computeGrounding(required, evidence)
	if grounding == "unsupported" {
		text += "\n\n⚠️ 本次回答未取得相关数据源的工具结果（未接入或查询失败），以上内容可信度低，请以工具/控制台实际数据为准。"
	}
	return text, evidence, grounding, nil
}

// toolSource returns the registered tool's source, guarding a nil provider.
func (s *Service) toolSource(name string) string {
	if s.toolProvider == nil {
		return ""
	}
	return s.toolProvider.ToolSource(name)
}

// classifyToolError maps an execution error to an evidence status. Missing and
// unavailable tools are distinguished from genuine execution failures.
func (s *Service) classifyToolError(name string, err error) string {
	if s.toolSource(name) == "" {
		return "unavailable"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "not available") ||
		strings.Contains(msg, "not configured") ||
		strings.Contains(msg, "未配置") ||
		strings.Contains(msg, "未接入") {
		return "unavailable"
	}
	return "failed"
}

// latestUserText returns the newest user message text, or "" when absent.
func latestUserText(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// streamAnswer streams the final reply and accumulates it for persistence.
func (s *Service) streamAnswer(ctx context.Context, llmMessages []llm.Message, h StreamHandler) (string, error) {
	var full strings.Builder
	err := s.streamLLM(ctx, llmMessages, func(delta string) error {
		full.WriteString(delta)
		if h.OnDelta != nil {
			return h.OnDelta(delta)
		}
		return nil
	})
	return full.String(), err
}

// buildLLMMessages prepends the system prompt to the conversation history.
func (s *Service) buildLLMMessages(messages []Message) []llm.Message {
	llmMessages := make([]llm.Message, 0, len(messages)+1)
	llmMessages = append(llmMessages, llm.Message{
		Role:    "system",
		Content: s.systemPrompt,
	})
	for _, m := range messages {
		llmMessages = append(llmMessages, llm.Message{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	return llmMessages
}

// streamLLM streams the given llm messages and invokes onDelta per text delta.
func (s *Service) streamLLM(ctx context.Context, llmMessages []llm.Message, onDelta func(string) error) error {
	var deltaErr error
	_, err := s.client.ChatStream(ctx, llmMessages, func(chunk llm.ChatChunk) {
		if deltaErr != nil || len(chunk.Choices) == 0 || chunk.Choices[0].Delta == nil {
			return
		}
		if content := chunk.Choices[0].Delta.Content; content != "" {
			if e := onDelta(content); e != nil {
				deltaErr = e
			}
		}
	})
	if deltaErr != nil {
		return deltaErr
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("LLM stream failed: %w", err)
	}
	return nil
}

// parseToolArguments decodes an OpenAI tool-call arguments string.
func parseToolArguments(raw string) (map[string]interface{}, error) {
	args := map[string]interface{}{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return args, nil
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("invalid tool arguments: %w", err)
	}
	return args, nil
}

// stringifyToolResult renders a tool result for the LLM and SSE transport.
func stringifyToolResult(result interface{}) string {
	if result == nil {
		return ""
	}
	if b, err := json.Marshal(result); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", result)
}

// isPlaceholderKey detects common placeholder/invalid API keys.
func isPlaceholderKey(key string) bool {
	placeholders := []string{
		"sk-your-api-key-here",
		"your-api-key",
		"your-api-key-here",
		"placeholder",
		"none",
		"<your-api-key>",
	}
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, p := range placeholders {
		if lower == p || strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}
