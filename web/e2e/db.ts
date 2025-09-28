import { Client } from 'pg'

const FALLBACK_DATABASE_URL = (
  process.env.E2E_DATABASE_URL ||
  'postgres://postgres:postgres@127.0.0.1:55432/gateway_test?sslmode=disable'
).trim()

function resolveConnectionString() {
  const candidates = [
    process.env.E2E_DATABASE_URL,
    process.env.TEST_DATABASE_URL,
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
    await client.query('TRUNCATE TABLE trusted_devices RESTART IDENTITY CASCADE;')
    await client.query('TRUNCATE TABLE mfa_backup_codes RESTART IDENTITY CASCADE;')
    await client.query('TRUNCATE TABLE mfa_methods RESTART IDENTITY CASCADE;')
    await client.query(
      `UPDATE user_sessions
          SET trusted_device_id = NULL,
              mfa_verified_at = NULL,
              recent_step_up_at = NULL;`
    )
    await client.query('UPDATE users SET mfa_enrolled = FALSE, mfa_enforced_at = NULL;')
    await client.query(
      `UPDATE settings
          SET value = jsonb_set(value, '{require_all_users}', 'false'::jsonb, true),
              updated_at = NOW()
        WHERE key = 'mfa';`
    )
    await client.query(
      `INSERT INTO settings (key, value, updated_at)
       VALUES ('mfa', jsonb_build_object('require_all_users', false), NOW())
       ON CONFLICT (key) DO UPDATE
         SET value = jsonb_set(settings.value, '{require_all_users}', 'false'::jsonb, true),
             updated_at = NOW();`
    )
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
