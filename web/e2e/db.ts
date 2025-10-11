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
    process.env.RGW_DATABASE_URL,
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
  // console.log(`[withDbClient] Using connection string: ${connectionString}`)

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
    await client.query(
      'UPDATE users SET mfa_enrolled = FALSE, mfa_enforced_at = NULL, preferred_mfa_method = NULL;'
    )
    // Delete any MFA-related global settings (app_name = NULL)
    await client.query(
      `DELETE FROM settings
        WHERE app_name IS NULL
          AND key IN ('mfa_require_all_users', 'mfa_trusted_device_ttl_days', 'mfa_step_up_window_minutes');`
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
      'UPDATE users SET mfa_enrolled = FALSE, mfa_enforced_at = NULL, preferred_mfa_method = NULL WHERE email = $1;',
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

export async function clearMfaAttempts() {
  await withDbClient(async (client) => {
    await client.query('DELETE FROM mfa_totp_attempts;')
    await client.query('DELETE FROM mfa_webauthn_attempts;')
    await client.query('DELETE FROM used_totp_steps;')
  })
}

export async function getUserMfaSecret(email: string): Promise<string | null> {
  return await withDbClient(async (client) => {
    const result = await client.query(
      `SELECT secret FROM mfa_methods
       WHERE user_id = (SELECT id FROM users WHERE email = $1)
       AND type = 'totp'
       AND confirmed_at IS NOT NULL
       ORDER BY created_at DESC
       LIMIT 1;`,
      [email]
    )
    return result.rows[0]?.secret || null
  })
}

export async function clearAllGlobalSettings() {
  await withDbClient(async (client) => {
    // Clear all global settings except the ones needed for E2E tests
    await client.query(
      `DELETE FROM settings
       WHERE app_name IS NULL
       AND key NOT IN ('mfa', 'approved_commands', 'service_image_patterns')`
    )
  })
}

export async function setupBothMfaMethodsForUser(email: string) {
  await withDbClient(async (client) => {
    // First reset any existing MFA state
    await client.query(
      'DELETE FROM mfa_methods WHERE user_id = (SELECT id FROM users WHERE email = $1);',
      [email]
    )
    await client.query(
      'DELETE FROM mfa_backup_codes WHERE user_id = (SELECT id FROM users WHERE email = $1);',
      [email]
    )

    // Insert TOTP method (using real secret from dev database)
    await client.query(
      `INSERT INTO mfa_methods (user_id, type, secret, label, created_at, confirmed_at, last_used_at)
       VALUES (
         (SELECT id FROM users WHERE email = $1),
         'totp',
         'K745D33R6A3NCWP5C3NYDQMBQF5ZFFHU',
         'Authenticator App',
         NOW(),
         NOW(),
         NULL
       );`,
      [email]
    )

    // Insert WebAuthn method (using real Yubikey credential from dev database)
    await client.query(
      `INSERT INTO mfa_methods (user_id, type, credential_id, public_key, label, created_at, confirmed_at, last_used_at)
       VALUES (
         (SELECT id FROM users WHERE email = $1),
         'webauthn',
         decode('b08fa0de532b22a9537c63309329c7cad86d8f4c72dc3f05f94e9f7d2d8acfe9e5a9d9a88794e2a8f4bd3de69371ed17ae2e0229e4ed9f37896381a322a1016f', 'hex'),
         decode('a5010203262001215820debc463a75d894212b5b9717110b10c330217872751093370c2cf78a3fd25d7b2258208f4e7ecfdc836e2984883b666666b54306151e9f785b0935e9415d9cd3baeea6', 'hex'),
         'Security Key',
         NOW(),
         NOW(),
         NULL
       );`,
      [email]
    )

    // Generate backup codes
    const backupCodes = [
      'BACKUP01',
      'BACKUP02',
      'BACKUP03',
      'BACKUP04',
      'BACKUP05',
      'BACKUP06',
      'BACKUP07',
      'BACKUP08',
      'BACKUP09',
      'BACKUP10',
    ]
    for (const code of backupCodes) {
      await client.query(
        `INSERT INTO mfa_backup_codes (user_id, code_hash, used_at, created_at)
         VALUES (
           (SELECT id FROM users WHERE email = $1),
           $2,
           NULL,
           NOW()
         );`,
        [email, code]
      )
    }

    // Mark user as MFA enrolled and set default preferred method to TOTP
    await client.query(
      'UPDATE users SET mfa_enrolled = TRUE, preferred_mfa_method = $2 WHERE email = $1;',
      [email, 'totp']
    )
  })
}
