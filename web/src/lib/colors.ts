// Fixed categorical color assignment (never cycled) per the dataviz skill's
// validated palette. Sources are always assigned the same slot regardless of
// which are present, so a filter never repaints the survivors.
import type { Source } from '../api/types'

export const SOURCE_COLOR_VARS: Record<Source, string> = {
  claude_code: 'var(--series-1)',
  codex: 'var(--series-2)',
  antigravity: 'var(--series-3)',
}

export const SOURCE_LABELS: Record<Source, string> = {
  claude_code: 'Claude Code',
  codex: 'Codex',
  antigravity: 'Antigravity',
}

const FALLBACK_SLOTS = ['var(--series-4)', 'var(--series-5)', 'var(--series-6)', 'var(--series-7)', 'var(--series-8)']

/** Resolves a color for a series key; known sources get their fixed slot, anything else falls through in order. */
export function colorForKey(key: string, indexIfUnknown: number): string {
  if (key in SOURCE_COLOR_VARS) return SOURCE_COLOR_VARS[key as Source]
  return FALLBACK_SLOTS[indexIfUnknown % FALLBACK_SLOTS.length]
}

export function labelForKey(key: string): string {
  return (SOURCE_LABELS as Record<string, string>)[key] ?? key
}
