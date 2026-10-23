import { useState } from 'react'
import { Button } from './ui/button'
import { Dialog, DialogActions, DialogBody, DialogContent, DialogTitle } from './ui/dialog'
import { Field } from './ui/field'
import { Input } from './ui/input'

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

/** Modal text prompt (replaces window.prompt): one labelled input, Enter
 * submits, confirm disabled while the value is empty or unchanged. */
export function PromptDialog({ title, message, label, initialValue, confirmLabel, cancelLabel = '취소', maxLength = 100, busy, onConfirm, onCancel }: PromptDialogProps) {
  const [value, setValue] = useState(initialValue)
  const trimmed = value.trim()
  const canConfirm = !busy && trimmed !== '' && trimmed !== initialValue

  return (
    // While busy the dialog ignores Escape and outside clicks, like an alert dialog.
    <Dialog open onOpenChange={(open) => !open && !busy && onCancel()}>
      <DialogContent>
        <DialogTitle>{title}</DialogTitle>
        <DialogBody className="flex flex-col gap-3 [&_p]:m-0 [&_p]:text-fg1">
          {message && <p>{message}</p>}
          <Field label={label}>
            <Input
              value={value}
              maxLength={maxLength}
              onChange={(e) => setValue(e.target.value)}
              autoFocus
              autoComplete="off"
              onFocus={(e) => e.target.select()}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && canConfirm) onConfirm(trimmed)
              }}
            />
          </Field>
        </DialogBody>
        <DialogActions>
          <Button onClick={onCancel} disabled={busy}>
            {cancelLabel}
          </Button>
          <Button appearance="primary" onClick={() => onConfirm(trimmed)} disabled={!canConfirm}>
            {busy ? '처리 중…' : confirmLabel}
          </Button>
        </DialogActions>
      </DialogContent>
    </Dialog>
  )
}
