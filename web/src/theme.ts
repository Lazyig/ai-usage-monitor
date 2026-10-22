import { type Theme, webDarkTheme, webLightTheme } from '@fluentui/react-components'

// Fluent 2 web themes with the Windows 11 type ramp first (Segoe UI Variable),
// then Malgun Gothic so Hangul renders in the system face instead of a fallback.
const FONT_FAMILY =
  "'Segoe UI Variable Text', 'Segoe UI', 'Malgun Gothic', -apple-system, BlinkMacSystemFont, 'Apple SD Gothic Neo', Roboto, 'Helvetica Neue', sans-serif"

export const lightTheme: Theme = { ...webLightTheme, fontFamilyBase: FONT_FAMILY }
export const darkTheme: Theme = { ...webDarkTheme, fontFamilyBase: FONT_FAMILY }

/** Follows the OS light/dark setting, like Windows apps do. */
export const DARK_QUERY = '(prefers-color-scheme: dark)'
