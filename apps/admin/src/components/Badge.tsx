import type { HTMLAttributes } from 'react'
import { cn } from '../lib/cn'

export type BadgeVariant = 'neutral' | 'accent' | 'success' | 'danger' | 'warning'

export default function Badge({
  className,
  variant = 'neutral',
  ...props
}: HTMLAttributes<HTMLSpanElement> & { variant?: BadgeVariant }) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium',
        {
          'border-line bg-surface-muted text-content-muted': variant === 'neutral',
          'border-accent-border bg-accent-subtle text-accent': variant === 'accent',
          'border-success-border bg-success-subtle text-success-text': variant === 'success',
          'border-danger-border bg-danger-subtle text-danger-text': variant === 'danger',
          'border-amber-200 bg-amber-50 text-amber-700': variant === 'warning',
        },
        className,
      )}
      {...props}
    />
  )
}
