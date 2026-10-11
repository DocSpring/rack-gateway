import { describe, expect, it } from 'vitest'
import {
  decryptColumnValue,
  encryptColumnValue,
  mfaSecretScope,
  parseColumnKey,
} from '../../e2e/column-crypto'

// The E2E helpers write encrypted TOTP seeds straight into the database, so they must match the
// gateway byte for byte. internal/gateway/colcrypt/colcrypt_test.go asserts the same vector.
const VECTOR = {
  key: 'AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=',
  nonce: 'a0a1a2a3a4a5a6a7a8a9aaab',
  plaintext: 'JBSWY3DPEHPK3PXP',
  userId: 42,
  envelope: 'rgwenc:v1:c2fa4cfe:oKGio6SlpqeoqaqrrFovehz4Ru8nLdeYNCqYjj75XvCHi5qUNLQy-T7NX2E',
}

describe('E2E column encryption helper', () => {
  const key = parseColumnKey(VECTOR.key)
  const scope = mfaSecretScope(VECTOR.userId, 'totp')

  it('produces the same envelope as the gateway', () => {
    const envelope = encryptColumnValue(
      key,
      VECTOR.plaintext,
      scope,
      Buffer.from(VECTOR.nonce, 'hex')
    )
    expect(envelope).toBe(VECTOR.envelope)
    expect(decryptColumnValue(key, VECTOR.envelope, scope)).toBe(VECTOR.plaintext)
  })

  it('round-trips with a random nonce', () => {
    const first = encryptColumnValue(key, VECTOR.plaintext, scope)
    const second = encryptColumnValue(key, VECTOR.plaintext, scope)
    expect(first).not.toBe(second)
    expect(decryptColumnValue(key, first, scope)).toBe(VECTOR.plaintext)
  })

  it("rejects a value bound to another user's row", () => {
    expect(() => decryptColumnValue(key, VECTOR.envelope, mfaSecretScope(43, 'totp'))).toThrow()
  })

  it('rejects a value encrypted under another key', () => {
    const otherKey = parseColumnKey(Buffer.alloc(32, 0xff).toString('base64'))
    expect(() => decryptColumnValue(otherKey, VECTOR.envelope, scope)).toThrow()
  })

  it('rejects malformed keys', () => {
    expect(() => parseColumnKey('not-a-key')).toThrow()
    expect(() => parseColumnKey(Buffer.alloc(16).toString('base64'))).toThrow()
  })
})
