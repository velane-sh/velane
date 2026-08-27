import type { HTMLAttributes, ThHTMLAttributes, TdHTMLAttributes } from 'react'
import { cn } from '../lib/cn'

export function Table({
  className,
  minWidthClassName,
  children,
  ...props
}: HTMLAttributes<HTMLDivElement> & { minWidthClassName?: string }) {
  return (
    <div className={cn('overflow-hidden rounded-lg border border-line bg-surface', className)} {...props}>
      <div className="overflow-x-auto">
        <table className={cn('w-full text-sm', minWidthClassName)}>
          {children}
        </table>
      </div>
    </div>
  )
}

export function THead({ className, ...props }: HTMLAttributes<HTMLTableSectionElement>) {
  return <thead className={cn('bg-surface-muted text-left text-xs font-medium uppercase tracking-wide text-content-muted', className)} {...props} />
}

export function TH({ className, ...props }: ThHTMLAttributes<HTMLTableCellElement>) {
  return <th className={cn('px-6 py-3', className)} {...props} />
}

export function TBody({ className, ...props }: HTMLAttributes<HTMLTableSectionElement>) {
  return <tbody className={cn('divide-y divide-line', className)} {...props} />
}

export function TR({
  className,
  interactive = false,
  ...props
}: HTMLAttributes<HTMLTableRowElement> & { interactive?: boolean }) {
  return <tr className={cn('hover:bg-surface-muted', interactive && 'cursor-pointer', className)} {...props} />
}

export function TD({ className, ...props }: TdHTMLAttributes<HTMLTableCellElement>) {
  return <td className={cn('px-6 py-4', className)} {...props} />
}
