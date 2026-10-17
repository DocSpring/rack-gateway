import { createCipheriv, createDecipheriv, createHmac, randomBytes } from 'node:crypto'

/**
 * The gateway's column encryption (internal/gateway/colcrypt), for E2E helpers that read or
 * write encrypted columns such as mfa_methods.secret directly:
 *
 *   rgwenc:v1:<kid>:<base64url(nonce || ciphertext || tag)>
 *
 * AES-256-GCM with a 96-bit nonce. kid is the first 4 bytes of
 * HMAC-SHA256(key, "rack-gateway column key id") in hex, and the additional data
 * "rgwenc:v1:<kid>|<table.column>|<row>" binds a value to its column and row. A shared test
 * vector (src/test/column-crypto.test.ts and colcrypt_test.go) keeps this in step with Go.
 */

const VERSION_PREFIX = 'rgwenc:v1:'
const NONCE_BYTES = 12
const TAG_BYTES = 16
const KEY_BYTES = 32

interface ColumnScope {
  column: string
  row: string
}

/**
 * The scope of mfa_methods.secret: the method's user and type. A pending WebAuthn method has the type of a
 * confirmed one (colcrypt.MFASecretScope).
 */
export function mfaSecretScope(userId: number | string, methodType: string): ColumnScope {
  const type = methodType.startsWith('webauthn') ? 'webauthn' : methodType
  return { column: 'mfa_methods.secret', row: `user:${userId}|type:${type}` }
}

export function parseColumnKey(encoded: string): Buffer {
  const trimmed = encoded.trim()
  const key = Buffer.from(trimmed, 'base64')
  if (key.length !== KEY_BYTES || key.toString('base64') !== trimmed) {
    throw new Error('COLUMN_ENCRYPTION_KEY must be standard base64 of exactly 32 bytes')
  }
  return key
}

function columnKeyFromEnv(): Buffer {
  const encoded = process.env.COLUMN_ENCRYPTION_KEY
  if (!encoded) {
    throw new Error('COLUMN_ENCRYPTION_KEY is not set (mise.toml sets the test gateway key)')
  }
  return parseColumnKey(encoded)
}

function keyId(key: Buffer): string {
  return createHmac('sha256', key)
    .update('rack-gateway column key id')
    .digest()
    .subarray(0, 4)
    .toString('hex')
}

function additionalData(kid: string, scope: ColumnScope): Buffer {
  return Buffer.from(`${VERSION_PREFIX}${kid}|${scope.column}|${scope.row}`)
}

export function encryptColumnValue(
  key: Buffer,
  plaintext: string,
  scope: ColumnScope,
  nonce: Buffer = randomBytes(NONCE_BYTES)
): string {
  const kid = keyId(key)
  const cipher = createCipheriv('aes-256-gcm', key, nonce, { authTagLength: TAG_BYTES })
  cipher.setAAD(additionalData(kid, scope))
  const sealed = Buffer.concat([
    cipher.update(plaintext, 'utf8'),
    cipher.final(),
    cipher.getAuthTag(),
  ])
  return `${VERSION_PREFIX}${kid}:${Buffer.concat([nonce, sealed]).toString('base64url')}`
}

export function decryptColumnValue(key: Buffer, envelope: string, scope: ColumnScope): string {
  const kid = keyId(key)
  const prefix = `${VERSION_PREFIX}${kid}:`
  if (!envelope.startsWith(prefix)) {
    throw new Error(`value is not encrypted with this COLUMN_ENCRYPTION_KEY (no ${prefix} prefix)`)
  }
  const payload = Buffer.from(envelope.slice(prefix.length), 'base64url')
  if (payload.length < NONCE_BYTES + TAG_BYTES) {
    throw new Error('encrypted value is too short')
  }
  const nonce = payload.subarray(0, NONCE_BYTES)
  const ciphertext = payload.subarray(NONCE_BYTES, payload.length - TAG_BYTES)
  const decipher = createDecipheriv('aes-256-gcm', key, nonce, { authTagLength: TAG_BYTES })
  decipher.setAAD(additionalData(kid, scope))
  decipher.setAuthTag(payload.subarray(payload.length - TAG_BYTES))
  return Buffer.concat([decipher.update(ciphertext), decipher.final()]).toString('utf8')
}

/** Encrypts a TOTP seed for a user's mfa_methods row, as the gateway stores it. */
export function encryptMfaSecret(userId: number | string, secret: string): string {
  return encryptColumnValue(columnKeyFromEnv(), secret, mfaSecretScope(userId, 'totp'))
}

/** Decrypts the stored mfa_methods.secret of a user's TOTP method. */
export function decryptMfaSecret(userId: number | string, stored: string): string {
  return decryptColumnValue(columnKeyFromEnv(), stored, mfaSecretScope(userId, 'totp'))
}
