<#
.SYNOPSIS
  Stop the OpsAgent backend (:8080) and web console (:3100).

.DESCRIPTION
  Safety first: this never blindly kills whatever owns a port. For each port it
  resolves the owning process and stops it ONLY after verifying the process
  identity looks like ours (opsagent-api.exe for the API, a node/next process
  for the web console). Anything else on the port is reported and left alone,
  so an unrelated service that happens to squat the port is never killed.
#>
[CmdletBinding()]
param(
  [int]$ApiPort = 8080,
  [int]$WebPort = 3100
)

$ErrorActionPreference = "Stop"

function Get-PortOwner([int]$Port) {
  $conn = Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue |
    Select-Object -First 1
  if (-not $conn) { return $null }
  return Get-Process -Id $conn.OwningProcess -ErrorAction SilentlyContinue
}

# Stop the process on $Port only if $IdentityOk returns $true for it.
function Stop-IfOurs([int]$Port, [string]$Label, [scriptblock]$IdentityOk) {
  $proc = Get-PortOwner $Port
  if (-not $proc) {
    Write-Host "[$Label] nothing listening on :$Port"
    return
  }
  $name = $proc.ProcessName
  $path = try { $proc.Path } catch { "<unknown>" }
  if (& $IdentityOk $proc) {
    Write-Host "[$Label] stopping PID $($proc.Id) ($name) on :$Port"
    Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
  }
  else {
    Write-Warning "[$Label] :$Port is owned by PID $($proc.Id) ($name — $path) which does NOT look like OpsAgent; leaving it alone"
  }
}

# API: the owner must be our opsagent-api.exe binary.
Stop-IfOurs -Port $ApiPort -Label "api" -IdentityOk {
  param($p)
  ($p.Path -and ($p.Path -like "*opsagent-api.exe")) -or ($p.ProcessName -eq "opsagent-api")
}

# Web: the owner must be a node process (that is how `next dev` runs). We can't
# distinguish our next instance from another node by name alone, so we also
# require the command line to reference next/this repo before stopping.
Stop-IfOurs -Port $WebPort -Label "web" -IdentityOk {
  param($p)
  if ($p.ProcessName -ne "node") { return $false }
  $cmd = try {
    (Get-CimInstance Win32_Process -Filter "ProcessId=$($p.Id)" -ErrorAction SilentlyContinue).CommandLine
  }
  catch { $null }
  return $cmd -and ($cmd -match "next" -or $cmd -match "OpsAgent")
}

Write-Host "OpsAgent stop complete."
