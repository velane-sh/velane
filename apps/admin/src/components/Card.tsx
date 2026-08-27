import type { HTMLAttributes, ReactNode } from 'react'
import { cn } from '../lib/cn'

interface CardProps extends HTMLAttributes<HTMLDivElement> {
  padded?: boolean
}

export function Card({ className, padded = true, ...props }: CardProps) {
  return <div className={cn('rounded-lg border border-line bg-surface shadow-sm', padded && 'p-6', className)} {...props} />
}

export function CardHeader({
  title,
  description,
  actions,
  className,
  ...props
}: {
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  className?: string
} & HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('flex flex-wrap items-center justify-between gap-3 border-b border-line px-5 py-4', className)} {...props}>
      <div>
        <h2 className="text-sm font-semibold text-content">{title}</h2>
        {description && <p className="mt-1 text-sm text-content-muted">{description}</p>}
      </div>
      {actions}
    </div>
  )
}

export function CardBody({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('p-6', className)} {...props} />
}

export default Card
