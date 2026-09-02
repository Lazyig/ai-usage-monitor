# ai-usage-agent uninstaller for Windows (run in an elevated PowerShell).
#   powershell -ExecutionPolicy Bypass -c "& ([scriptblock]::Create((irm https://SERVER/downloads/uninstall.ps1)))"
#   append  -KeepState  to keep config/cursors/queue (%APPDATA%\ai-usage-agent)
# Removing the device on the server is a separate step: 기기 관리 → 토큰 폐기 or 완전 삭제.
param([switch]$KeepState, [string]$Prefix = "$env:LOCALAPPDATA\ai-usage-agent")
$ErrorActionPreference = "Continue"
$exe = Join-Path $Prefix "ai-usage-agent.exe"
$svc = Get-Service -Name ai-usage-agent -ErrorAction SilentlyContinue
if ($svc -and $svc.Status -ne 'Stopped') {
  Write-Host "stopping ai-usage-agent service"
  Stop-Service -Name ai-usage-agent -Force -ErrorAction SilentlyContinue
  try { $svc.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30)) } catch {}
}
if (Test-Path $exe) { & $exe uninstall 2>$null | Out-Null }
if (Get-Service -Name ai-usage-agent -ErrorAction SilentlyContinue) { sc.exe delete ai-usage-agent | Out-Null }
Get-Process -Name ai-usage-agent -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1
if (Test-Path $Prefix) { Remove-Item -Recurse -Force $Prefix -ErrorAction SilentlyContinue; Write-Host "removed $Prefix" }
$state = Join-Path $env:APPDATA "ai-usage-agent"
if (-not $KeepState -and (Test-Path $state)) { Remove-Item -Recurse -Force $state -ErrorAction SilentlyContinue; Write-Host "removed $state" }
elseif ($KeepState) { Write-Host "kept $state" }
Write-Host "done. To stop the server from accepting this device's token, revoke or delete it in the dashboard (기기 관리)."
