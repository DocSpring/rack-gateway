// A YubiKey's Yubico OTP: 44 modhex characters, which the key types followed by Enter (see
// internal/gateway/auth/mfa/totp.go).
const YUBIKEY_OTP_REGEX = /^[cbdefghijklnrtuv]{44}$/

export function normalizeYubiKeyOTP(value: string): string {
  return value.trim().toLowerCase()
}

export function isValidYubiKeyOTP(value: string): boolean {
  return YUBIKEY_OTP_REGEX.test(normalizeYubiKeyOTP(value))
}
