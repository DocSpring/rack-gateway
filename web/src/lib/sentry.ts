import { browserTracingIntegration, init as sentryInit } from '@sentry/react'

let sentryEnabled = false

function parseSampleRate(raw: string | undefined, fallback: number): number {
  const value = normalizeEnv(raw)
  if (!value) return fallback
  const parsed = Number.parseFloat(value)
  if (Number.isNaN(parsed) || parsed < 0 || parsed > 1) {
    return fallback
  }
  return parsed
}

function normalizeEnv(value: string | undefined): string | undefined {
  if (!value) return
  const trimmed = value.trim()
  return trimmed === '' ? undefined : trimmed
}

function readMetaContent(name: string): string | undefined {
  if (typeof document === 'undefined') return
  const meta = document.querySelector(`meta[name="${name}"]`) as HTMLMetaElement | null
  const content = meta?.getAttribute('content')?.trim()
  return content && content !== '' ? content : undefined
}

export function initSentry(): boolean {
  if (sentryEnabled) return true

  const dsn = readMetaContent('rgw-sentry-dsn')
  if (!dsn) {
    return false
  }

  const environment =
    readMetaContent('rgw-sentry-environment') ??
    (import.meta.env.PROD ? 'production' : 'development')

  const release =
    readMetaContent('rgw-sentry-release') ??
    normalizeEnv(import.meta.env.VITE_RELEASE as string | undefined)

  const tracesSampleRate = parseSampleRate(readMetaContent('rgw-sentry-traces-sample-rate'), 0)

  const integrations = [browserTracingIntegration()]

  sentryInit({
    dsn,
    environment,
    release,
    integrations,
    tracesSampleRate,
    // Sentry collects all of these unless told otherwise. Requests carry session cookies, CSRF and MFA headers,
    // OAuth codes in query strings, and secrets in bodies, so none of them may leave the browser.
    dataCollection: {
      userInfo: false,
      cookies: false,
      httpHeaders: false,
      httpBodies: [],
      urlQueryParams: false,
      stackFrameVariables: false,
    },
  })

  sentryEnabled = true
  return true
}

export function __resetSentryForTests() {
  sentryEnabled = false
}
