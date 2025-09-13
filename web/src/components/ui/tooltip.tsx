import {
  Arrow as TooltipArrowPrimitive,
  Content as TooltipContentPrimitive,
  Portal as TooltipPortalPrimitive,
  Provider as TooltipProviderPrimitive,
  Root as TooltipRoot,
  Trigger as TooltipTriggerPrimitive,
} from '@radix-ui/react-tooltip'
import type * as React from 'react'
import { cn } from '@/lib/utils'

function TooltipProvider({ ...props }: React.ComponentProps<typeof TooltipProviderPrimitive>) {
  return <TooltipProviderPrimitive delayDuration={150} skipDelayDuration={100} {...props} />
}

function Tooltip({ ...props }: React.ComponentProps<typeof TooltipRoot>) {
  return <TooltipRoot {...props} />
}

function TooltipTrigger({ ...props }: React.ComponentProps<typeof TooltipTriggerPrimitive>) {
  return <TooltipTriggerPrimitive {...props} />
}

function TooltipContent({
  className,
  side = 'top',
  ...props
}: React.ComponentProps<typeof TooltipContentPrimitive>) {
  return (
    <TooltipPortalPrimitive>
      <TooltipContentPrimitive
        className={cn(
          'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 z-50 overflow-hidden rounded-md border bg-popover px-3 py-1.5 text-popover-foreground text-xs shadow-md data-[state=closed]:animate-out data-[state=open]:animate-in',
          className
        )}
        side={side}
        sideOffset={8}
        {...props}
      >
        {props.children}
        <TooltipArrowPrimitive className="fill-popover" />
      </TooltipContentPrimitive>
    </TooltipPortalPrimitive>
  )
}

export { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger }
