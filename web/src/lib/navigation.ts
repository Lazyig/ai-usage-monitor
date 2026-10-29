/** The page to return to after login: an in-app path only. "//host" and
 * "/\\host" would be read as another origin (react-router advisory
 * GHSA-wrjc-x8rr-h8h6). */
export function safeReturnPath(p: string | undefined): string {
  if (!p || !p.startsWith('/') || p.startsWith('//') || p.startsWith('/\\')) return '/'
  return p
}
