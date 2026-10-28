import { describe, expect, it } from 'vitest'
import { agentVersionStatus, isComparableVersion, normalizeVersion } from './agentVersion'

describe('agentVersionStatus', () => {
  it('treats a leading v as the same build', () => {
    expect(normalizeVersion(' v0.2.1 ')).toBe('0.2.1')
    expect(agentVersionStatus('v0.2.1', '0.2.1')).toBe('current')
  })

  it('flags any other version, newer or older', () => {
    expect(agentVersionStatus('0.2.0', '0.2.1')).toBe('mismatch')
    expect(agentVersionStatus('0.3.0', '0.2.1')).toBe('mismatch')
    expect(agentVersionStatus('0.0.0-1a2b3c4d', '0.0.0-5e6f7a8b')).toBe('mismatch')
  })

  it('has no verdict without a published release', () => {
    expect(isComparableVersion('dev')).toBe(false)
    expect(agentVersionStatus('0.2.1', 'dev')).toBeNull()
    expect(agentVersionStatus('0.2.1', '')).toBeNull()
  })

  it('reports a device that never sent its version as unknown', () => {
    expect(agentVersionStatus('', '0.2.1')).toBe('unknown')
  })
})
