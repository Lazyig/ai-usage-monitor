import { useState } from 'react'

interface ConfirmDialogProps {
  title: string
  /** One sentence stating the consequence of the action. */
  message: string
  confirmLabel: string
  cancelLabel?: string
  /** When set, the confirm button stays disabled until the user types this
   * exact text (used for irreversible actions like 완전 삭제). */
  requireText?: string
  busy?: boolean
  /** Destructive actions get the red confirm button (default); others the primary one. */
  danger?: boolean
  onConfirm: () => void
  onCancel: () => void
}

/** Generic modal confirmation dialog for device actions (재발급 / 폐기 / 재동기화 /
 * 완전 삭제). Replaces the browser's native confirm so it matches the app. */
export function ConfirmDialog({ title, message, confirmLabel, cancelLabel = '취소', requireText, busy, danger = true, onConfirm, onCancel }: ConfirmDialogProps) {
  const [typed, setTyped] = useState('')
  const canConfirm = !busy && (!requireText || typed === requireText)

  return (
    <div className="modal-overlay" role="presentation" onClick={() => !busy && onCancel()}>
      <div className="modal-card" role="dialog" aria-modal="true" aria-labelledby="confirm-dialog-title" onClick={(e) => e.stopPropagation()}>
        <h3 id="confirm-dialog-title">{title}</h3>
        <p>{message}</p>
        {requireText && (
          <div className="field">
            <label htmlFor="confirm-dialog-typed">
              계속하려면 기기 이름 <strong>{requireText}</strong> 을(를) 입력하세요
            </label>
            <input
              id="confirm-dialog-typed"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoFocus
              autoComplete="off"
              onKeyDown={(e) => {
                if (e.key === 'Enter' && canConfirm) onConfirm()
              }}
            />
          </div>
        )}
        <div className="modal-actions">
          <button type="button" className="btn" onClick={onCancel} disabled={busy}>
            {cancelLabel}
          </button>
          <button type="button" className={danger ? 'btn btn-danger' : 'btn btn-primary'} onClick={onConfirm} disabled={!canConfirm}>
            {busy ? '처리 중…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
