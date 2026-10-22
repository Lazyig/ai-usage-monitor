import { describe, expect, it } from 'vitest'
import { markUnpriced } from './client'

describe('markUnpriced', () => {
  it('turns a row with nothing priced into an unknown cost', () => {
    expect(markUnpriced({ cost_usd: 0, cost_known_ratio: 0 }).cost_usd).toBeNull()
  })

  it('leaves priced, partly priced and older-server rows alone', () => {
    expect(markUnpriced({ cost_usd: 3.2, cost_known_ratio: 1 }).cost_usd).toBe(3.2)
    expect(markUnpriced({ cost_usd: 3.2, cost_known_ratio: 0.4 }).cost_usd).toBe(3.2)
    expect(markUnpriced({ cost_usd: 3.2 }).cost_usd).toBe(3.2)
  })
})
