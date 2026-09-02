// Shared error-message formatting so every "데이터를 불러오지 못했습니다" panel
// includes enough detail (HTTP status + the server's `error` field when
// present) to actually diagnose the problem, instead of a bare generic string.
import { ApiError } from '../api/client'

export function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    return `HTTP ${err.status} · ${err.message}`
  }
  if (err instanceof Error) return err.message
  return String(err)
}
