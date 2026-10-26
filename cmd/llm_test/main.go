// Package main provides LLM connection test utility.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Havens-blog/OpsAgent/internal/config"
	"github.com/Havens-blog/OpsAgent/internal/llm"
)

func main() {
	// Try to load from config directory first
	configDir := os.Getenv("OPSAGENT_CONFIG_DIR")
	if configDir == "" {
		// Default to ./config
		configDir = "./config"
	}

	settings, err := config.Load(configDir)
	if err != nil {
		fmt.Printf("⚠️  警告: 无法加载配置文件 (%v)\n", err)
		fmt.Println("使用环境变量配置...")
		testFromEnv()
		return
	}

	// Get LLM config
	llmConfig := settings.ToLLMConfig()

	if llmConfig.APIKey == "" {
		fmt.Println("❌ 错误: LLM_API_KEY 未设置")
		fmt.Println("\n请设置环境变量:")
		fmt.Println("  $env:LLM_API_KEY = 'your-api-key'")
		fmt.Println("\n或在配置文件中设置:")
		fmt.Println("  llm:")
		fmt.Println("    api_key: sk-your-api-key-here")
		os.Exit(1)
	}

	fmt.Println("=== LLM 连接测试 (从配置文件) ===")
	fmt.Printf("配置目录: %s\n", configDir)
	fmt.Printf("提供商: %s\n", llmConfig.Provider)
	fmt.Printf("端点: %s\n", llmConfig.BaseURL)
	fmt.Printf("模型: %s\n", llmConfig.Model)
	fmt.Printf("最大 Token: %d\n", llmConfig.MaxTokens)
	fmt.Printf("温度: %.1f\n", llmConfig.Temperature)
	fmt.Printf("超时: %d 秒\n", llmConfig.Timeout)
	fmt.Println()

	// Create LLM client config
	clientConfig := &llm.Config{
		Provider:    llmConfig.Provider,
		BaseURL:     llmConfig.BaseURL,
		APIKey:      llmConfig.APIKey,
		Model:       llmConfig.Model,
		MaxTokens:   llmConfig.MaxTokens,
		Temperature: llmConfig.Temperature,
		Timeout:     time.Duration(llmConfig.Timeout) * time.Second,
	}

	// Apply provider defaults if needed
	if clientConfig.BaseURL == "" {
		if provider, ok := llm.GetProvider(clientConfig.Provider); ok {
			clientConfig.BaseURL = provider.BaseURL
			if clientConfig.Model == "" {
				clientConfig.Model = provider.DefaultModel
			}
		}
	}

	// Create client
	client := llm.NewClient(clientConfig)

	// Test connection
	fmt.Println("▶️  测试连接...")

	messages := []llm.Message{
		{Role: "system", Content: "你是一个运维助手，请简洁回答。"},
		{Role: "user", Content: "你是哪个模型"},
	}

	response, err := client.Chat(context.Background(), messages)
	if err != nil {
		fmt.Printf("❌ 连接失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ 连接成功!")
	fmt.Println()
	fmt.Println("=== 响应 ===")
	if len(response.Choices) > 0 {
		fmt.Println(response.Choices[0].Message.Content)
	}
	fmt.Println()
	fmt.Println("=== Token 使用情况 ===")
	fmt.Printf("输入 Token: %d\n", response.Usage.PromptTokens)
	fmt.Printf("输出 Token: %d\n", response.Usage.CompletionTokens)
	fmt.Printf("总 Token: %d\n", response.Usage.TotalTokens)
}

func testFromEnv() {
	config := llm.ConfigFromEnv()

	if config.APIKey == "" {
		fmt.Println("❌ 错误: LLM_API_KEY 未设置")
		fmt.Println("\n请设置环境变量:")
		fmt.Println("  $env:LLM_API_KEY = 'your-api-key'")
		fmt.Println("\n或创建 config/settings.yaml 文件:")
		fmt.Println("  llm:")
		fmt.Println("    api_key: ${LLM_API_KEY}")
		os.Exit(1)
	}

	fmt.Println("=== LLM 连接测试 (从环境变量) ===")
	fmt.Printf("提供商: %s\n", config.Provider)
	fmt.Printf("端点: %s\n", config.BaseURL)
	fmt.Printf("模型: %s\n", config.Model)
	fmt.Printf("最大 Token: %d\n", config.MaxTokens)
	fmt.Printf("温度: %.1f\n", config.Temperature)
	fmt.Println()

	// Create client
	client := llm.NewClient(config)

	// Test connection
	fmt.Println("▶️  测试连接...")

	messages := []llm.Message{
		{Role: "system", Content: "你是一个运维助手，请简洁回答。"},
		{Role: "user", Content: "你好，请用一句话介绍你自己。"},
	}

	response, err := client.Chat(context.Background(), messages)
	if err != nil {
		fmt.Printf("❌ 连接失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ 连接成功!")
	fmt.Println()
	fmt.Println("=== 响应 ===")
	if len(response.Choices) > 0 {
		fmt.Println(response.Choices[0].Message.Content)
	}
	fmt.Println()
	fmt.Println("=== Token 使用情况 ===")
	fmt.Printf("输入 Token: %d\n", response.Usage.PromptTokens)
	fmt.Printf("输出 Token: %d\n", response.Usage.CompletionTokens)
	fmt.Printf("总 Token: %d\n", response.Usage.TotalTokens)
}
