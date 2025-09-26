import type { FullConfig } from '@playwright/test'
import { cleanupE2eArtifacts, resetAllMfaState } from './db'

export default async function globalTeardown(_config: FullConfig) {
  if (!process.env.DATABASE_URL) {
    return
  }

  try {
    await resetAllMfaState()
    await cleanupE2eArtifacts()
  } catch (error) {
    console.warn('[e2e] Failed to reset database after web E2E tests', error)
  }
}
