import { describe, expect, it } from 'vitest'
import { isValidBackupCode, normalizeBackupCode } from './backup-code'

describe('backup codes', () => {
  it('normalizes case, spaces and dashes', () => {
    expect(normalizeBackupCode('abcdef-012345')).toBe('ABCDEF012345')
    expect(normalizeBackupCode(' ABC DEF 012 345 ')).toBe('ABCDEF012345')
  })

  it('accepts only 12 hex characters', () => {
    expect(isValidBackupCode('abcdef-012345')).toBe(true)
    expect(isValidBackupCode('123456')).toBe(false)
    expect(isValidBackupCode('ABCDEF01234G')).toBe(false)
    expect(isValidBackupCode('ABCDEF0123456')).toBe(false)
  })
})
