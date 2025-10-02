import { Check, CircleAlert, Info, TriangleAlert } from 'lucide-react'
import { createElement } from 'react'
import hotToast from 'react-hot-toast'

type ToastOptions = {
  description?: string
  duration?: number
}

type ToastHandlers = {
  (title: string, options?: ToastOptions): string
  success: (title: string, options?: ToastOptions) => string
  error: (title: string, options?: ToastOptions) => string
  warning: (title: string, options?: ToastOptions) => string
  info: (title: string, options?: ToastOptions) => string
  dismiss: (toastId?: string) => void
}

function dismiss(toastId?: string) {
  hotToast.dismiss(toastId)
}

function toastFn(title: string, options?: ToastOptions) {
  return hotToast(title, options)
}

const toast = Object.assign(toastFn as ToastHandlers, {
  success: (title: string, options?: ToastOptions) =>
    hotToast.success(title, { ...options, icon: createElement(Check, { className: 'h-6 w-6' }) }),
  error: (title: string, options?: ToastOptions) =>
    hotToast.error(title, {
      ...options,
      icon: createElement(CircleAlert, { className: 'h-6 w-6' }),
    }),
  warning: (title: string, options?: ToastOptions) =>
    hotToast(title, { ...options, icon: createElement(TriangleAlert, { className: 'h-6 w-6' }) }),
  info: (title: string, options?: ToastOptions) =>
    hotToast(title, { ...options, icon: createElement(Info, { className: 'h-6 w-6' }) }),
  dismiss,
})

export { dismiss, toast }
export function useToast() {
  return { toast, dismiss }
}
