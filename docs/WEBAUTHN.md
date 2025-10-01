# WebAuthn CLI Implementation - Continuation

## Current State

✅ **Completed:**
- Removed Yubico OTP from UI (requires cloud validation or self-hosted server)
- Selected `github.com/go-ctap/ctaphid` for FIDO2/CTAP2 client library
- Added system library checks to `scripts/install.sh` for Linux (libudev-dev, libusb-1.0-0-dev)
- Added go-ctap/ctaphid dependency to go.mod (v0.8.1)
- Located CLI login flow in `cmd/cli/main.go:performMFAVerification()`
- **Added gateway API endpoints for WebAuthn assertion:**
  - `POST /.gateway/api/auth/mfa/webauthn/assertion/start` - Start WebAuthn assertion ceremony
  - `POST /.gateway/api/auth/mfa/webauthn/assertion/verify` - Verify WebAuthn assertion response
- **Created MFA service methods:**
  - `StartWebAuthnAssertion()` - Begin WebAuthn login ceremony
  - `VerifyWebAuthnAssertion()` - Validate assertion response with stored session data
- **Added DTOs:**
  - `WebAuthnAssertionStartResponse` - Returns challenge and session data
  - `VerifyWebAuthnAssertionRequest` - Accepts assertion response and session data

⏸️ **Deferred:**
- CLI WebAuthn integration - The `go-ctap/ctaphid` library has a complex iterator-based API that requires significant additional work to integrate properly. WebAuthn works fully in the web UI; CLI users can use TOTP or backup codes.

📝 **Remaining Work:**

### 1. Complete CLI WebAuthn Integration in Login Flow

**File:** `cmd/cli/main.go:performMFAVerification()`

Current code has TODO comment at line 842:
```go
// Check if user has WebAuthn enrolled
// TODO: Call GET /.gateway/api/auth/mfa/status to check available methods
// For now, try TOTP (will add WebAuthn support in next iteration)
```

**Implementation Steps:**

1. **Get MFA Status from Gateway**
   ```go
   // Call GET /.gateway/api/auth/mfa/status with session token
   // Response includes: enrolled, methods (array with type, label, etc)
   ```

2. **Check for WebAuthn Methods**
   ```go
   hasWebAuthn := false
   for _, method := range status.Methods {
       if method.Type == "webauthn" {
           hasWebAuthn = true
           break
       }
   }
   ```

3. **Try WebAuthn First (if available and device detected)**
   ```go
   import "github.com/convox/convox-gateway/internal/cli/webauthn"

   if hasWebAuthn && webauthn.CheckAvailability() {
       // Get assertion challenge from gateway
       // POST /.gateway/api/auth/mfa/webauthn/assertion/start
       // Returns: { challenge, rp_id, allow_credentials, timeout }

       // Sign with device
       assertion, err := webauthn.GetAssertion(options)
       if err != nil {
           fmt.Printf("WebAuthn failed: %v, falling back to TOTP\n", err)
           // Fall through to TOTP prompt
       } else {
           // Submit assertion
           // POST /.gateway/api/auth/mfa/verify with assertion response
           return nil
       }
   }
   ```

4. **Fallback to TOTP prompt** (existing code)

### 2. Add Gateway API Endpoints for WebAuthn Assertion

**Files to modify:**
- `internal/gateway/handlers/auth.go`
- `internal/gateway/routes/routes.go`

**New endpoints needed:**
```go
// Start WebAuthn assertion for CLI authentication
// POST /.gateway/api/auth/mfa/webauthn/assertion/start
// Returns challenge and credential options

// Verify WebAuthn assertion
// POST /.gateway/api/auth/mfa/verify
// Already exists, but needs to handle WebAuthn assertion format
```

### 3. Update WebAuthn Client Module

**File:** `internal/cli/webauthn/webauthn.go`

**Issues to fix:**
- Set correct `origin` in clientData (currently hardcoded to `http://localhost`)
- Get origin from gateway URL parameter
- Handle timeout properly
- Better error messages for common failures (no device, wrong key, etc.)

### 4. Testing Checklist

- [ ] Test WebAuthn login on macOS (no system libs needed)
- [ ] Test WebAuthn login on Linux with system libs installed
- [ ] Test WebAuthn login on Linux without system libs (should fallback to TOTP)
- [ ] Test fallback to TOTP when WebAuthn fails
- [ ] Test with user who has both WebAuthn and TOTP enrolled
- [ ] Test with user who only has TOTP enrolled
- [ ] Test error handling (device disconnected mid-auth, wrong key, etc.)

### 5. Documentation Updates Needed

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
- `cmd/cli/main.go` - CLI login flow (performMFAVerification function)
- `scripts/install.sh` - System dependency checks
- `internal/gateway/handlers/auth.go` - Gateway MFA handlers
- `internal/gateway/auth/mfa/service.go` - MFA service with WebAuthn support
