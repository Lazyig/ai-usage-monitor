import { useState } from 'react'

interface PromptDialogProps {
  title: string
  message?: string
  label: string
  initialValue: string
  confirmLabel: string
  cancelLabel?: string
  maxLength?: number
  busy?: boolean
  onConfirm: (value: string) => void
  onCancel: () => void
}

/** Modal text prompt (replaces window.prompt): one labelled input, Enter submits,
 * confirm disabled while the value is empty or unchanged. */
export function PromptDialog({ title, message, label, initialValue, confirmLabel, cancelLabel = '취소', maxLength = 100, busy, onConfirm, onCancel }: PromptDialogProps) {
  const [value, setValue] = useState(initialValue)
  const trimmed = value.trim()
  const canConfirm = !busy && trimmed !== '' && trimmed !== initialValue

  return (
    <div className="modal-overlay" role="presentation" onClick={() => !busy && onCancel()}>
      <div className="modal-card" role="dialog" aria-modal="true" aria-labelledby="prompt-dialog-title" onClick={(e) => e.stopPropagation()}>
        <h3 id="prompt-dialog-title">{title}</h3>
        {message && <p>{message}</p>}
        <div className="field">
          <label htmlFor="prompt-dialog-input">{label}</label>
          <input
            id="prompt-dialog-input"
            value={value}
            maxLength={maxLength}
            onChange={(e) => setValue(e.target.value)}
            autoFocus
            autoComplete="off"
            onFocus={(e) => e.target.select()}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && canConfirm) onConfirm(trimmed)
              if (e.key === 'Escape' && !busy) onCancel()
            }}
          />
        </div>
        <div className="modal-actions">
          <button type="button" className="btn" onClick={onCancel} disabled={busy}>
            {cancelLabel}
          </button>
          <button type="button" className="btn btn-primary" onClick={() => onConfirm(trimmed)} disabled={!canConfirm}>
            {busy ? '처리 중…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
