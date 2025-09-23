import React from 'react'

import type { ToastActionElement } from '@/components/ui/toast'

const TOAST_LIMIT = 20
const TOAST_REMOVE_DELAY = 1_000_000

type ToastVariant = 'default' | 'success' | 'error' | 'warning' | 'info'

type ToastOptions = {
  open?: boolean
  duration?: number
  onOpenChange?: (open: boolean) => void
  title?: React.ReactNode
  description?: React.ReactNode
  action?: ToastActionElement
  variant?: ToastVariant
}

type ToasterToast = ToastOptions & {
  id: string
}

type ToastState = {
  toasts: ToasterToast[]
}

type ToastHandlers = {
  (options: ToastOptions): string
  (title: React.ReactNode, options?: ToastOptions): string
  success: (title: React.ReactNode, options?: ToastOptions) => string
  error: (title: React.ReactNode, options?: ToastOptions) => string
  warning: (title: React.ReactNode, options?: ToastOptions) => string
  info: (title: React.ReactNode, options?: ToastOptions) => string
  dismiss: (toastId?: string) => void
}

type Action =
  | { type: 'ADD_TOAST'; toast: ToasterToast }
  | { type: 'DISMISS_TOAST'; toastId?: ToasterToast['id'] }
  | { type: 'REMOVE_TOAST'; toastId?: ToasterToast['id'] }

const listeners = new Set<(state: ToastState) => void>()
const toastTimeouts = new Map<string, ReturnType<typeof setTimeout>>()

let memoryState: ToastState = { toasts: [] }
let count = 0

const VARIANT_MAP: Record<'success' | 'error' | 'warning' | 'info', ToastVariant> = {
  success: 'success',
  error: 'error',
  warning: 'warning',
  info: 'info',
}

function dispatch(action: Action) {
  memoryState = reducer(memoryState, action)
  for (const listener of listeners) {
    listener(memoryState)
  }
}

function reducer(state: ToastState, action: Action): ToastState {
  switch (action.type) {
    case 'ADD_TOAST':
      return {
        ...state,
        toasts: [action.toast, ...state.toasts].slice(0, TOAST_LIMIT),
      }
    case 'DISMISS_TOAST': {
      const { toastId } = action

      if (toastId) addToRemoveQueue(toastId)
      else for (const toast of state.toasts) addToRemoveQueue(toast.id)

      return {
        ...state,
        toasts: state.toasts.map((toast) =>
          toast.id === toastId || toastId === undefined
            ? {
                ...toast,
                open: false,
              }
            : toast
        ),
      }
    }
    case 'REMOVE_TOAST':
      if (action.toastId === undefined) {
        return { ...state, toasts: [] }
      }
      return {
        ...state,
        toasts: state.toasts.filter((toast) => toast.id !== action.toastId),
      }
    default:
      return state
  }
}

function addToRemoveQueue(toastId: string) {
  if (toastTimeouts.has(toastId)) return

  const timeout = setTimeout(() => {
    toastTimeouts.delete(toastId)
    dispatch({ type: 'REMOVE_TOAST', toastId })
  }, TOAST_REMOVE_DELAY)

  toastTimeouts.set(toastId, timeout)
}

function addToast(options: ToastOptions): string {
  const id = `${Date.now()}_${++count}`
  dispatch({
    type: 'ADD_TOAST',
    toast: {
      ...options,
      id,
      open: true,
    },
  })
  return id
}

function dismiss(toastId?: string) {
  dispatch({ type: 'DISMISS_TOAST', toastId })
}

function useToast(): ToastState & { toast: ToastHandlers; dismiss: typeof dismiss } {
  const [state, setState] = React.useState<ToastState>(memoryState)

  React.useEffect(() => {
    listeners.add(setState)
    return () => {
      listeners.delete(setState)
    }
  }, [])

  return {
    ...state,
    toast: toastController,
    dismiss,
  }
}

function isToastContent(value: unknown): value is string | React.ReactElement {
  return typeof value === 'string' || React.isValidElement(value)
}

function normalizeOptions(
  contentOrOptions: React.ReactNode | ToastOptions,
  options?: ToastOptions
): ToastOptions {
  if (isToastContent(contentOrOptions)) {
    const base = options ?? {}
    return {
      ...base,
      title: contentOrOptions as React.ReactNode,
    }
  }

  if (
    contentOrOptions &&
    typeof contentOrOptions === 'object' &&
    !Array.isArray(contentOrOptions)
  ) {
    return contentOrOptions as ToastOptions
  }

  return options ?? {}
}

function createTypedToast(variant: ToastVariant) {
  return (contentOrOptions: React.ReactNode | ToastOptions, options?: ToastOptions) =>
    addToast({
      ...normalizeOptions(contentOrOptions, options),
      variant,
    })
}

function toastFn(contentOrOptions: React.ReactNode | ToastOptions, options?: ToastOptions) {
  return addToast(normalizeOptions(contentOrOptions, options))
}

const toastController = Object.assign(toastFn as ToastHandlers, {
  success: createTypedToast(VARIANT_MAP.success),
  error: createTypedToast(VARIANT_MAP.error),
  warning: createTypedToast(VARIANT_MAP.warning),
  info: createTypedToast(VARIANT_MAP.info),
  dismiss,
})

export { dismiss, toastController as toast, useToast }
export type { ToasterToast }
