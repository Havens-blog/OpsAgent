# Windows 本地开发环境运行指南

## 前置要求

### 1. 安装 Go

```powershell
# 下载并安装 Go 1.21+
# 访问: https://go.dev/dl/
# 或使用 winget
winget install GoLang.Go
```

验证安装：
```powershell
go version
# 应输出: go version go1.21.x windows/amd64
```

### 2. 配置环境变量

```powershell
# 设置 GOPATH（如果尚未设置）
[System.Environment]::SetEnvironmentVariable("GOPATH", "$env:USERPROFILE\go", "User")

# 设置 PATH
$goPath = "$env:USERPROFILE\go"
$currentPath = [System.Environment]::GetEnvironmentVariable("Path", "User")
[System.Environment]::SetEnvironmentVariable("Path", "$currentPath;$goPath\bin", "User")

# 重新加载环境变量
$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
```

## 运行方式

### 方式 1: 直接运行 (推荐开发环境)

```powershell
# 进入项目目录
cd f:\OpsAgent

# 设置 Anthropic API Key
$env:ANTHROPIC_API_KEY = "your-api-key-here"

# 运行 CLI
go run cmd/main.go

# 或运行特定命令
go run cmd/main.go version
go run cmd/main.go status
go run cmd/main.go help
```

### 方式 2: 编译后运行

```powershell
# 进入项目目录
cd f:\OpsAgent

# 编译
go build -o opsagent.exe cmd/main.go

# 设置环境变量
$env:ANTHROPIC_API_KEY = "your-api-key-here"

# 运行
.\opsagent.exe version
.\opsagent.exe status
.\opsagent.exe help
```

### 方式 3: 安装为全局命令

```powershell
# 进入项目目录
cd f:\OpsAgent

# 安装到 GOPATH
go install cmd/main.go

# 运行（opsagent.exe 会在 %USERPROFILE%\go\bin\ 中）
$env:ANTHROPIC_API_KEY = "your-api-key-here"
opsagent version
```

## 可用命令

```powershell
# 显示版本
opsagent version

# 显示状态
opsagent status

# 显示配置
opsagent config show

# RCA 分析（示例）
opsagent analyze `
  --incident-id=INC-001 `
  --start-time=2024-01-01T00:00:00Z `
  --end-time=2024-01-01T01:00:00Z `
  --severity=P1 `
  --services=order-service

# 查询日志（示例）
opsagent logs `
  --query="level:error" `
  --start-time=2024-01-01T00:00:00Z `
  --end-time=2024-01-01T01:00:00Z

# 查询指标（示例）
opsagent metrics `
  --query="http_requests_total" `
  --start-time=2024-01-01T00:00:00Z `
  --end-time=2024-01-01T01:00:00Z

# 显示帮助
opsagent help
```

## 运行测试

```powershell
# 运行所有测试
go test ./...

# 运行特定包的测试
go test ./internal/agent/...
go test ./internal/guardrails/...

# 运行测试并显示覆盖率
go test -cover ./...

# 生成覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## 运行 REST API 服务

创建 `cmd/api/main.go`：

```powershell
# 确保 API 入口文件存在
# 然后运行
go run cmd/api/main.go
```

或使用现有的 CLI：

```powershell
# 如果 CLI 支持 serve 命令
opsagent serve
```

## 环境变量配置

创建 `.env` 文件（在项目根目录）：

```env
# API Key
ANTHROPIC_API_KEY=sk-ant-xxxxx

# 环境
OPSAGENT_ENVIRONMENT=development
OPSAGENT_HOST=0.0.0.0
OPSAGENT_PORT=8080

# 日志
OPSAGENT_LOG_LEVEL=debug
OPSAGENT_LOG_FORMAT=text

# Agent 配置
OPSAGENT_MODEL=claude-sonnet-4-20250514
OPSAGENT_MAX_TURNS=10
OPSAGENT_AGENT_TIMEOUT=300
```

加载环境变量：

```powershell
# PowerShell 5.1+
Get-Content .env | ForEach-Object {
    if ($_ -match '^([^=]+)=(.*)$') {
        [System.Environment]::SetEnvironmentVariable($matches[1], $matches[2], "Process")
    }
}

# 验证
$env:ANTHROPIC_API_KEY
```

## 使用 Docker Desktop (可选)

```powershell
# 构建
docker build -t opsagent:latest .

# 运行
docker run --rm -p 8080:8080 -p 9090:9090 `
  -e ANTHROPIC_API_KEY=your-key `
  opsagent:latest

# 或使用 docker-compose
docker-compose up -d
```

## 常见问题

### 1. "go: command not found"

确保 Go 已安装并在 PATH 中：
```powershell
where.exe go
```

### 2. API Key 未设置错误

确保设置了环境变量：
```powershell
$env:ANTHROPIC_API_KEY = "your-key"
```

### 3. 端口被占用

更改端口：
```powershell
$env:OPSAGENT_PORT = "8081"
```

### 4. 模块依赖问题

```powershell
# 清理并重新下载
go clean -modcache
go mod download
```

## VS Code 配置

创建 `.vscode/launch.json`：

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Run OpsAgent",
      "type": "go",
      "request": "launch",
      "mode": "auto",
      "program": "${workspaceFolder}/cmd/main.go",
      "env": {
        "ANTHROPIC_API_KEY": "your-api-key-here"
      },
      "args": []
    },
    {
      "name": "Run Tests",
      "type": "go",
      "request": "launch",
      "mode": "test",
      "program": "${workspaceFolder}",
      "env": {
        "ANTHROPIC_API_KEY": "your-api-key-here"
      }
    }
  ]
}
```

创建 `.vscode/settings.json`：

```json
{
  "go.toolsManagement.autoUpdate": true,
  "go.lintTool": "golangci-lint",
  "go.lintOnSave": "package",
  "go.formatTool": "goimports",
  "terminal.integrated.defaultProfile.windows": "PowerShell"
}
```

## 快速启动脚本

创建 `run.ps1`：

```powershell
# 设置 API Key
$env:ANTHROPIC_API_KEY = "your-api-key-here"
$env:OPSAGENT_LOG_LEVEL = "debug"

# 显示版本
Write-Host "=== OpsAgent ===" -ForegroundColor Green
go run cmd/main.go version

# 运行命令
if ($args.Count -gt 0) {
    go run cmd/main.go @args
} else {
    go run cmd/main.go help
}
```

使用：
```powershell
.\run.ps1 version
.\run.ps1 status
```
