# ai-usage-agent installer for Windows (PowerShell 5+). The script takes parameters, so run it as a
# script block (plain `irm | iex` cannot pass -Server/-Token):
#   powershell -ExecutionPolicy Bypass -c "& ([scriptblock]::Create((irm https://SERVER/downloads/install.ps1))) -Server https://SERVER -Token aum_xxx"
# or download it and run: .\install.ps1 -Server https://SERVER -Token aum_xxx
# Extra Claude Code config directories: -ClaudeDir 'C:\cfg\work','D:\claude personal'
# (default: $env:CLAUDE_CONFIG_DIR, read by the agent itself).
# Update an installed agent, keeping its config.json (server, token, folders): -Update instead of -Token,
# from an elevated PowerShell of the account that installed it.
#
# The service runs as LocalSystem, so the binary goes to Program Files and the agent keeps its state in
# %ProgramData%\ai-usage-agent (SYSTEM and Administrators only): neither may be writable by the user.
# Installs from before that (binary in %LOCALAPPDATA%, state in %APPDATA%) are moved there by any run of
# this script; the old binary folder is removed afterwards.
param([string]$Server, [string]$Token, [string]$Prefix = "$env:ProgramFiles\ai-usage-agent", [string[]]$ClaudeDir = @(), [switch]$Update, [switch]$AllowHttp)
$ErrorActionPreference = "Stop"
if (-not $Server) { throw "-Server required" }
# Plain http would carry the binary, its checksum and the device token in the clear, so anyone on the
# path could swap the agent that then runs as LocalSystem. Only this machine itself is exempt.
$serverUri = [Uri]$Server
if ($serverUri.Scheme -eq 'http') {
  if (@('localhost', '127.0.0.1', '[::1]') -notcontains $serverUri.Host.ToLower()) {
    if (-not $AllowHttp) {
      throw "refusing plain $Server: the binary and the device token would cross the network unencrypted. Use https://, or pass -AllowHttp."
    }
    Write-Warning "plain http server: the binary and the device token are not encrypted"
  }
} elseif ($serverUri.Scheme -ne 'https') {
  throw "-Server must start with https:// (or http:// for this machine)"
}
$legacyPrefix = "$env:LOCALAPPDATA\ai-usage-agent"
$legacyExe = Join-Path $legacyPrefix "ai-usage-agent.exe"
$migrate = (Test-Path $legacyExe) -and ([IO.Path]::GetFullPath($legacyPrefix) -ne [IO.Path]::GetFullPath($Prefix))
# An update replaces an existing agent; none here means the wrong account or -Prefix.
if ($Update -and -not $Token -and -not (Test-Path (Join-Path $Prefix "ai-usage-agent.exe")) -and -not $migrate) {
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
# Remembered so a run that does not re-register the service starts it again.
$wasRunning = [bool]($svc -and $svc.Status -eq 'Running')
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
if ($AllowHttp) { $agentArgs += @('--allow-http') }
foreach ($d in $ClaudeDir) { $agentArgs += @('--claude-dir', $d) }
# Moving an old install must re-register the service (it points at the old binary and state), so it
# always runs install; the agent carries config.json, cursors and the queue over to %ProgramData%.
if ($Token -or $Update -or $migrate) {
  & $dst @agentArgs
  # A native command's failure does not trip $ErrorActionPreference.
  if ($LASTEXITCODE -ne 0) { throw "ai-usage-agent install failed (exit code $LASTEXITCODE)" }
  if ($migrate) {
    Remove-Item -Recurse -Force $legacyPrefix -ErrorAction SilentlyContinue
    Write-Host "moved the agent from $legacyPrefix to $Prefix"
  }
} else {
  if ($wasRunning) {
    Start-Service -Name ai-usage-agent
    Write-Host "restarted the ai-usage-agent service with the new binary"
  }
  $hint = ($ClaudeDir | ForEach-Object { " --claude-dir '$_'" }) -join ''
  Write-Host "next: $dst install --server $Server --token <device token>$hint"
}
