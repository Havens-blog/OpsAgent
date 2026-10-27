# OpsAgent 本地启动脚本

Write-Host "启动 OpsAgent 后端服务..." -ForegroundColor Green
$apiJob = Start-Job -ScriptBlock {
    cd F:\OPSAGENT
    F:\OPSAGENT\build\opsagent-api
}

Write-Host "启动前端开发服务器..." -ForegroundColor Green  
$frontendJob = Start-Job -ScriptBlock {
    cd F:\OPSAGENT\web\frontend
    npm run dev
}

Write-Host ""
Write-Host "✅ OpsAgent 已启动！" -ForegroundColor Green
Write-Host "🌐 前端地址: http://localhost:3000" -ForegroundColor Cyan
Write-Host "🔧 后端地址: http://localhost:8080" -ForegroundColor Cyan
Write-Host ""
Write-Host "按 Ctrl+C 停止服务" -ForegroundColor Yellow

try {
    while ($true) { Start-Sleep 1 }
} finally {
    Stop-Job $apiJob
    Stop-Job $frontendJob
    Remove-Job $apiJob, $frontendJob
    Write-Host "已停止服务" -ForegroundColor Yellow
}
