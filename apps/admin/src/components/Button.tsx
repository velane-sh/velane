import type { ButtonHTMLAttributes } from 'react'
import { cn } from '../lib/cn'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ButtonSize = 'sm' | 'md' | 'icon'

export function buttonClasses({
  variant = 'primary',
  size = 'md',
}: {
  variant?: ButtonVariant
  size?: ButtonSize
} = {}) {
  return cn(
    'inline-flex items-center justify-center gap-2 rounded-lg font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg disabled:opacity-50 disabled:pointer-events-none',
    {
      'bg-accent text-accent-fg hover:bg-accent-hover': variant === 'primary',
      'border border-line-strong bg-surface text-content hover:bg-surface-muted': variant === 'secondary',
      'text-content-muted hover:bg-surface-muted hover:text-content': variant === 'ghost',
      'text-danger-text hover:bg-danger-subtle': variant === 'danger',
      'h-8 px-3 text-sm': size === 'sm',
      'h-9 px-4 text-sm': size === 'md',
      'h-8 w-8': size === 'icon',
    },
  )
}

export default function Button({
  className,
  variant = 'primary',
  size = 'md',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant
  size?: ButtonSize
}) {
  return <button className={cn(buttonClasses({ variant, size }), className)} {...props} />
}
