// Backup codes are 12 uppercase hex characters (see internal/gateway/auth/mfa/service.go).
const BACKUP_CODE_REGEX = /^[0-9A-F]{12}$/
const BACKUP_CODE_SEPARATORS_REGEX = /[\s-]+/g

/** Normalizes a backup code the way people type it: any case, with spaces or dashes. */
export function normalizeBackupCode(value: string): string {
  return value.replace(BACKUP_CODE_SEPARATORS_REGEX, '').toUpperCase()
}

export function isValidBackupCode(value: string): boolean {
  return BACKUP_CODE_REGEX.test(normalizeBackupCode(value))
}
