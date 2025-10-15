import type { FullConfig } from '@playwright/test'
import { cleanupE2eArtifacts, listE2eDatabaseUrls, resetAllMfaState } from './db'

export default async function globalSetup(_config: FullConfig) {
  const databaseUrls = listE2eDatabaseUrls()

  if (databaseUrls.length === 0) {
    console.warn('[e2e] No database URLs detected; skipping database cleanup')
    return
  }

  try {
    // The db.ts helper functions use environment variables to connect
    // For parallel shards, we need to clean each database
    const originalUrl = process.env.E2E_DATABASE_URL
    for (const url of databaseUrls) {
      // Temporarily set env for this database
      process.env.E2E_DATABASE_URL = url

      try {
        await cleanupE2eArtifacts()
        await resetAllMfaState()
      } finally {
        // Restore original after all iterations
      }
    }

    // Restore original env var after all database operations
    if (originalUrl) {
      process.env.E2E_DATABASE_URL = originalUrl
    } else {
      // biome-ignore lint/performance/noDelete: Need to delete env var to prevent "undefined" string
      delete process.env.E2E_DATABASE_URL
    }
  } catch (error) {
    console.warn('[e2e] Failed to prepare database state', error)
  }
}
