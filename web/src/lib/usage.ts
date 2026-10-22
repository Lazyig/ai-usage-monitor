// Row-level derivations shared by every usage table and chart. The API returns
// the four token classes separately and never a total, so "tokens" is always a
// derived quantity — defined once here rather than re-summed at each call site.

/** Any usage row/point carrying the four billable token classes. */
export interface TokenCounts {
  input: number
  output: number
  cache_read: number
  cache_write: number
}

/** Total billable tokens: input + output + cache read + cache write. */
export function tokensOf(r: TokenCounts): number {
  return r.input + r.output + r.cache_read + r.cache_write
}

/** "가격 반영 99.8%" for a partly priced row. Rounded down, so a row missing
 * any price never reads as 100%. */
export function pricedShareLabel(ratio: number): string {
  return `가격 반영 ${(Math.floor(ratio * 1000) / 10).toFixed(1)}%`
}

/** Share of prompt tokens served from cache — cache_read / (input + cache_read).
 * Zero when nothing was read at all, so an idle row reads as 0% not NaN. */
export function cacheHitRatio(r: TokenCounts): number {
  const denom = r.input + r.cache_read
  return denom > 0 ? r.cache_read / denom : 0
}
