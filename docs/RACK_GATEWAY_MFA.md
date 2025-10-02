# Rack Gateway MFA & Step-Up Authentication Plan

## Background

Rack Gateway currently relies on Google OAuth to authenticate developers. Once a user obtains a JWT, both the web UI and the CLI operate without any second factor. Sessions are short-lived only for the browser; the CLI stores a long-lived JWT locally. To meet the new security requirements (mandatory 2FA, trusted devices, step-up controls, and unified auditing) we are introducing first-class MFA across web and CLI.

The in-progress implementation already adds database support for MFA methods, backup codes, trusted devices, and richer session metadata. The CLI now exchanges the OAuth result for an opaque server-managed session token rather than a JWT, and the server accepts those tokens on both Bearer and Basic channels. This document captures the remaining work to complete the feature end-to-end.

## Objectives

- Require multifactor authentication (TOTP/WebAuthn) after the initial OAuth login for both web and CLI.
- Enforce "trust this device for 30 days" via signed cookies and per-device tracking.
- Provide backup codes (10 one-use codes) with regeneration support and audit trails.
- Perform step-up MFA when sensitive actions occur or when recent MFA freshness exceeds 10 minutes.
- Allow CLI machines to bind authenticated sessions, reuse the recent MFA window, and request step-up on demand (`--mfa`).
- Deliver comprehensive UX changes: 2FA enrollment on first login, self-service management on the user page, clear error handling for expired sessions, etc.
- Ensure all behaviour is covered by automated tests and linting, and runs through the existing task wrappers.

## Key Concepts & Data Model

- `users`: now includes `mfa_enrolled` and `mfa_enforced_at`.
- `user_sessions`: stores channel (`web` vs `cli`), device metadata, MFA verification timestamps, and associated trusted device IDs.
- `mfa_methods`: multiple MFA devices per user (initial focus on TOTP; placeholders for WebAuthn/YubiKey).
- `mfa_backup_codes`: 10 one-time codes; regeneration invalidates previous codes.
- `trusted_devices`: signed cookies bound to device fingerprints, 30-day default TTL.
- MFA settings stored in `settings` table (`require_all_users`, `trusted_device_ttl_days`, `step_up_window_minutes`).

## Functional Requirements

1. **Login Flow**

   - After OAuth success, if user is not enrolled and enforcement is enabled, redirect to MFA enrollment (web) or return an error prompting CLI to enroll.
   - If enrolled, validate trusted device cookie. When absent or invalid, prompt for MFA code.
   - Upon successful MFA, stamp `mfa_verified_at` on the session, set trusted device cookie when requested, and issue `recent_step_up_at`.

2. **CLI Flow**

   - `rack-gateway login` starts OAuth as today, then completes login by exchanging the authorization code for a session token bound to the machine ID.
   - CLI stores `session_token`, session ID, channel, device metadata in `config.json`. All subsequent commands send `Authorization: Bearer <session_token>`.
   - Provide `rack-gateway mfa verify` (or `--mfa`) to refresh the step-up window proactively.

3. **Step-Up Enforcement**

   - Middleware checks `recent_step_up_at`. If older than 10 minutes (configurable) or risk flags triggered, redirect (web) or respond with 401 + `NeedsMFA` payload (CLI).
   - Sensitive routes: env mutations, token management, MFA settings, destructive actions, rack operations, etc.

4. **Frontend UX**

   - Force enrollment page on first login when enforcement enabled.
   - User settings page shows current MFA status, "Enable/Disable 2FA" button, device list, backup code management.
   - Session expiration surfaces friendly toast (“Session expired – sign in again”).

5. **Audit & Security**
   - Log MFA enrollment, verification, trusted device creation/revocation, backup code regen/use, step-up challenges.
   - Rate-limit verification endpoints (e.g., 5 attempts / 5 minutes per user+IP).

## Implementation Plan

### Backend

1. Finish wiring MFA service into handlers:
   - API endpoints for enrollment, verification, backup code regeneration, trusted device management.
   - Middleware for step-up enforcement and trusted device cookie issuance.
   - CLI-specific endpoints to request MFA challenges and verify codes.
2. Update routes config to expose new endpoints and ensure CSRF/rate limiting.
3. Extend `AuthService` and middleware to differentiate between session tokens, JWTs, and API tokens.
4. Implement risk/step-up checks (initial static list of sensitive routes; later add heuristics for IP/UA changes).
5. Ensure `mfa_enforced_at` populated for all users when `require_all_users` setting is true.
6. Add DB helpers and tests for new flows (e.g., listing trusted devices, revoking sessions, backup code regeneration).

### CLI

1. Expose commands to handle MFA prompts:
   - Interactive prompt when server responds with `NeedsMFA` or similar.
   - `--mfa` flag to pre-establish recent MFA.
   - Display backup codes when generated and warn about one-time visibility.
2. Persist machine metadata from `determineDeviceInfo()` and handle config migrations (existing JWT tokens need upgrade path).
3. Update login success messaging to mention 2FA status and next steps when enrollment required.
4. Tests: update CLI integration tests to exercise the new flows using mocked responses.

### Web UI

1. Build MFA enrollment wizard: QR code display, manual key entry, code verification, backup code download/print.
2. User profile page additions:
   - "2FA Enabled" badge with status.
   - List MFA devices (TOTP to start), remove button with confirmation (requires step-up code + typing `DISABLE`).
   - Trusted device list with revoke buttons.
   - Backup code list/regenerate button (with modal warning).
3. Login redirect logic: if `mfa_enrolled=false`, send user to enrollment page immediately.
4. Toast handling for session expired vs unauthorized.
5. Frontend tests (Vitest) for new components and flows.

### Testing & Tooling

- Update Go unit tests to cover MFA service, session enforcement, new handlers.
- Add integration/e2e tests (Playwright or CLI safe-test harness) with simulated MFA challenges.
- Ensure `task go:lint`, `task go:test`, `task web:test`, and `task ci` remain green.
- Provide migrations/seed updates for dev environment.

### Rollout & Migration

1. Introduce feature flag via `settings.mfa` so we can deploy safely (enforced by default but allow temporary override in case of incident).
2. Provide migration script/notes for existing CLI configs (detect JWT tokens, prompt user to re-login to obtain session token).
3. Document new environment variables (`MFA_ENFORCEMENT`, etc.) and instructions for operators.

## Current Status (as of this document)

- Database schema migration created for MFA tables and session metadata.
- `SessionManager` now issues opaque tokens with channel/device metadata; CLI logins consume them.
- CLI config gains stable `machine_id` and stores session details; login flow partially updated.
- Auth middleware can authenticate session tokens; web login continues to function with new session metadata fields.
- `task go:lint` runs clean.

## Next Steps

1. Implement MFA enrollment/verification handlers and connect to MFA service.
2. Update CLI flow to prompt for MFA code after OAuth completion when required.
3. Build frontend UI for enrollment and management.
4. Add step-up middleware for sensitive routes and ensure CLI receives actionable error responses.
5. Write comprehensive unit/integration tests.
6. Document operator workflows and publish release notes.

---

Document owner: Codex CLI agent
Last updated: $(date '+%Y-%m-%d %H:%M:%S')
