import type { FullConfig } from '@playwright/test'
import { cleanupE2eArtifacts, listE2eDatabaseUrls, resetAllMfaState } from './db'

export default async function globalTeardown(_config: FullConfig) {
  const databaseUrls = listE2eDatabaseUrls()
  console.log('[teardown] Database URLs:', databaseUrls)
  if (databaseUrls.length === 0) return

  try {
    const originalUrl = process.env.E2E_DATABASE_URL
    for (const url of databaseUrls) {
      console.log('[teardown] Processing database:', url)
      process.env.E2E_DATABASE_URL = url

      try {
        await resetAllMfaState()
        await cleanupE2eArtifacts()
      } finally {
        // Restore after all iterations
      }
    }
    // Restore original after loop
    if (originalUrl) {
      process.env.E2E_DATABASE_URL = originalUrl
    } else {
      // biome-ignore lint/performance/noDelete: Need to delete env var to prevent "undefined" string
      delete process.env.E2E_DATABASE_URL
    }
  } catch (error) {
    console.warn('[e2e] Failed to reset database after web E2E tests', error)
  }
}
