import { useState, type FormEvent } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { ApiError } from '../api/client'
import { useAuth } from '../state/AuthContext'

export function Login() {
  const { isAuthenticated, login } = useAuth()
  const location = useLocation()
  const [token, setToken] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (isAuthenticated) {
    const from = (location.state as { from?: Location } | null)?.from
    return <Navigate to={from?.pathname ?? '/'} replace />
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (!token.trim()) return
    setSubmitting(true)
    setError(null)
    try {
      await login(token.trim())
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setError('토큰이 올바르지 않습니다.')
      } else {
        setError(err instanceof Error ? err.message : '로그인에 실패했습니다.')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="login-screen">
      <form className="login-card" onSubmit={onSubmit}>
        <h1>AI Usage Monitor</h1>
        <p className="login-subtitle">관리자 토큰으로 로그인하세요.</p>
        <label htmlFor="admin-token">관리자 토큰</label>
        <input
          id="admin-token"
          type="password"
          autoComplete="off"
          autoFocus
          placeholder="ADMIN_TOKEN"
          value={token}
          onChange={(e) => setToken(e.target.value)}
        />
        {error && (
          <p className="login-error" role="alert">
            {error}
          </p>
        )}
        <button type="submit" className="btn btn-primary" disabled={submitting || !token.trim()}>
          {submitting ? '확인 중…' : '로그인'}
        </button>
      </form>
    </div>
  )
}
