import { copyText } from '../lib/clipboard'
import { Button } from './ui/button'

/** A one-line code value (token, command) with a copy button beside it. */
export function CopyableCode({ value, ariaLabel }: { value: string; ariaLabel?: string }) {
  return (
    <div className="mt-1.5 flex items-center gap-2 [&_[data-slot=button]]:shrink-0">
      <code className="min-w-0 flex-1 overflow-x-auto rounded-md border border-stroke2 bg-bg1 px-2.5 py-2 font-mono text-200 whitespace-pre">
        {value}
      </code>
      <Button touch size="small" onClick={() => copyText(value)} aria-label={ariaLabel}>
        복사
      </Button>
    </div>
  )
}
