## Task 015 – internal/gateway/handlers/auth_mfa_verification.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/015-auth-mfa-verification-dedupe`
Branch: `agents/015-auth-mfa-verification-dedupe`

### Scope
- Deduplicate the step-up verification flow shared by `VerifyMFA` and `VerifyWebAuthnAssertion` (context loading, trusted device handling, session update, response construction).
- Factor common error handling for failed verifications and trusted device updates into reusable helpers without changing response payloads.
- Keep the extra MFA debug logging intact (or relocate it into the new helper if that keeps output identical).

### Suggested approach
1. Extract a helper that accepts the context, trust-device flag, and a function performing the specific verification (`VerifyTOTP` vs `VerifyWebAuthnAssertion`).
2. Ensure both paths still notify the security notifier on failure and call `notifyLoginComplete` only after session updates succeed.
3. Add focused unit tests in `auth_mfa_verification_test.go` (or new tests) covering both TOTP and WebAuthn flows using the shared helper, including failure branches.

### Required checks
- `task go:test`
- Any new MFA verification tests introduced

Document the helper with a short comment so future contributors know how to plug in additional factors.
