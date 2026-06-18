// Package llm provides predefined provider configurations.
package llm

import "os"

// Provider represents a predefined LLM provider.
type Provider struct {
	Name         string
	BaseURL      string
	DefaultModel string
}

// All predefined providers in a single map.
var Providers = map[string]Provider{
	"claude": {
		Name:         "Claude (Anthropic)",
		BaseURL:      "https://api.anthropic.com",
		DefaultModel: "claude-sonnet-4-20250514",
	},
	"dashscope": {
		Name:         "阿里云百炼",
		BaseURL:      "https://dashscope.aliyuncs.com/compatible-mode/v1",
		DefaultModel: "qwen-plus",
	},
	"volcengine": {
		Name:         "火山引擎方舟",
		BaseURL:      "https://ark.cn-beijing.volces.com/api/v3",
		DefaultModel: "doubao-pro-32k",
	},
	"openai": {
		Name:         "OpenAI",
		BaseURL:      "https://api.openai.com/v1",
		DefaultModel: "gpt-3.5-turbo",
	},
	"azure": {
		Name:         "Azure OpenAI",
		BaseURL:      "https://{resource}.openai.azure.com/openai/deployments/{deployment}",
		DefaultModel: "gpt-35-turbo",
	},
	"deepseek": {
		Name:         "DeepSeek",
		BaseURL:      "https://api.deepseek.com/v1",
		DefaultModel: "deepseek-chat",
	},
	"moonshot": {
		Name:         "Moonshot AI",
		BaseURL:      "https://api.moonshot.cn/v1",
		DefaultModel: "moonshot-v1-8k",
	},
	"zhipu": {
		Name:         "智谱AI",
		BaseURL:      "https://open.bigmodel.cn/api/paas/v4",
		DefaultModel: "glm-4",
	},
	"baichuan": {
		Name:         "百川智能",
		BaseURL:      "https://api.baichuan-ai.com/v1",
		DefaultModel: "Baichuan2-Turbo",
	},
	"minimax": {
		Name:         "MiniMax",
		BaseURL:      "https://api.minimax.chat/v1",
		DefaultModel: "abab6.5s-chat",
	},
}

// ConfigFromEnv creates a config from environment variables.
func ConfigFromEnv() *Config {
	provider := os.Getenv("LLM_PROVIDER")
	baseURL := os.Getenv("LLM_BASE_URL")
	apiKey := os.Getenv("LLM_API_KEY")
	model := os.Getenv("LLM_MODEL")

	// Use predefined provider if available
	if provider != "" && baseURL == "" {
		if p, ok := Providers[provider]; ok {
			baseURL = p.BaseURL
			if model == "" {
				model = p.DefaultModel
			}
		}
	}

	// Fallback to Anthropic for backward compatibility
	if provider == "" {
		provider = "openai"
	}

	return &Config{
		Provider:    provider,
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Model:       model,
		MaxTokens:   4096,
		Temperature: 0.7,
	}
}

// GetProvider returns a provider by name.
func GetProvider(name string) (Provider, bool) {
	p, ok := Providers[name]
	return p, ok
}

// IsOpenAICompatible checks if a provider is OpenAI compatible.
func IsOpenAICompatible(provider string) bool {
	switch provider {
	case "dashscope", "volcengine", "openai", "azure", "deepseek", "moonshot", "zhipu", "baichuan", "minimax":
		return true
	default:
		return false
	}
}
