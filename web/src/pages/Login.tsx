import { Body1, Button, Card, Field, Input, Text } from '@fluentui/react-components'
import { DataUsage24Filled } from '@fluentui/react-icons'
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
    <div className="flex min-h-svh items-center justify-center p-4 pt-[max(16px,env(safe-area-inset-top))] pb-[max(16px,env(safe-area-inset-bottom))]">
      <Card size="large" className="login-card">
        <form className="flex flex-col gap-4" onSubmit={onSubmit}>
          <div className="flex items-center gap-2.5 [&_h1]:m-0">
            <DataUsage24Filled className="text-brand" />
            <Text as="h1" size={600} weight="semibold">
              AI Usage Monitor
            </Text>
          </div>
          <Body1 className="-mt-2 text-fg2">관리자 토큰으로 로그인하세요.</Body1>
          <Field label="관리자 토큰" validationState={error ? 'error' : 'none'} validationMessage={error ?? undefined}>
            <Input
              id="admin-token"
              type="password"
              autoComplete="off"
              autoFocus
              placeholder="ADMIN_TOKEN"
              value={token}
              onChange={(_, d) => setToken(d.value)}
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
