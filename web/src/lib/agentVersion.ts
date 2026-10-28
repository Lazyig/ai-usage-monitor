// Compares each device's agent with the build the server hands out, so the
// device list can point at agents that need the update command.

export type AgentVersionStatus = 'current' | 'mismatch' | 'unknown'

/** "v0.2.1" and "0.2.1" are the same build. */
export function normalizeVersion(v: string | undefined | null): string {
  return (v ?? '').trim().replace(/^v(?=\d)/i, '')
}

/** A published version the devices can be compared with: local builds are
 * all "dev" and an empty one means no manifest was published. */
export function isComparableVersion(v: string | undefined | null): boolean {
  const n = normalizeVersion(v)
  return n !== '' && n !== 'dev'
}

/** How a device's agent relates to the published one. Any difference counts
 * as a mismatch, newer included: the published build is what an update
 * installs, and version strings (tags, 0.0.0-<sha>) do not order reliably.
 * null when there is nothing to compare against. */
export function agentVersionStatus(agent: string | undefined | null, published: string | undefined | null): AgentVersionStatus | null {
  if (!isComparableVersion(published)) return null
  const a = normalizeVersion(agent)
  if (a === '') return 'unknown'
  return a === normalizeVersion(published) ? 'current' : 'mismatch'
}
