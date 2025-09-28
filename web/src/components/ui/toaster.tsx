import { Check, CircleAlert, Info, TriangleAlert, X } from 'lucide-react'
import React from 'react'

import {
  Toast,
  ToastAction,
  ToastClose,
  ToastDescription,
  ToastProvider,
  ToastTitle,
  ToastViewport,
} from '@/components/ui/toast'

import { useToast } from '@/components/ui/use-toast'

type ToastVariant = 'default' | 'success' | 'error' | 'warning' | 'info'
type ToastIconKey = Exclude<ToastVariant, 'default'>

const variantIcons: Partial<Record<ToastIconKey, React.ReactNode>> = {
  success: <Check aria-hidden="true" className="h-5 w-5" />,
  error: <CircleAlert aria-hidden="true" className="h-5 w-5" />,
  warning: <TriangleAlert aria-hidden="true" className="h-5 w-5" />,
  info: <Info aria-hidden="true" className="h-5 w-5" />,
}

const CloseIcon = <X aria-hidden="true" className="h-4 w-4" />

function ToastViewportContainer() {
  const { toasts, dismiss } = useToast()

  return (
    <ToastProvider swipeDirection="right">
      {toasts.map(({ id, title, description, action, ...rest }) => {
        const variant = rest.variant ?? 'default'
        const Icon = variant !== 'default' ? variantIcons[variant as ToastIconKey] : undefined

        return (
          <Toast
            key={id}
            {...rest}
            onOpenChange={(open) => {
              if (!open) dismiss(id)
            }}
          >
            <div className="flex w-full items-start gap-3">
              {Icon ? <div className="mt-1 text-current">{Icon}</div> : null}
              <div className="grid flex-1 gap-1">
                {title ? <ToastTitle>{title}</ToastTitle> : null}
                {description ? <ToastDescription>{description}</ToastDescription> : null}
              </div>
              <ToastClose className="text-current">
                {CloseIcon}
                <span className="sr-only">Close</span>
              </ToastClose>
            </div>
            {action ? (
              <div className="mt-2 flex justify-end">
                {React.isValidElement(action) ? (
                  action
                ) : (
                  <ToastAction altText="Toast action">{action}</ToastAction>
                )}
              </div>
            ) : null}
          </Toast>
        )
      })}
      <ToastViewport />
    </ToastProvider>
  )
}

const Toaster: React.FC = () => <ToastViewportContainer />

export { Toaster }
