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

  const dsn = readMetaContent('cgw-sentry-dsn')
  if (!dsn) {
    return false
  }

  const environment =
    readMetaContent('cgw-sentry-environment') ??
    (import.meta.env.PROD ? 'production' : 'development')

  const release =
    readMetaContent('cgw-sentry-release') ??
    normalizeEnv(import.meta.env.VITE_RELEASE as string | undefined)

  const tracesSampleRate = parseSampleRate(readMetaContent('cgw-sentry-traces-sample-rate'), 0)

  const integrations = [browserTracingIntegration()]

  sentryInit({
    dsn,
    environment,
    release,
    integrations,
    tracesSampleRate,
  })

  sentryEnabled = true
  return true
}

export function isSentryEnabled(): boolean {
  return sentryEnabled
}

export function __resetSentryForTests() {
  sentryEnabled = false
}
