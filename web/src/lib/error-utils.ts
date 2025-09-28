import { isAxiosError } from 'axios'

export function getErrorMessage(error: unknown, fallback = 'Something went wrong'): string {
  if (typeof error === 'string' && error.trim() !== '') {
    return error
  }
  if (isAxiosError<{ error?: string; message?: string }>(error)) {
    const data = error.response?.data
    const message = data?.message ?? data?.error
    if (typeof message === 'string' && message.trim() !== '') {
      return message
    }
  }
  if (error instanceof Error && error.message.trim() !== '') {
    return error.message
  }
  if (typeof error === 'object' && error !== null) {
    const maybeMessage = (error as { message?: string }).message
    if (typeof maybeMessage === 'string' && maybeMessage.trim() !== '') {
      return maybeMessage
    }
  }
  return fallback
}
