// Package chat provides the conversational assistant service for OpsAgent.
package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
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

请用简洁专业的中文回答。如果需要查询实时数据（日志、指标、拓扑等）但目前无法访问，请如实告知并给出可行的替代排查建议。`
)

// Service provides conversational AI capabilities.
type Service struct {
	client       *llm.Client
	systemPrompt string
	available    bool
	model        string
}

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
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
