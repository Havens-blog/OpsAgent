# LLM 配置指南

OpsAgent 支持通过配置文件或环境变量配置多种 OpenAI 兼容的大语言模型。

## 支持的提供商

| 提供商 | Provider | 默认模型 | Base URL |
|--------|----------|----------|----------|
| 阿里云百炼 | `dashscope` | `qwen-plus` | `https://dashscope.aliyuncs.com/compatible-mode/v1` |
| 火山引擎方舟 | `volcengine` | `doubao-pro-32k` | `https://ark.cn-beijing.volces.com/api/v3` |
| DeepSeek | `deepseek` | `deepseek-chat` | `https://api.deepseek.com/v1` |
| 智谱AI | `zhipu` | `glm-4` | `https://open.bigmodel.cn/api/paas/v4` |
| Moonshot AI | `moonshot` | `moonshot-v1-8k` | `https://api.moonshot.cn/v1` |
| 百川智能 | `baichuan` | `Baichuan2-Turbo` | `https://api.baichuan-ai.com/v1` |
| MiniMax | `minimax` | `abab6.5s-chat` | `https://api.minimax.chat/v1` |
| OpenAI | `openai` | `gpt-3.5-turbo` | `https://api.openai.com/v1` |
| Azure OpenAI | `azure` | `gpt-35-turbo` | `https://{resource}.openai.azure.com/openai/deployments/{deployment}` |

## 配置方式

### 方式 1: 配置文件 (推荐)

编辑 `config/settings.yaml`:

```yaml
llm:
  provider: dashscope
  base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
  api_key: ${LLM_API_KEY}  # 从环境变量读取
  model: qwen-plus
  max_tokens: 4096
  temperature: 0.7
  timeout: 120
```

### 方式 2: 环境变量

```bash
# Windows PowerShell
$env:LLM_PROVIDER = "dashscope"
$env:LLM_API_KEY = "sk-your-api-key-here"
$env:LLM_MODEL = "qwen-plus"

# Linux/Mac
export LLM_PROVIDER=dashscope
export LLM_API_KEY=sk-your-api-key-here
export LLM_MODEL=qwen-plus
```

### 方式 3: .env 文件

创建 `.env` 文件:

```bash
LLM_PROVIDER=dashscope
LLM_API_KEY=sk-your-api-key-here
LLM_MODEL=qwen-plus
```

## 快速开始

### 1. 配置阿里云百炼

```yaml
llm:
  provider: dashscope
  api_key: ${LLM_API_KEY}
  model: qwen-plus
```

设置环境变量:
```bash
$env:LLM_API_KEY = "sk-xxx"
```

### 2. 配置火山引擎方舟

```yaml
llm:
  provider: volcengine
  base_url: https://ark.cn-beijing.volces.com/api/v3
  api_key: ${LLM_API_KEY}
  model: ep-xxxxx  # 使用推理接入点 ID
```

### 3. 测试连接

```bash
go run cmd/llm_test/main.go
```

## 环境变量优先级

1. 环境变量优先级最高
2. 配置文件次之
3. 代码默认值最低

例如，如果同时设置了:
- 配置文件: `model: qwen-plus`
- 环境变量: `LLM_MODEL=qwen-max`

则环境变量 `qwen-max` 会被使用。

## 配置参数说明

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `provider` | string | `dashscope` | LLM 提供商名称 |
| `base_url` | string | (自动) | API 端点，不填则使用预设值 |
| `api_key` | string | (必填) | API 密钥 |
| `model` | string | (自动) | 模型名称，不填则使用预设值 |
| `max_tokens` | int | `4096` | 最大输出 token 数 |
| `temperature` | float64 | `0.7` | 温度参数 (0-2) |
| `timeout` | int | `120` | 请求超时时间(秒) |

## 代码示例

### 使用配置文件

```go
import (
    "github.com/Havens-blog/OpsAgent/internal/config"
    "github.com/Havens-blog/OpsAgent/internal/llm"
)

// 加载配置
settings, _ := config.Load("./config")
llmConfig := settings.ToLLMConfig()

// 创建客户端
client := llm.NewClient(&llm.Config{
    Provider:    llmConfig.Provider,
    BaseURL:     llmConfig.BaseURL,
    APIKey:      llmConfig.APIKey,
    Model:       llmConfig.Model,
    MaxTokens:   llmConfig.MaxTokens,
    Temperature: llmConfig.Temperature,
    Timeout:     time.Duration(llmConfig.Timeout) * time.Second,
})

// 调用 LLM
response, _ := client.Chat(ctx, messages)
```

### 使用环境变量

```go
import "github.com/Havens-blog/OpsAgent/internal/llm"

// 从环境变量加载
config := llm.ConfigFromEnv()
client := llm.NewClient(config)

// 调用 LLM
response, _ := client.Chat(ctx, messages)
```

## 兼容性说明

所有提供商都使用 OpenAI 兼容的 API 格式：
- 端点: `/chat/completions`
- 请求体: 符合 OpenAI ChatCompletion 规范
- 响应体: 符合 OpenAI ChatCompletionResponse 规范

如果您的提供商使用不同的 API 格式，需要自定义适配。
