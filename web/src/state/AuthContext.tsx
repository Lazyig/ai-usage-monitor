import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { clearStoredToken, getStoredToken, onUnauthorized, setStoredToken, verifyToken } from '../api/client'

interface AuthContextValue {
  token: string | null
  isAuthenticated: boolean
  /** Verifies the token against GET /api/v1/devices, then stores it. Throws on 401. */
  login: (token: string) => Promise<void>
  logout: () => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState<string | null>(() => getStoredToken())

  useEffect(() => {
    // Any 401 from anywhere in the app drops back to the login screen.
    onUnauthorized(() => {
      clearStoredToken()
      setToken(null)
    })
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({
      token,
      isAuthenticated: token !== null,
      login: async (candidate: string) => {
        await verifyToken(candidate)
        setStoredToken(candidate)
        setToken(candidate)
      },
      logout: () => {
        clearStoredToken()
        setToken(null)
      },
    }),
    [token],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
