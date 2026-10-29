// Shared with Devices (token issuance) and Downloads (generic reference) —
// keeps the two install one-liners in exactly one place.
export interface InstallCommands {
  unix: string
  windows: string
}

/** True when the dashboard is served over plain http from another machine:
 * the installers refuse such a server unless told to allow it, because the
 * binary and the device token would cross the network unencrypted. */
export function isInsecureOrigin(origin: string): boolean {
  try {
    const u = new URL(origin)
    return u.protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(u.hostname)
  } catch {
    return false
  }
}

function insecureFlags(origin: string): { unix: string; windows: string } {
  return isInsecureOrigin(origin) ? { unix: ' --allow-http', windows: ' -AllowHttp' } : { unix: '', windows: '' }
}

export function installCommands(origin: string, token: string): InstallCommands {
  const f = insecureFlags(origin)
  return {
    unix: `curl -fsSL ${origin}/downloads/install.sh | sh -s -- --server ${origin} --token ${token}${f.unix}`,
    windows: `powershell -ExecutionPolicy Bypass -c "& ([scriptblock]::Create((irm ${origin}/downloads/install.ps1))) -Server ${origin} -Token ${token}${f.windows}"`,
  }
}

/** Update one-liners: the installer replaces the binary and re-registers the
 * service with the server, token and folders already in config.json, so no
 * token is needed. Run as the account that installed the agent. */
export function updateCommands(origin: string): InstallCommands {
  const f = insecureFlags(origin)
  return {
    unix: `curl -fsSL ${origin}/downloads/install.sh | sh -s -- --server ${origin} --update${f.unix}`,
    windows: `powershell -ExecutionPolicy Bypass -c "& ([scriptblock]::Create((irm ${origin}/downloads/install.ps1))) -Server ${origin} -Update${f.windows}"`,
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
