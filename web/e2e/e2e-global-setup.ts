import type { FullConfig } from '@playwright/test'
import { cleanupE2eArtifacts, resetAllMfaState } from './db'

export default async function globalSetup(_config: FullConfig) {
  if (!process.env.DATABASE_URL) {
    console.warn('[e2e] DATABASE_URL not set; skipping database cleanup')
    return
  }

  try {
    await cleanupE2eArtifacts()
    await resetAllMfaState()
  } catch (error) {
    console.warn('[e2e] Failed to prepare database state', error)
  }
}
