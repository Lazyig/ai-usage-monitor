import { describe, expect, it } from 'vitest'
import { installCommands, isInsecureOrigin, updateCommands } from './installCommands'

describe('install commands over plain http', () => {
  it('adds the allow flag only for http from another machine', () => {
    expect(isInsecureOrigin('https://usage.example.com')).toBe(false)
    expect(isInsecureOrigin('http://localhost:8080')).toBe(false)
    expect(isInsecureOrigin('http://127.0.0.1:18081')).toBe(false)
    expect(isInsecureOrigin('http://[::1]:8080')).toBe(false)
    expect(isInsecureOrigin('http://192.168.1.10:8080')).toBe(true)
    expect(installCommands('http://192.168.1.10', 'aum_x').unix).toMatch(/--allow-http$/)
    expect(updateCommands('http://192.168.1.10').windows).toMatch(/-AllowHttp"$/)
    expect(installCommands('https://usage.example.com', 'aum_x').unix).not.toMatch(/allow-http/)
  })
})
