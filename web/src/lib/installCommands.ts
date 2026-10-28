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

/** Update one-liners: the installer replaces the binary and re-registers the
 * service with the server, token and folders already in config.json, so no
 * token is needed. Run as the account that installed the agent. */
export function updateCommands(origin: string): InstallCommands {
  return {
    unix: `curl -fsSL ${origin}/downloads/install.sh | sh -s -- --server ${origin} --update`,
    windows: `powershell -ExecutionPolicy Bypass -c "& ([scriptblock]::Create((irm ${origin}/downloads/install.ps1))) -Server ${origin} -Update"`,
  }
}

/** Removal one-liners shown on the Downloads page. `--keep-state` / `-KeepState`
 * leaves config.json, cursors.json and the offline queue in place. */
export function uninstallCommands(origin: string): InstallCommands {
  return {
    unix: `curl -fsSL ${origin}/downloads/uninstall.sh | sh`,
    windows: `powershell -ExecutionPolicy Bypass -c "& ([scriptblock]::Create((irm ${origin}/downloads/uninstall.ps1)))"`,
  }
}
