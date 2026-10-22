import {
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Field,
  Input,
} from '@fluentui/react-components'
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

/** Fluent modal text prompt (replaces window.prompt): one labelled input, Enter
 * submits, confirm disabled while the value is empty or unchanged. */
export function PromptDialog({ title, message, label, initialValue, confirmLabel, cancelLabel = '취소', maxLength = 100, busy, onConfirm, onCancel }: PromptDialogProps) {
  const [value, setValue] = useState(initialValue)
  const trimmed = value.trim()
  const canConfirm = !busy && trimmed !== '' && trimmed !== initialValue

  return (
    <Dialog open modalType={busy ? 'alert' : 'modal'} onOpenChange={(_, data) => !data.open && !busy && onCancel()}>
      <DialogSurface>
        <DialogBody>
          <DialogTitle>{title}</DialogTitle>
          <DialogContent className="flex flex-col gap-3 [&_p]:m-0 [&_p]:text-fg1">
            {message && <p>{message}</p>}
            <Field label={label}>
              <Input
                value={value}
                maxLength={maxLength}
                onChange={(_, d) => setValue(d.value)}
                autoFocus
                autoComplete="off"
                onFocus={(e) => e.target.select()}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && canConfirm) onConfirm(trimmed)
                }}
              />
            </Field>
          </DialogContent>
          <DialogActions>
            <Button onClick={onCancel} disabled={busy}>
              {cancelLabel}
            </Button>
            <Button appearance="primary" onClick={() => onConfirm(trimmed)} disabled={!canConfirm}>
              {busy ? '처리 중…' : confirmLabel}
            </Button>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  )
}
