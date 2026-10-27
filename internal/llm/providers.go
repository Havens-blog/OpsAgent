// Package llm provides predefined provider configurations.
package llm

import "os"

// Provider represents a predefined LLM provider.
type Provider struct {
	Name         string
	BaseURL      string
	DefaultModel string
	// Models is a non-exhaustive list of commonly used model ids for this
	// provider, used to populate the console dropdown. Model catalogs change
	// over time, so the UI also allows a free-text model id.
	Models []string
}

// All predefined providers in a single map.
var Providers = map[string]Provider{
	"claude": {
		Name:         "Claude (Anthropic)",
		BaseURL:      "https://api.anthropic.com",
		DefaultModel: "claude-sonnet-4-20250514",
		Models: []string{
			"claude-sonnet-4-20250514",
			"claude-3-5-sonnet-20241022",
			"claude-3-5-haiku-20241022",
			"claude-3-opus-20240229",
		},
	},
	"dashscope": {
		Name:         "阿里云百炼",
		BaseURL:      "https://dashscope.aliyuncs.com/compatible-mode/v1",
		DefaultModel: "qwen-plus",
		Models: []string{
			"qwen-max",
			"qwen-plus",
			"qwen-turbo",
			"qwen-long",
		},
	},
	"volcengine": {
		Name:         "火山引擎方舟",
		BaseURL:      "https://ark.cn-beijing.volces.com/api/v3",
		DefaultModel: "doubao-pro-32k",
		Models: []string{
			"doubao-pro-32k",
			"doubao-pro-128k",
			"doubao-lite-32k",
		},
	},
	"openai": {
		Name:         "OpenAI",
		BaseURL:      "https://api.openai.com/v1",
		DefaultModel: "gpt-4o-mini",
		Models: []string{
			"gpt-4o",
			"gpt-4o-mini",
			"gpt-4-turbo",
			"gpt-3.5-turbo",
		},
	},
	"azure": {
		Name:         "Azure OpenAI",
		BaseURL:      "https://{resource}.openai.azure.com/openai/deployments/{deployment}",
		DefaultModel: "gpt-35-turbo",
		Models: []string{
			"gpt-4o",
			"gpt-4-turbo",
			"gpt-35-turbo",
		},
	},
	"deepseek": {
		Name:         "DeepSeek",
		BaseURL:      "https://api.deepseek.com/v1",
		DefaultModel: "deepseek-chat",
		Models: []string{
			"deepseek-chat",
			"deepseek-reasoner",
		},
	},
	"moonshot": {
		Name:         "Moonshot AI",
		BaseURL:      "https://api.moonshot.cn/v1",
		DefaultModel: "moonshot-v1-8k",
		Models: []string{
			"moonshot-v1-8k",
			"moonshot-v1-32k",
			"moonshot-v1-128k",
		},
	},
	"zhipu": {
		Name:         "智谱AI",
		BaseURL:      "https://open.bigmodel.cn/api/paas/v4",
		DefaultModel: "glm-4",
		Models: []string{
			"glm-4-plus",
			"glm-4",
			"glm-4-air",
			"glm-4-flash",
		},
	},
	"baichuan": {
		Name:         "百川智能",
		BaseURL:      "https://api.baichuan-ai.com/v1",
		DefaultModel: "Baichuan2-Turbo",
		Models: []string{
			"Baichuan4",
			"Baichuan3-Turbo",
			"Baichuan2-Turbo",
		},
	},
	"minimax": {
		Name:         "MiniMax",
		BaseURL:      "https://api.minimax.chat/v1",
		DefaultModel: "abab6.5s-chat",
		Models: []string{
			"abab6.5s-chat",
			"abab6.5-chat",
		},
	},
}

// ProviderOrder is the stable display order for providers in the console.
var ProviderOrder = []string{
	"dashscope", "volcengine", "deepseek", "zhipu", "moonshot",
	"baichuan", "minimax", "claude", "openai", "azure",
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
