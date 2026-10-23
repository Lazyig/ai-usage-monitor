import { DataUsage24Filled } from '@fluentui/react-icons/headless/svg/data-usage'
import { useState, type FormEvent } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { ApiError } from '../api/client'
import { Button } from '../components/ui/button'
import { Card } from '../components/ui/card'
import { Field } from '../components/ui/field'
import { Input } from '../components/ui/input'
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
    <div className="flex min-h-svh items-center justify-center p-4 pt-[max(16px,env(safe-area-inset-top))] pb-[max(16px,env(safe-area-inset-bottom))]">
      <Card className="w-full max-w-[400px] p-8 shadow-16">
        <form className="flex flex-col gap-4" onSubmit={onSubmit}>
          <div className="flex items-center gap-2.5 [&_h1]:m-0">
            <DataUsage24Filled className="text-brand" />
            <h1 className="text-600 leading-600 font-semibold">AI Usage Monitor</h1>
          </div>
          <span className="-mt-2 text-300 leading-300 text-fg2">관리자 토큰으로 로그인하세요.</span>
          <Field label="관리자 토큰" validationMessage={error ?? undefined}>
            <Input
              id="admin-token"
              type="password"
              autoComplete="off"
              autoFocus
              placeholder="ADMIN_TOKEN"
              value={token}
              onChange={(e) => setToken(e.target.value)}
            />
          </Field>
          <Button type="submit" appearance="primary" disabled={submitting || !token.trim()}>
            {submitting ? '확인 중…' : '로그인'}
          </Button>
        </form>
      </Card>
    </div>
  )
}
