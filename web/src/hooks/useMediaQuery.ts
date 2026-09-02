import { useEffect, useState } from 'react'

/**
 * Tracks whether a CSS media query currently matches, updating live as the
 * viewport changes. Chart sizing (ResponsiveContainer height, axis tick font
 * size, tick interval) is passed to recharts as numeric props rather than
 * CSS, so those need a JS breakpoint check like this one instead of a media
 * query.
 */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => (typeof window !== 'undefined' ? window.matchMedia(query).matches : false))

  useEffect(() => {
    const mql = window.matchMedia(query)
    const onChange = () => setMatches(mql.matches)
    onChange()
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [query])

  return matches
}

/** Matches the phone breakpoint used throughout index.css. */
export const PHONE_QUERY = '(max-width: 640px)'
