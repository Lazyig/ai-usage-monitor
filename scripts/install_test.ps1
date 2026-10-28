# No service installation or external downloads: all platform operations are mocked.
$ErrorActionPreference = 'Stop'
$global:ReviewInstallerFixture = [System.Text.Encoding]::UTF8.GetBytes('review-agent-binary')
$sha = [System.Security.Cryptography.SHA256]::Create()
$expected = [BitConverter]::ToString($sha.ComputeHash($global:ReviewInstallerFixture)).Replace('-', '').ToLower()
$sha.Dispose()
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'amd64' }
$name = "ai-usage-agent_windows_$arch.exe"
function Invoke-WebRequest { param($Uri, $OutFile, [switch]$UseBasicParsing) [IO.File]::WriteAllBytes($OutFile, $global:ReviewInstallerFixture) }
function Invoke-RestMethod { param($Uri, [switch]$UseBasicParsing) return @{ files = $global:ReviewInstallerEntries } }
function Get-Service { param($Name, $ErrorAction) return $null }
function Get-Process { param($Name, $ErrorAction) return $null }

foreach ($kind in @('valid', 'missing', 'short', 'mismatch', 'duplicate')) {
  $dir = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
  New-Item -ItemType Directory -Path $dir | Out-Null
  try {
    $dest = Join-Path $dir 'ai-usage-agent.exe'
    [IO.File]::WriteAllText($dest, 'previous-agent')
    $value = switch ($kind) { 'missing' { '' } 'short' { 'deadbeef' } 'mismatch' { 'a' * 64 } default { $expected.ToUpper() } }
    $global:ReviewInstallerEntries = @(@{ name = $name; sha256 = $value })
    if ($kind -eq 'duplicate') { $global:ReviewInstallerEntries += @{ name = $name; sha256 = $value } }
    $failed = $false
    $failure = ''
    try { & "$PSScriptRoot/install.ps1" -Server 'https://example.test' -Prefix $dir } catch { $failed = $true; $failure = $_.Exception.Message }
    if ($kind -eq 'valid') {
      if ($failed -or [BitConverter]::ToString([IO.File]::ReadAllBytes($dest)) -ne [BitConverter]::ToString($global:ReviewInstallerFixture)) { throw "valid install failed: $failure" }
    } elseif (-not $failed -or [IO.File]::ReadAllText($dest) -ne 'previous-agent' -or (Test-Path "$dest.new")) {
      throw "invalid $kind manifest did not preserve the previous agent"
    }
    Write-Host "PASS PowerShell installer: $kind"
  } finally { Remove-Item -Recurse -Force $dir }
}

# -Update with no agent at the prefix (wrong account or -Prefix) stops before downloading anything.
$dir = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $dir | Out-Null
try {
  $global:ReviewInstallerEntries = @(@{ name = $name; sha256 = $expected })
  $failure = ''
  try { & "$PSScriptRoot/install.ps1" -Server 'https://example.test' -Prefix $dir -Update } catch { $failure = $_.Exception.Message }
  if ($failure -notlike '*no agent at*') { throw "-Update without an installed agent did not refuse: '$failure'" }
  if (Test-Path (Join-Path $dir 'ai-usage-agent.exe.new')) { throw "-Update downloaded before refusing" }
  Write-Host "PASS PowerShell installer: update needs an installed agent"
} finally { Remove-Item -Recurse -Force $dir }
