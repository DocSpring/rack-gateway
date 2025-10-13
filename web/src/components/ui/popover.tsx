'use client'

import { Content, Root, Trigger } from '@radix-ui/react-popover'
import type { ComponentProps } from 'react'

import { cn } from '@/lib/utils'

const Popover = Root
const PopoverTrigger = Trigger
const PopoverContent = ({
  align = 'center',
  className,
  sideOffset = 4,
  ...props
}: ComponentProps<typeof Content>) => (
  <Content
    align={align}
    className={cn(
      'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[state=closed]:slide-out-to-bottom-2 data-[state=open]:slide-in-from-top-2 z-50 w-72 rounded-md border border-border bg-popover p-4 text-popover-foreground shadow-md outline-none data-[state=closed]:animate-out data-[state=open]:animate-in',
      className
    )}
    sideOffset={sideOffset}
    {...props}
  />
)

export { Popover, PopoverTrigger, PopoverContent }
