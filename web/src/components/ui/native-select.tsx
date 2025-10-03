import type { SelectHTMLAttributes } from 'react'
import { forwardRef } from 'react'

import { cn } from '@/lib/utils'

const baseClass =
  'h-9 w-fit min-w-[8rem] rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none transition-[color,box-shadow] focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-input/30 dark:focus-visible:ring-ring/50'

const NativeSelect = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(
  ({ className, children, ...props }, ref) => (
    <select className={cn(baseClass, className)} ref={ref} {...props}>
      {children}
    </select>
  )
)

export { NativeSelect }
