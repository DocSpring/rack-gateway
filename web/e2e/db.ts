import { randomBytes } from 'node:crypto'
import { Client } from 'pg'

function getFallbackDatabaseUrl(): string {
  const e2eUrl = process.env.E2E_DATABASE_URL

  if (e2eUrl && e2eUrl !== 'undefined' && e2eUrl.trim().length > 0) {
    return e2eUrl.trim()
  }

  return 'postgres://postgres:postgres@127.0.0.1:55432/gateway_test?sslmode=disable'
}

const parseDatabaseUrlList = (value?: string) =>
  value
    ? value
        .split(',')
        .map((entry) => entry.trim())
        .filter((entry) => entry.length > 0)
    : []

export function listE2eDatabaseUrls(): string[] {
  const urls = new Set<string>()
  for (const value of parseDatabaseUrlList(process.env.E2E_DATABASE_URLS)) {
    urls.add(value)
  }

  // Only use E2E-specific database URLs, not the dev database
  const fallbacks = [
    process.env.E2E_DATABASE_URL,
    process.env.TEST_DATABASE_URL,
    process.env.RGW_DATABASE_URL,
    process.env.GATEWAY_DATABASE_URL,
    getFallbackDatabaseUrl(),
  ]

  for (const fallback of fallbacks) {
    if (fallback && fallback.trim().length > 0 && fallback !== 'undefined') {
      urls.add(fallback.trim())
    }
  }

  // Filter out dev database and "undefined" string - only return test databases
  return Array.from(urls).filter((url) => !url.includes('/gateway_dev') && url !== 'undefined')
}

function resolveConnectionString() {
  const candidates = [
    process.env.E2E_DATABASE_URL,
    process.env.TEST_DATABASE_URL,
    process.env.RGW_DATABASE_URL,
    process.env.GATEWAY_DATABASE_URL,
    process.env.DATABASE_URL,
  ]
    .map((value) => value?.trim())
    .filter((value): value is string => Boolean(value && value.length > 0 && value !== 'undefined'))

  const defaultPlaceholder = /postgres:\/\/[^@]*@?localhost:5432\/postgres(?:\?|$)/i

  if (candidates.length === 0) {
    return getFallbackDatabaseUrl()
  }

  const selected = candidates[0]
  if (defaultPlaceholder.test(selected)) {
    return getFallbackDatabaseUrl()
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
    const { rows } = await client.query<{ fn: string | null }>(
      `SELECT to_regprocedure('audit.reset_for_tests()') AS fn`
    )
    const resetFnExists = rows[0]?.fn != null

    if (resetFnExists) {
      await client.query("SET audit.allow_reset = 'on';")
      try {
        await client.query('SELECT audit.reset_for_tests();')
      } finally {
        await client.query('RESET audit.allow_reset;')
      }
    } else {
      await client.query("SET session_replication_role = 'replica';")
      try {
        await client.query('TRUNCATE TABLE audit.audit_event_aggregated RESTART IDENTITY CASCADE;')
        await client.query('TRUNCATE TABLE audit.audit_event CASCADE;')
      } finally {
        await client.query("SET session_replication_role = 'origin';")
      }
    }
    await client.query('ALTER SEQUENCE audit.audit_event_chain_index_seq RESTART WITH 0;')
    await client.query("DELETE FROM api_tokens WHERE name LIKE 'E2E Web%';")
    await client.query("DELETE FROM users WHERE name LIKE 'E2E Web%';")
  })
}

export async function ensureAdminUser(): Promise<void> {
  await withDbClient(async (client) => {
    await client.query(
      `
        INSERT INTO users (email, name, roles)
        VALUES ($1, $2, $3)
        ON CONFLICT (email) DO UPDATE
        SET name = EXCLUDED.name,
            roles = EXCLUDED.roles,
            updated_at = NOW()
      `,
      ['admin@example.com', 'Admin User', '["admin"]']
    )
  })
}

type AggregatedAuditSeed = {
  firstEventId: number
  lastEventId: number
  eventCount: number
  userEmail: string
  userName: string
  actionType: string
  action: string
  command: string
  resource: string
  resourceType: string
  status: string
  rbacDecision: string
  httpStatus: number
  minResponseTimeMs: number
  maxResponseTimeMs: number
  avgResponseTimeMs: number
  ipAddress: string
  userAgent: string
  details: string
}

