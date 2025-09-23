import {
  Action as ToastPrimitiveAction,
  Close as ToastPrimitiveClose,
  Description as ToastPrimitiveDescription,
  Provider as ToastPrimitiveProvider,
  Root as ToastPrimitiveRoot,
  Title as ToastPrimitiveTitle,
  Viewport as ToastPrimitiveViewport,
} from '@radix-ui/react-toast'
import { cva, type VariantProps } from 'class-variance-authority'
import React from 'react'

import { cn } from '@/lib/utils'

const ToastProvider = ToastPrimitiveProvider

const ToastViewport = React.forwardRef<
  React.ElementRef<typeof ToastPrimitiveViewport>,
  React.ComponentPropsWithoutRef<typeof ToastPrimitiveViewport>
>(({ className, ...props }, ref) => (
  <ToastPrimitiveViewport
    className={cn(
      'fixed right-0 bottom-0 z-[100] flex w-full max-w-sm flex-col gap-2 p-4 sm:right-0 sm:bottom-0 sm:max-w-md',
      'outline-none',
      className
    )}
    ref={ref}
    {...props}
  />
))
ToastViewport.displayName = ToastPrimitiveViewport.displayName

const toastVariants = cva(
  'group pointer-events-auto relative flex w-full items-start gap-3 overflow-hidden rounded-lg border px-4 py-3 shadow-lg transition-all',
  {
    variants: {
      variant: {
        default: 'border-border bg-popover text-popover-foreground',
        success: 'border-emerald-500/40 bg-emerald-600/95 text-emerald-50 shadow-emerald-400/20',
        error:
          'border-destructive/40 bg-destructive text-destructive-foreground shadow-destructive/20',
        warning: 'border-amber-500/40 bg-amber-500/95 text-amber-50 shadow-amber-400/20',
        info: 'border-primary/40 bg-primary text-primary-foreground shadow-primary/20',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  }
)

const Toast = React.forwardRef<
  React.ElementRef<typeof ToastPrimitiveRoot>,
  React.ComponentPropsWithoutRef<typeof ToastPrimitiveRoot> & VariantProps<typeof toastVariants>
>(({ className, variant, ...props }, ref) => (
  <ToastPrimitiveRoot
    className={cn(
      toastVariants({ variant }),
      'data-[state=closed]:animate-fade-out data-[state=open]:animate-slide-in data-[swipe=end]:animate-slide-out',
      className
    )}
    ref={ref}
    {...props}
  />
))
Toast.displayName = ToastPrimitiveRoot.displayName

const ToastTitle = React.forwardRef<
  React.ElementRef<typeof ToastPrimitiveTitle>,
  React.ComponentPropsWithoutRef<typeof ToastPrimitiveTitle>
>(({ className, ...props }, ref) => (
  <ToastPrimitiveTitle
    className={cn('font-semibold text-sm sm:text-base', className)}
    ref={ref}
    {...props}
  />
))
ToastTitle.displayName = ToastPrimitiveTitle.displayName

const ToastDescription = React.forwardRef<
  React.ElementRef<typeof ToastPrimitiveDescription>,
  React.ComponentPropsWithoutRef<typeof ToastPrimitiveDescription>
>(({ className, ...props }, ref) => (
  <ToastPrimitiveDescription
    className={cn('text-muted-foreground text-xs sm:text-sm', className)}
    ref={ref}
    {...props}
  />
))
ToastDescription.displayName = ToastPrimitiveDescription.displayName

const ToastClose = React.forwardRef<
  React.ElementRef<typeof ToastPrimitiveClose>,
  React.ComponentPropsWithoutRef<typeof ToastPrimitiveClose>
>(({ className, children, ...props }, ref) => (
  <ToastPrimitiveClose
    className={cn(
      'absolute top-3 right-3 rounded-full p-1 text-current opacity-70 transition hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring',
      className
    )}
    ref={ref}
    {...props}
  >
    {children ?? <span className="sr-only">Close</span>}
  </ToastPrimitiveClose>
))
ToastClose.displayName = ToastPrimitiveClose.displayName

const ToastAction = React.forwardRef<
  React.ElementRef<typeof ToastPrimitiveAction>,
  React.ComponentPropsWithoutRef<typeof ToastPrimitiveAction>
>(({ className, ...props }, ref) => (
  <ToastPrimitiveAction
    className={cn(
      'inline-flex shrink-0 items-center justify-center rounded-md border border-border bg-background px-3 py-1 font-medium text-sm transition-colors hover:bg-muted focus:outline-none focus:ring-2 focus:ring-ring',
      className
    )}
    ref={ref}
    {...props}
  />
))
ToastAction.displayName = ToastPrimitiveAction.displayName

export type ToastActionElement = React.ReactElement<typeof ToastAction>

export {
  Toast,
  ToastAction,
  ToastClose,
  ToastDescription,
  ToastProvider,
  ToastTitle,
  ToastViewport,
  toastVariants,
}
