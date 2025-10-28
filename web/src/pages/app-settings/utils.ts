import { isAxiosError } from 'axios'

type SettingsErrorPayload = {
  error?: string
}

export function extractErrorMessage(error: unknown): string | undefined {
  if (isAxiosError<SettingsErrorPayload>(error)) {
    const payload = error.response?.data
    if (typeof payload === 'string') {
      return payload
    }
    if (payload && typeof payload.error === 'string') {
      return payload.error
    }
  }
  if (error instanceof Error) {
    return error.message
  }
  return
}
