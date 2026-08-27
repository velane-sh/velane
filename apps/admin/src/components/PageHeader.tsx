import type { HTMLAttributes, ReactNode } from 'react'
import { cn } from '../lib/cn'

interface PageHeaderProps extends Omit<HTMLAttributes<HTMLElement>, 'title'> {
  title: ReactNode
  eyebrow?: ReactNode
  description?: ReactNode
  actions?: ReactNode
  className?: string
}

export default function PageHeader({ title, eyebrow, description, actions, className, ...props }: PageHeaderProps) {
  return (
    <header className={cn('mb-8 flex flex-wrap items-start justify-between gap-4', className)} {...props}>
      <div>
        {eyebrow && <p className="text-xs font-semibold uppercase tracking-[0.09em] text-content-muted">{eyebrow}</p>}
        <h1 className={cn('text-2xl font-bold tracking-tight text-content', eyebrow && 'mt-2')}>{title}</h1>
        {description && <p className="mt-1 text-sm text-content-muted">{description}</p>}
      </div>
      {actions}
    </header>
  )
}
