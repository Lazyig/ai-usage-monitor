# ai-usage-agent installer for Windows (PowerShell 5+).
#   irm https://SERVER/downloads/install.ps1 | iex   -> then run Install-AiUsageAgent -Server https://SERVER -Token aum_xxx
# or: powershell -c "& { $(irm https://SERVER/downloads/install.ps1) } -Server https://SERVER -Token aum_xxx"
param([string]$Server, [string]$Token, [string]$Prefix = "$env:LOCALAPPDATA\ai-usage-agent")
$ErrorActionPreference = "Stop"
if (-not $Server) { throw "-Server required" }
$Server = $Server.TrimEnd('/')
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'amd64' }
$name = "ai-usage-agent_windows_$arch.exe"
New-Item -ItemType Directory -Force -Path $Prefix | Out-Null
$dst = Join-Path $Prefix "ai-usage-agent.exe"
Write-Host "downloading $Server/downloads/$name"
Invoke-WebRequest -Uri "$Server/downloads/$name" -OutFile $dst -UseBasicParsing
$manifest = Invoke-RestMethod -Uri "$Server/downloads/manifest.json" -UseBasicParsing
$want = ($manifest.files | Where-Object { $_.name -eq $name }).sha256
if ($want) {
  $got = (Get-FileHash -Algorithm SHA256 $dst).Hash.ToLower()
  if ($got -ne $want) { throw "sha256 mismatch: got $got want $want" }
  Write-Host "sha256 ok"
}
Write-Host "installed $dst"
if ($Token) { & $dst install --server $Server --token $Token }
else { Write-Host "next: $dst install --server $Server --token <device token>" }
