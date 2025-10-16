import {
  type UseMutationOptions,
  useMutation as useReactQueryMutation,
} from '@tanstack/react-query'

import { toast } from '@/components/ui/use-toast'
import { isMFAError } from '@/contexts/step-up-context'
import { getErrorMessage } from '@/lib/error-utils'

type CustomMutationOptions<TData, TError, TVariables, TContext> = UseMutationOptions<
  TData,
  TError,
  TVariables,
  TContext
> & {
  /**
   * Whether to show an error toast automatically.
   * Default: true (show error toast unless it's an MFA error)
   * Set to false when you need custom error handling that doesn't show a toast.
   */
  showToastError?: boolean
}

/**
 * Custom useMutation wrapper that automatically handles error toasts.
 *
 * Features:
 * - ALWAYS shows an error toast by default (unless preventToastError is true)
 * - Automatically suppresses MFA errors (handled by step-up dialog with inline errors)
 * - Still calls your custom onError handler if provided
 * - Eliminates duplicate toast.error(getErrorMessage(error)) across the app
 *
 * Usage:
 * ```tsx
 * const mutation = useMutation({
 *   mutationFn: async () => { ... },
 *   onSuccess: () => { ... },
 *   // onError with toast.error is now automatic!
 * })
 * ```
 *
 * Custom error handling:
 * ```tsx
 * const mutation = useMutation({
 *   mutationFn: async () => { ... },
 *   onError: (error) => {
 *     // Default toast already shown (unless it's an MFA error)
 *     // Do custom error handling here (state updates, etc.)
 *     setErrorState(error)
 *   }
 * })
 * ```
 *
 * Disable toast for special cases:
 * ```tsx
 * const mutation = useMutation({
 *   mutationFn: async () => { ... },
 *   showToastError: false,
 *   onError: (error) => {
 *     // Handle error completely custom way
 *   }
 * })
 * ```
 */
export function useMutation<TData = unknown, TError = Error, TVariables = void, TContext = unknown>(
  options: CustomMutationOptions<TData, TError, TVariables, TContext>
) {
  const { showToastError = true, ...restOptions } = options
  const originalOnError = options.onError

  return useReactQueryMutation<TData, TError, TVariables, TContext>({
    ...restOptions,
    onError: (...args) => {
      const [error] = args
      // Show toast error unless:
      // 1. showToastError is false
      // 2. It's an MFA error (handled by step-up dialog with inline error)
      if (showToastError && !isMFAError(error)) {
        toast.error(getErrorMessage(error))
      }

      // Still call the custom onError handler if provided
      if (originalOnError) {
        originalOnError(...args)
      }
    },
  })
}
