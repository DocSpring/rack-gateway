# WebAuthn CLI Implementation - Continuation

## Current State

✅ **Completed:**

- Removed Yubico OTP from UI (requires cloud validation or self-hosted server)
- Switched CLI WebAuthn implementation to `github.com/keys-pub/go-libfido2`
- Added system library checks to `scripts/install.sh` for Linux (libfido2-dev, libudev-dev, libusb-1.0-0-dev)
- GitHub Actions installs libfido2, enables CGO, and builds/tests the CLI with hardware support enabled by default
- **Gateway API endpoints for WebAuthn assertion:**
  - `POST /api/v1/auth/mfa/webauthn/assertion/start` - Start WebAuthn assertion ceremony
  - `POST /api/v1/auth/mfa/webauthn/assertion/verify` - Verify WebAuthn assertion response
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

**Library Choice: `github.com/keys-pub/go-libfido2`**

- Thin CGO wrapper around Yubico's libfido2 with stable device support
- Linux requires libfido2 development headers (`libfido2-dev`) plus udev/usb libs
- macOS ships libfido2 already; builds succeed with CGO enabled
- The library is a hard dependency: builds fail immediately if the native headers are missing

**Flow:**

1. CLI calls MFA status endpoint → knows what methods user has
2. If WebAuthn available → CLI calls assertion start endpoint → gets challenge
3. CLI uses `internal/cli/webauthn.GetAssertion()` → prompts user to touch device
4. Device signs challenge → CLI submits assertion to verify endpoint
5. Gateway validates → returns success/failure
6. On WebAuthn failure → CLI falls back to TOTP prompt

**Build Requirements:**

- Ensure `CGO_ENABLED=1` (Go defaults to this when a C toolchain is present)
- Install libfido2 + libudev + libusb development headers before running `go build ./cmd/rack-gateway`
- CI and release pipelines install the packages and compile with real hardware support to keep the path green

## Related Files

- `internal/cli/webauthn/webauthn.go` - WebAuthn client implementation
- `cmd/rack-gateway/main.go` - CLI login flow (performMFAVerification function)
- `scripts/install.sh` - System dependency checks
- `internal/gateway/handlers/auth.go` - Gateway MFA handlers
- `internal/gateway/auth/mfa/service.go` - MFA service with WebAuthn support
