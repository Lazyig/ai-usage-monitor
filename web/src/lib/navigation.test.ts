import { describe, expect, it } from 'vitest'
import { safeReturnPath } from './navigation'

describe('safeReturnPath', () => {
  it('keeps in-app paths and drops anything that could leave the origin', () => {
    expect(safeReturnPath('/events')).toBe('/events')
    expect(safeReturnPath(undefined)).toBe('/')
    expect(safeReturnPath('//evil.example')).toBe('/')
    expect(safeReturnPath('/\\evil.example')).toBe('/')
    expect(safeReturnPath('https://evil.example')).toBe('/')
  })
})
