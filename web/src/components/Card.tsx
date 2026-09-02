import type { ReactNode } from 'react'

interface CardProps {
  /** Card title, rendered as an <h2>. Omit for a header-less card. */
  title?: ReactNode
  /** Optional small line under the title. */
  subtitle?: ReactNode
  /** Right-aligned controls (segmented toggles, top-N buttons, filters, …).
   * Wraps below the title row on narrow screens. */
  controls?: ReactNode
  children?: ReactNode
  /** Extra class names appended after the base "card" class, e.g. "section"
   * for the standard 28px stacking margin used between top-level cards. */
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
    <div className={className ? `card ${className}` : 'card'}>
      {hasHeader && (
        <div className="card-header">
          {title != null && (
            <div className="card-heading">
              <h2>{title}</h2>
              {subtitle != null && <p className="card-subtitle">{subtitle}</p>}
            </div>
          )}
          {controls != null && <div className="card-controls">{controls}</div>}
        </div>
      )}
      {children}
    </div>
  )
}
