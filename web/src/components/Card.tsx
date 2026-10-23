import type { ReactNode } from 'react'
import { Card as CardSurface } from './ui/card'

interface CardProps {
  /** Card title, rendered as an <h2>. Omit for a header-less card. */
  title?: ReactNode
  /** Optional small line under the title. */
  subtitle?: ReactNode
  /** Right-aligned controls (segmented toggles, top-N buttons, filters, …).
   * Wraps below the title row on narrow screens. */
  controls?: ReactNode
  children?: ReactNode
  /** Extra class names for the card surface, e.g. "mb-6" for the standard
   * stacking margin used between top-level cards. */
  className?: string
}

/**
 * Shared card shell used for every chart/table panel that has a title
 * and/or controls, so header spacing and control alignment are identical
 * across pages. Title (+ optional subtitle) sits on the left, controls are
 * right-aligned on the same row and wrap below the title on narrow screens.
 */
export function Card({ title, subtitle, controls, children, className }: CardProps) {
  const hasHeader = title != null || controls != null
  return (
    <CardSurface className={className}>
      {hasHeader && (
        <div className="flex flex-wrap items-center justify-between gap-3">
          {title != null && (
            <div className="flex flex-col gap-0.5 [&>h2]:m-0">
              <h2 className="text-400 leading-400 font-semibold">{title}</h2>
              {subtitle != null && <span className="text-200 leading-200 text-fg3">{subtitle}</span>}
            </div>
          )}
          {controls != null && <div className="flex flex-wrap items-center gap-2">{controls}</div>}
        </div>
      )}
      {children}
    </CardSurface>
  )
}
