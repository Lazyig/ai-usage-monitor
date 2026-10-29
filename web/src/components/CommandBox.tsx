import { copyText } from '../lib/clipboard'
import { Button } from './ui/button'

/** Splits a command string around a placeholder substring so the
 * placeholder can be highlighted in a distinct color; the rest of the
 * command renders as plain monospace text. */
export function CommandBox({ command, highlight = '' }: { command: string; highlight?: string }) {
  const idx = highlight ? command.indexOf(highlight) : -1
  const before = idx >= 0 ? command.slice(0, idx) : command
  const after = idx >= 0 ? command.slice(idx + highlight.length) : ''
  return (
    <div className="relative mb-1">
      <pre className="m-0 max-w-full overflow-x-auto rounded-md border border-code-stroke bg-code-bg py-2.5 pr-11 pl-3 font-mono text-200 whitespace-pre text-code-fg">
        {before}
        {idx >= 0 && <span className="font-mono font-semibold text-series-4">{highlight}</span>}
        {after}
      </pre>
      <Button touch size="small" className="absolute top-1.5 right-1.5 shadow-[-10px_0_10px_var(--code-bg)]"
        onClick={() => copyText(command)}
        aria-label="명령 복사"
      >
        복사
      </Button>
    </div>
  )
}
