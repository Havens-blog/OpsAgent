# OpsAgent 与 Claude Skills 互操作指南

## 概述

OpsAgent 通过 MCP (Model Context Protocol) 实现与其他 Claude Code skills 的双向互操作：

1. **OpsAgent 作为 MCP 服务器** - 暴露 AIOps 能力给外部使用
2. **OpsAgent 作为 MCP 客户端** - 调用外部 skills（如联网搜索）

## 架构图

```
┌─────────────────────────────────────────────────────────────┐
│                     Claude Code                             │
│                  (或其他 MCP 客户端)                         │
└────────────────────┬────────────────────────────────────────┘
                     │ MCP 协议
         ┌───────────┴───────────┐
         ↓                       ↓
┌─────────────────────┐   ┌─────────────────────────────────┐
│  OpsAgent MCP 服务�� │   │     Exa Search MCP 服务��       │
│  (暴露 AIOps 工具)    │   │     (外部联网搜索能力)           │
└──────────┬──────────┘   └─────────────────┬───────────────┘
           │                               │
    ┌──────┴──────┐                 ┌──────┴──────┐
    ↓             ↓                 ↓             ↓
┌────────┐  ┌─────────┐       ┌────────┐  ┌──────────┐
│ SLS    │  │Prometheus│       │ Web    │  │  Code    │
│ ARMS   │  │   等    │       │ Search │  │  Search  │
└────────┘  └─────────┘       └────────┘  └──────────┘
```

## 场景一：OpsAgent 调用外部 Skills

### 使用联网搜索增强 RCA 分析

当 OpsAgent 分析故障时，可以联网搜索相关知识：

```go
// Agent 在执行过程中调用联网搜索
result, err := mcpClient.CallTool(ctx, "exa", "web_search_exa", map[string]interface{}{
    "query": "PostgreSQL connection pool exhaustion best practices",
    "numResults": 5,
})

// 结合搜索结果给出更精准的建议
enhancedAdvice := combineWithSearchResult(analysis, result)
```

### 查找代码示例

遇到问题时可以搜索相关代码示例：

```go
codeExamples, err := mcpClient.CallTool(ctx, "exa", "get_code_context_exa", map[string]interface{}{
    "query": "Go HTTP client timeout handling patterns",
    "tokensNum": 3000,
})
```

## 场景二：外部调用 OpsAgent 能力

### 在任何项目中使用 OpsAgent

配置 OpsAgent MCP 服务器后，可以在任何项目中调用：

```json
// ~/.claude.json
{
  "opsagent": {
    "command": "go",
    "args": ["run", "f:/OpsAgent/cmd/mcp/main.go", "--port", "8081"]
  }
}
```

### 可用工具

| 工具名 | 功能 |
|-------|------|
| `opsagent_rca_analyze` | 根因分析 |
| `opsagent_log_search` | 日志搜索 |
| `opsagent_metrics_query` | 指标查询 |
| `opsagent_topology_get` | 服务拓扑 |
| `opsagent_changes_list` | 变更列表 |

## 配置步骤

### 1. 配置外部 MCP 工具

在 `config/settings.yaml` 添加：

```yaml
mcp:
  clients:
    exa:
      base_url: "http://localhost:3000"
```

### 2. 配置 OpsAgent MCP 服务器

在 `~/.claude.json` 添加：

```json
{
  "opsagent": {
    "command": "go",
    "args": ["run", "f:/OpsAgent/cmd/mcp/main.go", "--port", "8081"],
    "env": {
      "LLM_PROVIDER": "dashscope",
      "LLM_API_KEY": "your-key"
    }
  }
}
```

### 3. 启动和测试

```bash
# 启动 OpsAgent MCP 服务器
go run cmd/mcp/main.go --port 8081

# 测试工具列表
curl http://localhost:8081/mcp/tools

# 测试工具调用
curl -X POST http://localhost:8081/mcp/call \
  -H "Content-Type: application/json" \
  -d '{"name":"opsagent_topology_get","arguments":{}}'
```

## 代码集成

### 在 Agent 中使用外部 MCP 工具

```go
// 创建 MCP 客户端
multiClient := mcp.NewMultiClient()
multiClient.Register("exa", mcp.NewClient("http://localhost:3000"))

// 创建桥接器
bridge := tool.NewMCPBridge(multiClient)
bridge.SetupExternalTools()

// 注册到工具系统
for _, t := range bridge.ListTools() {
    toolRegistry.Register(t)
}

// Agent 可以调用这些工具
searchResult, err := toolRegistry.Call(ctx, "exa_web_search_exa", map[string]interface{}{
    "query": "Go patterns 2024",
})
```

## 实际使用示例

### 示例 1：故障分析 + 知识搜索

```
用户: order-service 数据库连接池耗尽

OpsAgent 执行:
1. 查询日志: 发现大量 "connection pool exhausted"
2. 查询指标: postgres-order 连接数 200/200
3. 调用联网搜索: "PostgreSQL 连接池最佳实践"
4. 结合搜索结果建议:
   - 增加 max_connections
   - 启用连接池监控
   - 检查是否有连接泄漏
```

### 示例 2：代码问题 + 示例搜索

```
用户: Go HTTP 客户端如何优化性能？

OpsAgent 执行:
1. 调用代码搜索: "Go HTTP client optimization"
2. 返回 GitHub 示例代码
3. 结合 StackOverflow 讨论
4. 给出具体优化建议
```

## 相关文件

- MCP 服务器: [internal/mcp/server.go](internal/mcp/server.go)
- MCP 客户端: [internal/mcp/client.go](internal/mcp/client.go)
- 工具桥接: [internal/tool/mcp_tools.go](internal/tool/mcp_tools.go)
- MCP 主程序: [cmd/mcp/main.go](cmd/mcp/main.go)
- 配置说明: [docs/mcp-integration.md](docs/mcp-integration.md)

## 注意事项

1. **网络要求**: 外部 MCP 工具需要网络连接
2. **API 限制**: Exa 等 API 有调用限制和费用
3. **错误处理**: 外部工具可能不可用，需要降级策略
4. **性能考虑**: MCP 调用有延迟，考虑缓存结果

## 下一步

- [ ] 实现智能缓存减少重复调用
- [ ] 添加更多外部 MCP 工具集成
- [ ] 实现工具调用权限控制
- [ ] 添加调用成本统计和限制
