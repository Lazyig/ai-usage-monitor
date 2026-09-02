// Fixed categorical color assignment (never cycled) per the dataviz skill's
// validated palette. Sources are always assigned the same slot regardless of
// which are present, so a filter never repaints the survivors.
import type { Source } from '../api/types'

export const SOURCE_COLOR_VARS: Record<Source, string> = {
  claude_code: 'var(--series-1)',
  codex: 'var(--series-2)',
  antigravity: 'var(--series-3)',
}

/** Fixed display order for the three agents — used by every per-agent view so
 * a source keeps its slot (and color) whether or not it has data. */
export const SOURCE_ORDER: Source[] = ['claude_code', 'codex', 'antigravity']

export const SOURCE_LABELS: Record<Source, string> = {
  claude_code: 'Claude Code',
  codex: 'Codex',
  antigravity: 'Antigravity',
}

const FALLBACK_SLOTS = ['var(--series-4)', 'var(--series-5)', 'var(--series-6)', 'var(--series-7)', 'var(--series-8)']

/** Color for a non-source entity (device, model, …) by its fixed slot index.
 * A negative index means the neutral "기타" bucket that folds a long tail. */
export function colorForSlot(index: number): string {
  if (index < 0) return 'var(--text-muted)'
  return FALLBACK_SLOTS[index % FALLBACK_SLOTS.length]
}

/** Resolves a color for a series key; known sources get their fixed slot, anything else falls through in order. */
export function colorForKey(key: string, indexIfUnknown: number): string {
  if (key in SOURCE_COLOR_VARS) return SOURCE_COLOR_VARS[key as Source]
  return FALLBACK_SLOTS[indexIfUnknown % FALLBACK_SLOTS.length]
}

export function labelForKey(key: string): string {
  return (SOURCE_LABELS as Record<string, string>)[key] ?? key
}

// Token class is a second, independent categorical dimension (not a source),
// so it draws fixed slots from the same fallback ramp used for arbitrary
// keys elsewhere (models/projects) — never cycled, in stack order.
export type TokenClass = 'input' | 'cache_read' | 'cache_write' | 'output'

export const TOKEN_CLASS_ORDER: TokenClass[] = ['input', 'cache_read', 'cache_write', 'output']

export const TOKEN_CLASS_COLORS: Record<TokenClass, string> = {
  input: 'var(--series-4)',
  cache_read: 'var(--series-5)',
  cache_write: 'var(--series-6)',
  output: 'var(--series-7)',
}

export const TOKEN_CLASS_LABELS: Record<TokenClass, string> = {
  input: '입력',
  cache_read: '캐시 읽기',
  cache_write: '캐시 쓰기',
  output: '출력',
}
