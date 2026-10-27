<#
.SYNOPSIS
  Make OpsAgent survive session end and start automatically at logon, WITHOUT
  requiring administrator rights.

.DESCRIPTION
  Why a scheduled task (not a Windows service):
    - A Windows service requires administrator rights to install and the Go
      binary / Next.js dev server do not implement the service-control protocol.
    - A per-user logon scheduled task needs no admin rights, and the Task
      Scheduler launches the action in ITS OWN context — detached from whatever
      shell registered it. That is exactly what we need: the started services no
      longer belong to the calling shell's job object, so ending that session
      does not kill them. The same mechanism also provides auto-start at logon.

  The task runs start-opsagent.ps1, which is idempotent (it skips a service
  already listening), so the task firing again at the next logon is harmless.

  Remove with:  Unregister-ScheduledTask -TaskName 'OpsAgent' -Confirm:$false
#>
[CmdletBinding()]
param(
  [string]$TaskName = "OpsAgent",
  [string]$Root
)

$ErrorActionPreference = "Stop"

# Resolve script dir robustly: $PSScriptRoot can be empty in a param() default
# under `powershell -File`, so fall back to $PSCommandPath's directory.
$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $PSCommandPath }
if (-not $Root) { $Root = (Resolve-Path (Join-Path $scriptDir "..\..")).Path }

$startScript = Join-Path $scriptDir "start-opsagent.ps1"
if (-not (Test-Path $startScript)) { throw "start script not found: $startScript" }

$psExe = (Get-Command powershell.exe).Source
$user  = "$env:USERDOMAIN\$env:USERNAME"

Write-Host "Registering logon task '$TaskName' for $user"
Write-Host "  action: $psExe -File $startScript"

$action = New-ScheduledTaskAction -Execute $psExe `
  -Argument "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$startScript`"" `
  -WorkingDirectory $Root

$trigger = New-ScheduledTaskTrigger -AtLogOn -User $user

# Run only when the user is logged on (Interactive): no stored password, no admin.
$principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited

# Keep it alive and resilient: no execution time limit, allow on battery, and if
# the launcher ever overlaps just ignore the new instance (services are
# port-guarded anyway).
$settings = New-ScheduledTaskSettingsSet `
  -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
  -ExecutionTimeLimit ([TimeSpan]::Zero) `
  -MultipleInstances IgnoreNew `
  -StartWhenAvailable

# Replace any prior registration so re-running is idempotent.
if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
  Write-Host "  existing task found — replacing"
  Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}

Register-ScheduledTask -TaskName $TaskName `
  -Action $action -Trigger $trigger -Principal $principal -Settings $settings `
  -Description "Start OpsAgent backend (:8080) and web console (:3100) at logon." | Out-Null

Write-Host "Registered. Start it now with:  Start-ScheduledTask -TaskName '$TaskName'"
