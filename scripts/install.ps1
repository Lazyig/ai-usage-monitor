# ai-usage-agent installer for Windows (PowerShell 5+). The script takes parameters, so run it as a
# script block (plain `irm | iex` cannot pass -Server/-Token):
#   powershell -ExecutionPolicy Bypass -c "& ([scriptblock]::Create((irm https://SERVER/downloads/install.ps1))) -Server https://SERVER -Token aum_xxx"
# or download it and run: .\install.ps1 -Server https://SERVER -Token aum_xxx
# Extra Claude Code config directories: -ClaudeDir 'C:\cfg\work','D:\claude personal'
# (default: $env:CLAUDE_CONFIG_DIR, read by the agent itself).
# Update an installed agent, keeping its config.json (server, token, folders): -Update instead of -Token,
# from an elevated PowerShell of the account that installed it.
param([string]$Server, [string]$Token, [string]$Prefix = "$env:LOCALAPPDATA\ai-usage-agent", [string[]]$ClaudeDir = @(), [switch]$Update)
$ErrorActionPreference = "Stop"
if (-not $Server) { throw "-Server required" }
# An update replaces an existing agent in place; none here means the wrong account or -Prefix.
if ($Update -and -not $Token -and -not (Test-Path (Join-Path $Prefix "ai-usage-agent.exe"))) {
  throw "-Update: no agent at $Prefix\ai-usage-agent.exe. Run as the account that installed it, pass -Prefix, or install with -Token."
}
$Server = $Server.TrimEnd('/')
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'amd64' }
$name = "ai-usage-agent_windows_$arch.exe"
New-Item -ItemType Directory -Force -Path $Prefix | Out-Null
$dst = Join-Path $Prefix "ai-usage-agent.exe"
$tmp = "$dst.new"
# Download to a temp file first: the running service keeps $dst locked.
Write-Host "downloading $Server/downloads/$name"
Invoke-WebRequest -Uri "$Server/downloads/$name" -OutFile $tmp -UseBasicParsing
$manifest = Invoke-RestMethod -Uri "$Server/downloads/manifest.json" -UseBasicParsing
$entries = @($manifest.files | Where-Object { $_.name -eq $name })
if ($entries.Count -ne 1 -or $entries[0].sha256 -notmatch '^[a-fA-F0-9]{64}$') {
  Remove-Item -Force $tmp
  throw "manifest has no unique valid sha256 for $name"
}
$want = $entries[0].sha256.ToLower()
$got = (Get-FileHash -Algorithm SHA256 $tmp).Hash.ToLower()
if ($got -ne $want) { Remove-Item -Force $tmp; throw "sha256 mismatch: got $got want $want" }
Write-Host "sha256 ok"
# Stop a running service (and any stray process) before replacing the binary.
$svc = Get-Service -Name ai-usage-agent -ErrorAction SilentlyContinue
if ($svc -and $svc.Status -ne 'Stopped') {
  Write-Host "stopping running ai-usage-agent service"
  Stop-Service -Name ai-usage-agent -Force -ErrorAction SilentlyContinue
  $svc.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30))
}
Get-Process -Name ai-usage-agent -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
$moved = $false
for ($i = 0; $i -lt 10 -and -not $moved; $i++) {
  try { Move-Item -Force $tmp $dst; $moved = $true } catch { Start-Sleep -Seconds 1 }
}
if (-not $moved) { throw "could not replace $dst (still in use). Stop the ai-usage-agent service and retry." }
Write-Host "installed $dst"
# An update passes no token: `install` keeps the one in config.json and re-registers the service.
$agentArgs = @('install', '--server', $Server)
if ($Token) { $agentArgs += @('--token', $Token) }
foreach ($d in $ClaudeDir) { $agentArgs += @('--claude-dir', $d) }
if ($Token -or $Update) { & $dst @agentArgs }
else {
  $hint = ($ClaudeDir | ForEach-Object { " --claude-dir '$_'" }) -join ''
  Write-Host "next: $dst install --server $Server --token <device token>$hint"
}
