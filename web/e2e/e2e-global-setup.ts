import type { FullConfig } from '@playwright/test'
import { Client } from 'pg'

async function cleanupE2eResources(client: Client) {
  // Tokens created via UI tests
  await client.query("DELETE FROM api_tokens WHERE name LIKE 'E2E Web%';")
  // Audit logs that reference those tokens/users to keep tables tidy
  await client.query("DELETE FROM audit_logs WHERE resource LIKE 'E2E Web%';")
  await client.query("DELETE FROM audit_logs WHERE details LIKE '%E2E Web%';")
  // Users created via UI tests
  await client.query("DELETE FROM users WHERE name LIKE 'E2E Web%';")
}

export default async function globalSetup(_config: FullConfig) {
  const connectionString = process.env.DATABASE_URL
  if (!connectionString) {
    console.warn('[e2e] DATABASE_URL not set; skipping database cleanup')
    return
  }

  const sslMode = (process.env.DATABASE_SSL || '').toLowerCase()
  const client = new Client({
    connectionString,
    ssl: sslMode === 'disable' || sslMode === 'false' ? undefined : { rejectUnauthorized: false },
  })

  try {
    await client.connect()
    await cleanupE2eResources(client)
  } catch (error) {
    console.warn('[e2e] Failed to cleanup database', error)
  } finally {
    await client.end().catch(() => {})
  }
}
