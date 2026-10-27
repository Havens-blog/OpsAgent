# MCP 集成指南

OpsAgent 支持 MCP (Model Context Protocol) 协议，可以：

1. **作为 MCP 服务器** - 暴露 OpsAgent 能力给外部使用
2. **作为 MCP 客户端** - 调用外部 MCP 工具（如联网搜索）

## 配置方式

### 方式一：OpsAgent 作为 MCP 服务器

在 `~/.claude.json` 中添加 OpsAgent MCP 服务器：

```json
{
  "opsagent": {
    "command": "go",
    "args": [
      "run",
      "f:/OpsAgent/cmd/mcp/main.go",
      "--server",
      "--port", "8081"
    ],
    "env": {
      "LLM_PROVIDER": "dashscope",
      "LLM_API_KEY": "your-key"
    }
  }
}
```

### 方式二：配置外部 MCP 工具

在 OpsAgent 配置文件中添加 MCP 客户端：

```yaml
# config/settings.yaml

mcp:
  clients:
    # Exa 搜索
    exa:
      base_url: "http://localhost:3000"

    # 自定义 MCP 服务器
    custom-tools:
      base_url: "http://localhost:8082"
```

## 使用场景

### 场景 1：在 RCA 中使用联网搜索

Agent 在分析故障时，可以联网搜索相关知识：

```
用户: order-service 出现连接超时

OpsAgent:
1. 收集日志、指标...
2. 需要了解 "PostgreSQL 连接池最佳实践"
3. 调用 exa-search: web_search_exa("PostgreSQL connection pool best practices")
4. 结合搜索结果给出更精准的建议
```

### 场景 2：查找代码示例

遇到代码问题时，可以搜索相关示例：

```
用户: 如何优化 Go HTTP 客户端性能？

OpsAgent:
1. 调用 get_code_context_exa("Go HTTP client optimization patterns")
2. 搜索 GitHub 上的示例代码
3. 结合 StackOverflow 的讨论
4. 给出具体的优化建议
```

## MCP 工具映射

| 外部工具 | OpsAgent 工具名 | 用途 |
|---------|----------------|------|
| exa web search | `exa_web_search_exa` | 联网搜索 |
| exa code search | `exa_get_code_context_exa` | 代码示例搜索 |
| exa company research | `exa_company_research_exa` | 公司研究 |
| OpsAgent RCA | `opsagent_rca_analyze` | 根因分析 |
| OpsAgent Logs | `opsagent_log_search` | 日志搜索 |
| OpsAgent Metrics | `opsagent_metrics_query` | 指标查询 |

## 代码示例

### 在 Agent 中使用 MCP 工具

```go
package agent

import (
    "github.com/Havens-blog/OpsAgent/internal/mcp"
    "github.com/Havens-blog/OpsAgent/internal/tool"
)

// SetupMCPSets up MCP integration
func (a *Agent) SetupMCP() error {
    // 创建 MCP 多客户端
    multiClient := mcp.NewMultiClient()

    // 注册 Exa MCP 服务器
    exaClient := mcp.NewClient("http://localhost:3000")
    multiClient.Register("exa", exaClient)

    // 创建 MCP 桥接器
    bridge := tool.NewMCPBridge(multiClient)

    // 设置外部工具
    if err := bridge.SetupExternalTools(); err != nil {
        return err
    }

    // 注册工具到工具系统
    for _, t := range bridge.ListTools() {
        a.toolRegistry.Register(t)
    }

    return nil
}
```

### Agent 调用联网搜索

```go
// 在 Agent 执行过程中调用
func (a *Agent) searchWeb(query string) (string, error) {
    result, err := a.toolRegistry.Call(context.Background(), "exa_web_search_exa", map[string]interface{}{
        "query":      query,
        "numResults": 5,
    })
    if err != nil {
        return "", err
    }

    // 解析搜索结果
    // ...
    return formattedResult, nil
}
```

## 配置 Exa MCP 服务器

1. 获取 API Key: https://exa.ai
2. 在 `~/.claude.json` 中配置：

```json
{
  "exa-web-search": {
    "command": "npx",
    "args": ["-y", "exa-mcp-server"],
    "env": {
      "EXA_API_KEY": "your-exa-api-key"
    }
  }
}
```

## 启动 OpsAgent MCP 服务器

```bash
# 启动 MCP 服务器模式
go run cmd/mcp/main.go --port 8081

# 服务器将监听:
# - http://localhost:8081/mcp/tools (列出工具)
# - http://localhost:8081/mcp/call (调用工具)
```

## 验证集成

### 测试 OpsAgent 作为 MCP 服务器

```bash
curl http://localhost:8081/mcp/tools
```

### 测试 OpsAgent 调用外部 MCP

```go
// 在代码中测试
multiClient := mcp.NewMultiClient()
multiClient.Register("exa", mcp.NewClient("http://localhost:3000"))

result, err := multiClient.CallTool(ctx, "exa", "web_search_exa", map[string]interface{}{
    "query": "Golang patterns 2024",
})
```

## 架构图

```
┌─────────────────────────────────────────────────┐
│              Claude Code / 其他客户端             │
└────────────────┬────────────────────────────────┘
                 │ MCP 协议
                 ↓
┌─────────────────────────────────────────────────┐
│            OpsAgent MCP 服务器                  │
│  ├─ RCA 工具                                    │
│  ├─ 日志搜索工具                                │
│  ├─ 指标查询工具                                │
│  └─ 拓扑查询工具                                │
└─────────────────────────────────────────────────┘
                 │ MCP 客户端
                 ↓
┌─────────────────────────────────────────────────┐
│           外部 MCP 服务器 (如 Exa)              │
│  ├─ 网页搜索                                    │
│  ├─ 代码搜索                                    │
│  └─ 公司研究                                    │
└─────────────────────────────────────────────────┘
```

## 注意事项

1. **安全性**: MCP 通信通常在本地，无需额外认证
2. **性能**: MCP 调用有网络开销，考虑缓存结果
3. **错误处理**: 外部工具可能不可用，需要降级处理
4. **成本**: Exa 等 API 有调用限制和费用
