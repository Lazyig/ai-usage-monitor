import { Tooltip as TooltipPrimitive } from 'radix-ui'
import type { ReactElement, ReactNode } from 'react'

interface TooltipProps {
  content: ReactNode
  /** One focusable or hoverable element; the tip describes it. */
  children: ReactElement
}

/** Fluent 2 tooltip: a small light bubble (no arrow, as Fluent's default) after a 250ms delay. */
function Tooltip({ content, children }: TooltipProps) {
  return (
    <TooltipPrimitive.Provider delayDuration={250}>
      <TooltipPrimitive.Root>
        <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
        <TooltipPrimitive.Portal>
          <TooltipPrimitive.Content
            data-slot="tooltip"
            sideOffset={4}
            collisionPadding={8}
            className="z-50 box-border max-w-60 cursor-default rounded-md border border-transparent bg-bg1 px-[11px] pt-1 pb-1.5 text-200 leading-200 [overflow-wrap:break-word] text-fg1 [filter:drop-shadow(0_0_2px_var(--colorNeutralShadowAmbient))_drop-shadow(0_4px_8px_var(--colorNeutralShadowKey))]"
          >
            {content}
          </TooltipPrimitive.Content>
        </TooltipPrimitive.Portal>
      </TooltipPrimitive.Root>
    </TooltipPrimitive.Provider>
  )
}

export { Tooltip }
