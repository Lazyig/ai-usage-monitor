// The dashboard's selections (date range, auto refresh, number units) as stored on the
// server, and the conversions to and from the app's state.
import type { DashboardPreferences } from '../api/types'
import { type DateRange, DEFAULT_PRESET, isPreset, rangeForPreset } from './dateRange'
import { DEFAULT_NUMBER_UNITS, isNumberUnits, type NumberUnits } from './format'

export interface DashboardSettings {
  range: DateRange
  autoRefresh: boolean
  numberUnits: NumberUnits
}

export const DEFAULT_SETTINGS: DashboardSettings = {
  range: rangeForPreset(DEFAULT_PRESET),
  autoRefresh: false,
  numberUnits: DEFAULT_NUMBER_UNITS,
}

const DATE = /^\d{4}-\d{2}-\d{2}$/

/** Settings from a stored document. Anything missing or unusable falls back
 * to the default; relative presets are recomputed for today. */
export function fromPreferences(p: DashboardPreferences | null | undefined, today?: string): DashboardSettings {
  let range = rangeForPreset(DEFAULT_SETTINGS.range.preset, today)
  const r = p?.range
  if (r && isPreset(r.preset)) {
    if (r.preset !== 'custom') range = rangeForPreset(r.preset, today)
    else if (r.from && r.to && DATE.test(r.from) && DATE.test(r.to) && r.from < r.to) range = { preset: 'custom', from: r.from, to: r.to }
  }
  const units = p?.number_units
  return {
    range,
    autoRefresh: typeof p?.auto_refresh === 'boolean' ? p.auto_refresh : DEFAULT_SETTINGS.autoRefresh,
    numberUnits: isNumberUnits(units) ? units : DEFAULT_SETTINGS.numberUnits,
  }
}

/** The document to store. Relative presets keep only their name. */
export function toPreferences(s: DashboardSettings): DashboardPreferences {
  const range = s.range.preset === 'custom' ? { preset: 'custom', from: s.range.from, to: s.range.to } : { preset: s.range.preset }
  return { range, auto_refresh: s.autoRefresh, number_units: s.numberUnits }
}
