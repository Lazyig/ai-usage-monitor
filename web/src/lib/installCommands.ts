// Shared with Devices (token issuance) and Downloads (generic reference) —
// keeps the two install one-liners in exactly one place.
export interface InstallCommands {
  unix: string
  windows: string
}

export function installCommands(origin: string, token: string): InstallCommands {
  return {
    unix: `curl -fsSL ${origin}/downloads/install.sh | sh -s -- --server ${origin} --token ${token}`,
    windows: `powershell -ExecutionPolicy Bypass -c "& ([scriptblock]::Create((irm ${origin}/downloads/install.ps1))) -Server ${origin} -Token ${token}"`,
  }
}
