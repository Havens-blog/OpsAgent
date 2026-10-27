<#
.SYNOPSIS
  Start the OpsAgent backend (:8080) as a detached, session-independent
  process. The web console (:3100) is managed separately (see below).

.DESCRIPTION
  Idempotent: a service already listening on its port is left running. Each
  service is launched with Start-Process, so the processes are independent of
  whatever shell (or scheduled task) invoked this script and survive after it
  exits. The backend is started with its working directory set to the repo root
  so it loads the repo .env itself (the Go server reads .env on startup), which
  means credentials never have to be parsed/injected by this script.

  Frontend ownership: the :3100 Next.js console is now managed by the Haven
  platform script D:\Haven\dev.ps1 (service "opsagent-web"), so this script
  does NOT start it by default — that would make two managers fight over the
  port. Pass -StartWeb to start the frontend here too (legacy standalone mode).

  Logs are written to <root>\logs\opsagent-api.* and \opsagent-web.*.
#>
[CmdletBinding()]
param(
  # Repo root. Resolved in the body when not supplied (see below): $PSScriptRoot
  # can be empty inside a param() default when the script is invoked via
  # `powershell -File`, which silently broke the scheduled-task launch.
  [string]$Root,
  [int]$ApiPort = 8080,
  [int]$WebPort = 3100,
  # Start the :3100 frontend here too. OFF by default: the frontend is normally
  # managed by D:\Haven\dev.ps1 (service "opsagent-web"). Enable only for legacy
  # standalone operation where dev.ps1 is not in use.
  [switch]$StartWeb,
  # Force a fresh `next build` even if a prior production build exists. Use this
  # after changing frontend code (production mode has no hot reload).
  [switch]$Rebuild,
  # Node executable used to run `next`. Defaults to the DSH runtime node that is
  # known to work in this environment; override for a different host.
  [string]$NodeExe = "C:\Users\chengyinghao\.dsh\dsh-runtimes\dsh-primary-runtime\dependencies\node\bin\node.exe"
)

$ErrorActionPreference = "Stop"

# Resolve the repo root robustly. Prefer $PSScriptRoot, but fall back to the
# directory of $PSCommandPath (reliably set under `-File`) so this works no
# matter how the script is launched (shell, scheduled task, double-click).
if (-not $Root) {
  $scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $PSCommandPath }
  $Root = (Resolve-Path (Join-Path $scriptDir "..\..")).Path
}

function Test-PortBusy([int]$Port) {
  $null -ne (Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue |
    Select-Object -First 1)
}

$logDir = Join-Path $Root "logs"
if (-not (Test-Path $logDir)) { New-Item -ItemType Directory -Path $logDir -Force | Out-Null }

# ---- Backend ----------------------------------------------------------------
$apiExe = Join-Path $Root "build\opsagent-api.exe"
if (Test-PortBusy $ApiPort) {
  Write-Host "[api] already listening on :$ApiPort — skipping"
}
else {
  if (-not (Test-Path $apiExe)) {
    Write-Host "[api] building $apiExe ..."
    Push-Location $Root
    try { & go build -o $apiExe .\cmd\api; if ($LASTEXITCODE -ne 0) { throw "go build failed ($LASTEXITCODE)" } }
    finally { Pop-Location }
  }
  Write-Host "[api] starting (cwd=$Root so it loads .env) ..."
  $api = Start-Process -FilePath $apiExe -WorkingDirectory $Root `
    -RedirectStandardOutput (Join-Path $logDir "opsagent-api.out.log") `
    -RedirectStandardError  (Join-Path $logDir "opsagent-api.err.log") `
    -WindowStyle Hidden -PassThru
  Write-Host "[api] PID $($api.Id) on :$ApiPort"
}

# ---- Frontend ---------------------------------------------------------------
$webDir  = Join-Path $Root "web\frontend"
$nextBin = Join-Path $webDir "node_modules\next\dist\bin\next"
if (-not $StartWeb) {
  Write-Host "[web] skipped — frontend is managed by D:\Haven\dev.ps1 (opsagent-web). Pass -StartWeb to start it here."
}
elseif (Test-PortBusy $WebPort) {
  Write-Host "[web] already listening on :$WebPort — skipping"
}
elseif (-not (Test-Path $NodeExe)) {
  Write-Warning "[web] node not found at $NodeExe — skipping web console (set -NodeExe)"
}
else {
  # Next.js only loads env files under web\frontend, NOT the repo-root .env.
  # The server-side proxy (src/app/api/v1/[...path]/route.ts) needs
  # API_AUTH_TOKEN to forward authenticated requests, so inject it from the
  # root .env into THIS process's environment; the node child inherits it.
  $rootEnv = Join-Path $Root ".env"
  if (Test-Path $rootEnv) {
    foreach ($line in (Get-Content $rootEnv -Encoding UTF8)) {
      if ($line -match '^\s*API_AUTH_TOKEN\s*=\s*(.+?)\s*$') {
        $env:API_AUTH_TOKEN = $Matches[1].Trim('"').Trim("'")
      }
    }
  }
  if ($env:API_AUTH_TOKEN) {
    Write-Host "[web] API_AUTH_TOKEN injected into web env (value not echoed)"
  } else {
    Write-Warning "[web] API_AUTH_TOKEN not found in .env — proxy will forward without a token (backend must also have auth disabled)"
  }

  # Production mode: serve a prebuilt optimized bundle via `next start` instead
  # of `next dev`. This removes dev's slow first-compile (seconds per route) and
  # hot-reload overhead — the right tradeoff for a long-running console. The
  # cost is no hot reload: after changing frontend code, run `next build` again
  # (or pass -Rebuild) to regenerate .next.
  $buildId = Join-Path $webDir ".next\BUILD_ID"
  if ($Rebuild -or -not (Test-Path $buildId)) {
    Write-Host "[web] building production bundle (next build) ..."
    Push-Location $webDir
    try { & $NodeExe $nextBin build; if ($LASTEXITCODE -ne 0) { throw "next build failed ($LASTEXITCODE)" } }
    finally { Pop-Location }
  } else {
    Write-Host "[web] reusing existing production build (pass -Rebuild to force rebuild)"
  }

  Write-Host "[web] starting next start on :$WebPort ..."
  $web = Start-Process -FilePath $NodeExe `
    -ArgumentList @($nextBin, "start", "-p", "$WebPort", "-H", "127.0.0.1") `
    -WorkingDirectory $webDir `
    -RedirectStandardOutput (Join-Path $logDir "opsagent-web.out.log") `
    -RedirectStandardError  (Join-Path $logDir "opsagent-web.err.log") `
    -WindowStyle Hidden -PassThru
  Write-Host "[web] PID $($web.Id) on :$WebPort"
}

Write-Host "OpsAgent start complete. API=http://127.0.0.1:$ApiPort  Web=http://127.0.0.1:$WebPort"
