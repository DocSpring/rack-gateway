import { Client } from 'pg'

const FALLBACK_DATABASE_URL = (
  process.env.E2E_DATABASE_URL ||
  'postgres://postgres:postgres@127.0.0.1:55432/gateway?sslmode=disable'
).trim()

function resolveConnectionString() {
  const candidates = [
    process.env.DATABASE_URL,
    process.env.CGW_DATABASE_URL,
    process.env.GATEWAY_DATABASE_URL,
  ]
    .map((value) => value?.trim())
    .filter((value): value is string => Boolean(value && value.length > 0))

  const defaultPlaceholder = /postgres:\/\/[^@]*@?localhost:5432\/postgres(?:\?|$)/i

  if (candidates.length === 0) {
    return FALLBACK_DATABASE_URL
  }

  const selected = candidates[0]
  if (defaultPlaceholder.test(selected)) {
    return FALLBACK_DATABASE_URL
  }

  return selected
}

function resolveSslConfig() {
  const sslMode = (process.env.DATABASE_SSL || '').toLowerCase()
  if (sslMode === 'disable' || sslMode === 'false') {
    return
  }
  return { rejectUnauthorized: false }
}

async function withDbClient<T>(handler: (client: Client) => Promise<T>): Promise<T> {
  const connectionString = resolveConnectionString()

  const client = new Client({
    connectionString,
    ssl: resolveSslConfig(),
  })

  try {
    await client.connect()
    return await handler(client)
  } finally {
    await client.end().catch(() => {})
  }
}

export async function cleanupE2eArtifacts() {
  await withDbClient(async (client) => {
    await client.query("DELETE FROM api_tokens WHERE name LIKE 'E2E Web%';")
    await client.query("DELETE FROM audit_logs WHERE resource LIKE 'E2E Web%';")
    await client.query("DELETE FROM audit_logs WHERE details LIKE '%E2E Web%';")
    await client.query("DELETE FROM users WHERE name LIKE 'E2E Web%';")
  })
}

export async function resetAllMfaState() {
  await withDbClient(async (client) => {
    await client.query(
      `UPDATE user_sessions
          SET trusted_device_id = NULL,
              mfa_verified_at = NULL,
              recent_step_up_at = NULL;`
    )
    await client.query('DELETE FROM trusted_devices;')
    await client.query('DELETE FROM mfa_backup_codes;')
    await client.query('DELETE FROM mfa_methods;')
    await client.query('UPDATE users SET mfa_enrolled = FALSE, mfa_enforced_at = NULL;')
  })
}

export async function expireStepUpForAllSessions() {
  await withDbClient(async (client) => {
    await client.query(
      `UPDATE user_sessions
          SET recent_step_up_at = NULL,
              mfa_verified_at = NULL
        WHERE recent_step_up_at IS NOT NULL OR mfa_verified_at IS NOT NULL;`
    )
  })
}

export async function resetMfaForUser(email: string) {
  await withDbClient(async (client) => {
    await client.query(
      'DELETE FROM mfa_methods WHERE user_id = (SELECT id FROM users WHERE email = $1);',
      [email]
    )
    await client.query(
      'DELETE FROM mfa_backup_codes WHERE user_id = (SELECT id FROM users WHERE email = $1);',
      [email]
    )
    await client.query(
      'DELETE FROM trusted_devices WHERE user_id = (SELECT id FROM users WHERE email = $1);',
      [email]
    )
    await client.query(
      'UPDATE users SET mfa_enrolled = FALSE, mfa_enforced_at = NULL WHERE email = $1;',
      [email]
    )
    await client.query(
      'UPDATE user_sessions SET trusted_device_id = NULL, mfa_verified_at = NULL, recent_step_up_at = NULL WHERE user_id = (SELECT id FROM users WHERE email = $1);',
      [email]
    )
  })
}

export async function enforceMfaForUser(email: string) {
  await withDbClient(async (client) => {
    await client.query('UPDATE users SET mfa_enforced_at = NOW() WHERE email = $1;', [email])
  })
}
