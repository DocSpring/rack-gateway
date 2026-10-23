// A YubiKey's Yubico OTP, which the key types followed by Enter: its public ID (1 to 16 modhex
// characters, usually 12) then 32 encrypted ones (see yubiOTPPublicID in
// internal/gateway/auth/mfa/yubiotp.go).
const YUBIKEY_OTP_REGEX = /^[cbdefghijklnrtuv]{33,48}$/

export function normalizeYubiKeyOTP(value: string): string {
  return value.trim().toLowerCase()
}

export function isValidYubiKeyOTP(value: string): boolean {
  return YUBIKEY_OTP_REGEX.test(normalizeYubiKeyOTP(value))
}
