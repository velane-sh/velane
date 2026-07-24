import { ChevronDown } from 'lucide-react'
import type { SelectHTMLAttributes } from 'react'

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  containerClassName?: string
}

export default function Select({
  className = '',
  containerClassName = '',
  children,
  ...props
}: SelectProps) {
  return (
    <div className={`relative ${containerClassName}`}>
      <select
        className={`h-9 w-full appearance-none rounded-lg border border-gray-300 bg-white py-0 pl-3 pr-9 text-sm text-gray-700 outline-none transition-colors hover:border-gray-400 focus:border-gray-500 focus:ring-2 focus:ring-gray-200 ${className}`}
        {...props}
      >
        {children}
      </select>
      <ChevronDown
        aria-hidden="true"
        className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-500"
      />
    </div>
  )
}
