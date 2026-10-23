import { useState } from 'react'
import { Button } from './ui/button'
import { Dialog, DialogActions, DialogBody, DialogContent, DialogTitle } from './ui/dialog'
import { Field } from './ui/field'
import { Input } from './ui/input'

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

/** Modal confirmation for device actions (재발급 / 폐기 / 재동기화 /
 * 완전 삭제). Replaces the browser's native confirm so it matches the app. */
export function ConfirmDialog({ title, message, confirmLabel, cancelLabel = '취소', requireText, busy, danger = true, onConfirm, onCancel }: ConfirmDialogProps) {
  const [typed, setTyped] = useState('')
  const canConfirm = !busy && (!requireText || typed === requireText)

  return (
    // While busy the dialog ignores Escape and outside clicks, like an alert dialog.
    <Dialog open onOpenChange={(open) => !open && !busy && onCancel()}>
      <DialogContent>
        <DialogTitle>{title}</DialogTitle>
        <DialogBody className="flex flex-col gap-3 [&_p]:m-0 [&_p]:text-fg1">
          <p>{message}</p>
          {requireText && (
            <Field
              label={
                <>
                  계속하려면 기기 이름 <strong>{requireText}</strong> 을(를) 입력하세요
                </>
              }
            >
              <Input
                value={typed}
                onChange={(e) => setTyped(e.target.value)}
                autoFocus
                autoComplete="off"
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && canConfirm) onConfirm()
                }}
              />
            </Field>
          )}
        </DialogBody>
        <DialogActions>
          <Button onClick={onCancel} disabled={busy}>
            {cancelLabel}
          </Button>
          <Button appearance={danger ? 'danger' : 'primary'} onClick={onConfirm} disabled={!canConfirm}>
            {busy ? '처리 중…' : confirmLabel}
          </Button>
        </DialogActions>
      </DialogContent>
    </Dialog>
  )
}
