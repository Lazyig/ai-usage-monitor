// The one place the app touches Web Storage. Every access is wrapped, because
// `localStorage` throws (not returns null) in private windows, when site data
// is blocked, and inside some embedded webviews — a bare getItem in a module
// initializer is enough to white-screen the whole app.

export function readStored(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

export function writeStored(key: string, value: string): void {
  try {
    localStorage.setItem(key, value)
  } catch {
    // Non-persistent is an acceptable degradation — never fail the caller.
  }
}

export function removeStored(key: string): void {
  try {
    localStorage.removeItem(key)
  } catch {
    // ignore
  }
}
