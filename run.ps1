# OpsAgent 快速启动脚本
# 使用方法: .\run.ps1 [命令] [参数...]

# 设置环境变量
$env:ANTHROPIC_API_KEY = $env:ANTHROPIC_API_KEY  # 从系统环境变量读取
$env:OPSAGENT_LOG_LEVEL = "debug"
$env:OPSAGENT_LOG_FORMAT = "text"

# 显示 banner
Write-Host ""
Write-Host "╔══════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║         OpsAgent - 运维智能体系统          ║" -ForegroundColor Cyan
Write-Host "╚══════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""

# 检查 Go 安装
$goVersion = go version 2>$null
if ($LASTEXITCODE -ne 0) {
    Write-Host "❌ 错误: 未找到 Go，请先安装 Go" -ForegroundColor Red
    Write-Host "访问: https://go.dev/dl/" -ForegroundColor Yellow
    exit 1
}

Write-Host "📌 Go 版本: $goVersion" -ForegroundColor Green
Write-Host ""

# 特殊命令处理
if ($args.Count -gt 0) {
    $command = $args[0]
    
    switch ($command) {
        "llm-test" {
            Write-Host "▶️  测试 LLM 连接..." -ForegroundColor Cyan
            Write-Host ""
            go run cmd/llm_test/main.go
            if ($LASTEXITCODE -ne 0) {
                Write-Host ""
                Write-Host "❌ LLM 测试失败" -ForegroundColor Red
                exit $LASTEXITCODE
            }
            exit 0
        }
        default {
            # 其他命令传递给 main.go
            Write-Host "▶️  执行: go run cmd/main.go $args" -ForegroundColor Cyan
            Write-Host ""
            go run cmd/main.go @args
            if ($LASTEXITCODE -ne 0) {
                Write-Host ""
                Write-Host "❌ 命令执行失败 (退出码: $LASTEXITCODE)" -ForegroundColor Red
                exit $LASTEXITCODE
            }
            exit 0
        }
    }
}

# 如果没有参数，显示帮助
Write-Host "可用命令:" -ForegroundColor Cyan
Write-Host "  .\run.ps1 llm-test      - 测试 LLM 连接" -ForegroundColor White
Write-Host "  .\run.ps1 version      - 显示版本信息" -ForegroundColor White
Write-Host "  .\run.ps1 status       - 显示系统状态" -ForegroundColor White
Write-Host "  .\run.ps1 config show  - 显示配置" -ForegroundColor White
Write-Host "  .\run.ps1 help         - 显示帮助" -ForegroundColor White
Write-Host ""
Write-Host "示例:" -ForegroundColor Cyan
Write-Host "  .\run.ps1 llm-test" -ForegroundColor White
Write-Host "  .\run.ps1 version" -ForegroundColor White
Write-Host ""
exit 0