export async function seedAggregatedAuditLog(
  overrides: Partial<AggregatedAuditSeed> = {}
): Promise<void> {
  const now = new Date()
  const defaults: AggregatedAuditSeed = {
    firstEventId: 1,
    lastEventId: 3,
    eventCount: 3,
    userEmail: 'admin@example.com',
    userName: 'Admin User',
    actionType: 'convox',
    action: 'app.list',
    command: '',
    resource: 'all',
    resourceType: 'app',
    status: 'success',
    rbacDecision: 'allow',
    httpStatus: 200,
    minResponseTimeMs: 8,
    maxResponseTimeMs: 12,
    avgResponseTimeMs: 10,
    ipAddress: '172.217.167.113',
    userAgent: 'Playwright CLI',
    details: '{}',
  }

  const seed: AggregatedAuditSeed = { ...defaults, ...overrides }
  const firstHash = randomBytes(32)
  const lastHash = randomBytes(32)

  await withDbClient(async (client) => {
    await client.query(
      `
        INSERT INTO audit.audit_event_aggregated (
          first_event_id,
          last_event_id,
          first_seen,
          last_seen,
          first_hash,
          last_hash,
          event_count,
          min_response_time_ms,
          max_response_time_ms,
          avg_response_time_ms,
          user_email,
          user_name,
          api_token_id,
          api_token_name,
          action_type,
          action,
          command,
          resource,
          resource_type,
          details,
          ip_address,
          user_agent,
          status,
          rbac_decision,
          http_status,
          deploy_approval_request_id
        )
        VALUES (
          $1,
          $2,
          $3,
          $4,
          $5,
          $6,
          $7,
          $8,
          $9,
          $10,
          $11,
          $12,
          NULL,
          NULL,
          $13,
          $14,
          $15,
          $16,
          $17,
          $18,
          $19,
          $20,
          $21,
          $22,
          $23,
          NULL
        )
      `,
      [
        seed.firstEventId,
        seed.lastEventId,
        now,
        now,
        firstHash,
        lastHash,
        seed.eventCount,
        seed.minResponseTimeMs,
        seed.maxResponseTimeMs,
        seed.avgResponseTimeMs,
        seed.userEmail,
        seed.userName,
        seed.actionType,
        seed.action,
        seed.command,
        seed.resource,
        seed.resourceType,
        seed.details,
        seed.ipAddress,
        seed.userAgent,
        seed.status,
        seed.rbacDecision,
        seed.httpStatus,
      ]
    )
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
              mfa_verified_at = COALESCE(mfa_verified_at, NOW())
        WHERE recent_step_up_at IS NOT NULL OR mfa_verified_at IS NULL;`
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
    await client.query('TRUNCATE TABLE mfa_attempts RESTART IDENTITY CASCADE;')
    await client.query('TRUNCATE TABLE used_totp_steps RESTART IDENTITY CASCADE;')
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

export async function getPendingTotpSecret(email: string): Promise<string | null> {
  return await withDbClient(async (client) => {
    const result = await client.query(
      `SELECT secret FROM mfa_methods
       WHERE user_id = (SELECT id FROM users WHERE email = $1)
         AND type = 'totp'
         AND confirmed_at IS NULL
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

export async function getUserMfaEnrolled(email: string): Promise<boolean> {
  return await withDbClient(async (client) => {
    const result = await client.query('SELECT mfa_enrolled FROM users WHERE email = $1;', [email])
    return result.rows[0]?.mfa_enrolled ?? false
  })
}

export async function setupBothMfaMethodsForUser(email: string) {
  await withDbClient(async (client) => {
    const existingUser = await client.query('SELECT id FROM users WHERE email = $1;', [email])
    let userId: number | null = existingUser.rows[0]?.id ?? null

    if (!userId) {
      const inserted = await client.query(
        `INSERT INTO users (email, name, roles)
         VALUES ($1, $2, $3)
         ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name, roles = EXCLUDED.roles
         RETURNING id;`,
        [email, 'Admin User', '["admin"]']
      )
      userId = inserted.rows[0]?.id ?? null
    }

    if (!userId) {
      throw new Error(`Failed to ensure user exists for ${email}`)
    }

    // First reset any existing MFA state
    await client.query('DELETE FROM mfa_methods WHERE user_id = $1;', [userId])
    await client.query('DELETE FROM mfa_backup_codes WHERE user_id = $1;', [userId])

    // Insert TOTP method (using real secret from dev database)
    await client.query(
      `INSERT INTO mfa_methods (user_id, type, secret, label, created_at, confirmed_at, last_used_at)
       VALUES (
         $1,
         'totp',
         'K745D33R6A3NCWP5C3NYDQMBQF5ZFFHU',
         'Authenticator App',
         NOW(),
         NOW(),
         NULL
       );`,
      [userId]
    )

    // Insert WebAuthn method (using real Yubikey credential from dev database)
    await client.query(
      `INSERT INTO mfa_methods (user_id, type, credential_id, public_key, label, created_at, confirmed_at, last_used_at)
       VALUES (
         $1,
         'webauthn',
         decode('b08fa0de532b22a9537c63309329c7cad86d8f4c72dc3f05f94e9f7d2d8acfe9e5a9d9a88794e2a8f4bd3de69371ed17ae2e0229e4ed9f37896381a322a1016f', 'hex'),
         decode('a5010203262001215820debc463a75d894212b5b9717110b10c330217872751093370c2cf78a3fd25d7b2258208f4e7ecfdc836e2984883b666666b54306151e9f785b0935e9415d9cd3baeea6', 'hex'),
         'Security Key',
         NOW(),
         NOW(),
         NULL
       );`,
      [userId]
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
           $1,
           $2,
           NULL,
           NOW()
         );`,
        [userId, code]
      )
    }

    // Mark user as MFA enrolled and set default preferred method to TOTP
    await client.query(
      'UPDATE users SET mfa_enrolled = TRUE, preferred_mfa_method = $2 WHERE email = $1;',
      [email, 'totp']
    )
  })
}
