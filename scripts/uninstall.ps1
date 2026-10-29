# ai-usage-agent uninstaller for Windows (run in an elevated PowerShell).
#   powershell -ExecutionPolicy Bypass -c "& ([scriptblock]::Create((irm https://SERVER/downloads/uninstall.ps1)))"
#   append  -KeepState  to keep config/cursors/queue (%ProgramData%\ai-usage-agent)
# Removing the device on the server is a separate step: 기기 관리 → 토큰 폐기 or 완전 삭제.
# Also cleans up the locations agents before the move to Program Files used (%LOCALAPPDATA%, %APPDATA%).
param([switch]$KeepState, [string]$Prefix = "$env:ProgramFiles\ai-usage-agent")
$ErrorActionPreference = "Continue"
$exe = Join-Path $Prefix "ai-usage-agent.exe"
$svc = Get-Service -Name ai-usage-agent -ErrorAction SilentlyContinue
if ($svc -and $svc.Status -ne 'Stopped') {
  Write-Host "stopping ai-usage-agent service"
  Stop-Service -Name ai-usage-agent -Force -ErrorAction SilentlyContinue
  try { $svc.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30)) } catch {}
}
$legacyPrefix = "$env:LOCALAPPDATA\ai-usage-agent"
foreach ($candidate in @($exe, (Join-Path $legacyPrefix "ai-usage-agent.exe"))) {
  if (Test-Path $candidate) { & $candidate uninstall 2>$null | Out-Null; break }
}
if (Get-Service -Name ai-usage-agent -ErrorAction SilentlyContinue) { sc.exe delete ai-usage-agent | Out-Null }
Get-Process -Name ai-usage-agent -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1
foreach ($dir in @($Prefix, $legacyPrefix)) {
  if (Test-Path $dir) { Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue; Write-Host "removed $dir" }
}
foreach ($state in @((Join-Path $env:ProgramData "ai-usage-agent"), (Join-Path $env:APPDATA "ai-usage-agent"))) {
  if (-not (Test-Path $state)) { continue }
  if ($KeepState) { Write-Host "kept $state"; continue }
  Remove-Item -Recurse -Force $state -ErrorAction SilentlyContinue; Write-Host "removed $state"
}
Write-Host "done. To stop the server from accepting this device's token, revoke or delete it in the dashboard (기기 관리)."
