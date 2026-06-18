# OpenAI 兼容大模型接入指南

本指南介绍如何接入 OpenAI 兼容的大模型，包括阿里云百炼和火山引擎方舟。

## 支持的平台

| 平台 | 兼容性 | 端点示例 |
|------|--------|----------|
| **阿里云百炼** | ✅ OpenAI 兼容 | `https://dashscope.aliyuncs.com/compatible-mode/v1` |
| **火山方舟** | ✅ OpenAI 兼容 | `https://ark.cn-beijing.volces.com/api/v3` |
| **OpenAI** | ✅ 原生 | `https://api.openai.com/v1` |
| **Azure OpenAI** | ✅ 兼容 | `https://{resource}.openai.azure.com/openai/deployments/{deployment}` |
| **其他兼容服务** | ✅ 标准 OpenAI 格式 | 自定义端点 |

## 快速配置

### 1. 阿里云百炼

```env
# LLM Provider (必填)
LLM_PROVIDER=dashscope

# API Endpoint (必填)
LLM_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1

# API Key (必填)
LLM_API_KEY=sk-xxxxx

# 模型名称 (必填)
LLM_MODEL=qwen-plus
# 可选模型: qwen-turbo, qwen-plus, qwen-max

# 最大 Token 数 (可选)
LLM_MAX_TOKENS=4096

# 温度参数 (可选)
LLM_TEMPERATURE=0.7
```

### 2. 火山引擎方舟

```env
# LLM Provider
LLM_PROVIDER=volcengine

# API Endpoint
LLM_BASE_URL=https://ark.cn-beijing.volces.com/api/v3

# API Key
LLM_API_KEY=xxxxx

# 模型名称
LLM_MODEL=ep-xxxxx  # 推理接入点 ID

# 或使用预置模型
# LLM_MODEL=doubao-pro-32k
```

### 3. 通用 OpenAI 兼容服务

```env
LLM_PROVIDER=openai
LLM_BASE_URL=https://your-endpoint.com/v1
LLM_API_KEY=your-key
LLM_MODEL=gpt-3.5-turbo
```

## 配置文件方式

编辑 `config/settings.yaml`:

```yaml
agent:
  model: qwen-plus
  
llm:
  provider: dashscope
  base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
  api_key: ${LLM_API_KEY}
  model: qwen-plus
  max_tokens: 4096
  temperature: 0.7
  timeout: 120
```

## 获取 API Key

### 阿里云百炼

1. 访问 [阿里云百炼控制台](https://bailian.console.aliyun.com/)
2. 开通服务并创建 API Key
3. 选择模型: `qwen-turbo`, `qwen-plus`, `qwen-max`

### 火山引擎方舟

1. 访问 [火山引擎方舟控制台](https://console.volcengine.com/ark)
2. 创建推理接入点获取 `ep-xxxxx` ID
3. 或使用预置模型: `doubao-pro-32k`, `doubao-lite-4k`

## 模型映射

| Anthropic Claude | 阿里云百炼 | 火山方舟 |
|-----------------|-----------|---------|
| claude-3-haiku | qwen-turbo | doubao-lite-4k |
| claude-3-sonnet | qwen-plus | doubao-pro-4k |
| claude-3-opus | qwen-max | doubao-pro-32k |
| claude-3.5-sonnet | qwen-plus | doubao-pro-32k |

## 代码实现位置

```
internal/llm/
├── client.go          # OpenAI 兼容客户端
├── client_test.go     # 客户端测试
├── config.go          # LLM 配置
└── providers.go       # 预定义提供商配置
```

## 示例代码

```go
package main

import (
    "context"
    "fmt"
    
    "github.com/Havens-blog/OpsAgent/internal/llm"
)

func main() {
    // 创建客户端
    client := llm.NewClient(&llm.Config{
        Provider:  "dashscope",
        BaseURL:   "https://dashscope.aliyuncs.com/compatible-mode/v1",
        APIKey:    "sk-xxxxx",
        Model:     "qwen-plus",
        MaxTokens: 4096,
    })
    
    // 发送请求
    response, err := client.Chat(context.Background(), []llm.Message{
        {Role: "user", Content: "你好"},
    })
    
    if err != nil {
        panic(err)
    }
    
    fmt.Println(response.Content)
}
```

## 工具调用 (Function Calling)

OpenAI 兼容格式支持工具调用：

```go
tools := []llm.Tool{
    {
        Type: "function",
        Function: llm.FunctionDefinition{
            Name:        "query_logs",
            Description: "查询日志",
            Parameters: map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "query": map[string]interface{}{
                        "type":        "string",
                        "description": "日志查询语句",
                    },
                },
                "required": []string{"query"},
            },
        },
    },
}

response, err := client.ChatWithTools(ctx, messages, tools)
```

## 流式输出

```go
err := client.ChatStream(ctx, messages, func(chunk llm.ChatChunk) {
    fmt.Print(chunk.Content)
})
```

## 错误处理

```go
response, err := client.Chat(ctx, messages)

if err != nil {
    switch e := err.(type) {
    case *llm.APIError:
        // API 错误
        fmt.Printf("API Error: %s (code: %d)\n", e.Message, e.Code)
    case *llm.RateLimitError:
        // 速率限制
        fmt.Printf("Rate limited, retry after: %v\n", e.RetryAfter)
    default:
        // 其他错误
        fmt.Printf("Error: %v\n", err)
    }
}
```

## 测试连接

```powershell
# 设置环境变量
$env:LLM_PROVIDER="dashscope"
$env:LLM_BASE_URL="https://dashscope.aliyuncs.com/compatible-mode/v1"
$env:LLM_API_KEY="sk-xxxxx"
$env:LLM_MODEL="qwen-plus"

# 测试
go run cmd/main.go version
```
