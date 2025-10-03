# WebAuthn CLI Implementation - Continuation

## Current State

✅ **Completed:**

- Removed Yubico OTP from UI (requires cloud validation or self-hosted server)
- Selected `github.com/go-ctap/ctaphid` for FIDO2/CTAP2 client library
- Added system library checks to `scripts/install.sh` for Linux (libudev-dev, libusb-1.0-0-dev)
- Added go-ctap/ctaphid dependency to go.mod (v0.8.1)
- **Gateway API endpoints for WebAuthn assertion:**
  - `POST /.gateway/api/auth/mfa/webauthn/assertion/start` - Start WebAuthn assertion ceremony
  - `POST /.gateway/api/auth/mfa/webauthn/assertion/verify` - Verify WebAuthn assertion response
- **MFA service methods:**
  - `StartWebAuthnAssertion()` - Begin WebAuthn login ceremony
  - `VerifyWebAuthnAssertion()` - Validate assertion response with stored session data
- **DTOs:**
  - `WebAuthnAssertionStartResponse` - Returns challenge and session data
  - `VerifyWebAuthnAssertionRequest` - Accepts assertion response and session data
- **CLI WebAuthn integration:**
  - Created `internal/cli/webauthn` package with `GetAssertion()` and `CheckAvailability()` functions
  - Integrated into CLI login flow at `cmd/rack-gateway/main.go:performMFAVerification()`
  - Calls MFA status endpoint to detect WebAuthn enrollment
  - Attempts WebAuthn first if available, falls back to TOTP if it fails or no device found
  - Properly handles origin, challenge, and assertion flow

📝 **Testing Checklist:**

- [ ] Test WebAuthn login on macOS (no system libs needed)
- [ ] Test WebAuthn login on Linux with system libs installed
- [ ] Test WebAuthn login on Linux without system libs (should fallback to TOTP)
- [ ] Test fallback to TOTP when WebAuthn fails
- [ ] Test with user who has both WebAuthn and TOTP enrolled
- [ ] Test with user who only has TOTP enrolled
- [ ] Test error handling (device disconnected mid-auth, wrong key, etc.)

📝 **Documentation Updates Needed:**

- [ ] Update README with WebAuthn CLI usage
- [ ] Document system requirements for Linux in installation docs
- [ ] Add troubleshooting section for common WebAuthn issues
- [ ] Document fallback behavior (WebAuthn → TOTP)

## Architecture Notes

**Library Choice: `go-ctap/ctaphid`**

- Pure Go FIDO2/CTAP2 protocol implementation
- CGO only for macOS HID transport
- Linux requires: libudev-dev, libusb-1.0-0-dev
- macOS: works out of the box
- Windows: not yet tested

**Flow:**

1. CLI calls MFA status endpoint → knows what methods user has
2. If WebAuthn available → CLI calls assertion start endpoint → gets challenge
3. CLI uses `internal/cli/webauthn.GetAssertion()` → prompts user to touch device
4. Device signs challenge → CLI submits assertion to verify endpoint
5. Gateway validates → returns success/failure
6. On WebAuthn failure → CLI falls back to TOTP prompt

**Alternative Considered:**

- `github.com/keys-pub/go-libfido2` - wraps Yubico's libfido2 C library
- Rejected because it requires libfido2 system library on all platforms
- go-ctap/ctaphid only needs system libs on Linux, macOS works natively

## Related Files

- `internal/cli/webauthn/webauthn.go` - WebAuthn client implementation
- `cmd/rack-gateway/main.go` - CLI login flow (performMFAVerification function)
- `scripts/install.sh` - System dependency checks
- `internal/gateway/handlers/auth.go` - Gateway MFA handlers
- `internal/gateway/auth/mfa/service.go` - MFA service with WebAuthn support
