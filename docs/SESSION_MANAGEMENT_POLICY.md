# Access, Session, and Deployment Security Policy

## Purpose

This policy governs how sessions, authentication, and deployments are handled for the Convox Gateway. It ensures the protection of critical AWS infrastructure, aligns with SOC 2 requirements, and enforces bank-grade security practices.

---

## Web Sessions (Admin Console)

- **Idle timeout:** 5 minutes of inactivity. Any interaction (click, API call, tab focus with keep-alive ping) resets the timer.
- **No absolute cap while active:** Users may stay signed in indefinitely if actively using the console.
- **Step-up MFA:** Required for sensitive actions (e.g., user management, disabling deletion protection, infrastructure changes) if last MFA > 5 minutes.
- **Session storage:** Opaque, server-side session IDs stored in an HttpOnly, Secure, SameSite=Strict cookie.
- **Session rotation:** On login, privilege change, or sensitive action.
- **Revocation:** Sessions revoked immediately upon logout, password change, or admin-initiated “log out everywhere.”
- **CSRF protection:** Token injected into `<meta name="csrf-token">` and validated via `X-CSRF-Token` header + Origin/Referer checks.
- **Audit:** All session events logged (login, MFA challenge, step-up, logout, revocation, failures) with user ID, IP, and User Agent.

---

## CLI Authentication (Human Users)

### Goals

- Read-only CLI commands (e.g., `logs`, `ps`, `releases`) should be usable at any time without constant re-auth.
- Write or destructive actions (deploys, deletes, config changes) must require **live MFA** and **Gateway approval**.

### Authentication Flow

- CLI initiates OAuth 2.0 Authorization Code flow in the browser.
- User authenticates via IdP (Google SSO) with MFA (prefer WebAuthn).
- Gateway issues tokens based on scope.

### Token Model

1. **Read-Only Personal Access Token (PAT)**

   - Scope: `read:logs`, `read:apps`, `read:processes`, `read:releases`.
   - Expiry: 30–90 days (rotatable, revocable per device).
   - Stored in local config with `0600` permissions.
   - Logged and rate-limited at the Gateway.

2. **Privileged CLI Commands**
   - Destructive or production-impacting actions (e.g., `deploy`, `delete`) do not execute directly.
   - Flow:
     - CLI submits request with PAT to `/deploys` or privileged endpoint.
     - Gateway creates a **pending** deploy/command.
     - Admin must sign in via the web console, complete MFA, and review PR/commit/artifact/environment before clicking Approve.
     - Gateway executes using a server-held credential unavailable to CLI.
   - Optional: Two-person rule enforced for production.

### Revocation and Audit

- Users can view/revoke their PATs and CLI sessions in the “Security” page.
- “Log out everywhere” invalidates all PATs and sessions.
- Password change or role change revokes all PATs.
- Logs record issuance, use, approval, and revocation with user ID, scope, IP, UA, commit, and environment.

---

## CI/CD Service Accounts

- **Service account tokens** are used by CI/CD pipelines, not personal accounts.
- **Scope:** Limited to `deploy:start` and `artifact:read`. No administrative APIs.
- **Lifetime:** Tokens may be long-lived but must be rotated at least annually and on compromise.
- **Restriction:** CI/CD tokens can only start builds. They cannot directly deploy to production.
- **Deploy approval required:** Gateway places builds in `pending` state until an admin approves.

---

## Mandatory Human-in-the-Loop Deploy Approval

### Requirement

No production deployment can proceed without an authenticated administrator approving it in the Gateway UI with MFA. This is a hard requirement.

### Deploy States

- **pending:** CI/CD requested a deploy.
- **approved:** Admin authenticated with MFA and approved.
- **rejected:** Admin denied. CI/CD notified.
- **expired:** No approval within the window (default 15 minutes).
- **canceled:** CI/CD or admin canceled before approval.

### Approval Checks

- Approver is an admin with active MFA.
- Commit tied to an approved PR.
- Artifact digest matches the one reported by CI/CD (immutable artifacts).
- Environment lock not engaged.
- Optional two-person approval for production.

### Sequence

1. CI/CD calls `POST /deploys` with commit SHA, artifact digest, PR, env, service account ID.
2. Gateway creates a pending deploy, logs event, and sends Slack/email to admins.
3. Admin signs in, completes MFA, reviews details, and clicks Approve or Reject.
4. On Approve:
   - Gateway records approver ID, IP, timestamp.
   - Gateway executes deploy with server-held credential.
5. Notifications sent to CI/CD and Slack on completion.

### Notifications

- Slack: pending deploys, approvals, rejections, completions, failures.
- Email: pending deploys and failures.
- Links use short-lived signed URLs.

### Audit and Evidence

- All deploy approval requests, approvals, rejections, and completions logged.
- Snapshot of approval context (commit, PR, diff link, artifact digest, policy checks).
- Logs retained per SOC2 retention schedule.

---

## Key Management

- Master secret: `APP_SECRET_KEY` used for HMAC and key derivation.
- Subkeys derived via HKDF:
  - CSRF HMAC key
  - JWT signing key
  - OAuth state key
  - Webhook HMAC key
- Rotation: Keys rotated according to key management policy. Previous keys remain active during grace period.

---

## Compliance Mapping

- **SOC2 CC6.1 / CC6.2:** Logical access limited to authorized individuals.
- **SOC2 CC6.6:** Authorization is enforced, with least-privilege tokens and scopes.
- **SOC2 CC7.2:** Monitoring and alerts on authentication anomalies.
- **SOC2 CC7.3:** Sessions and tokens are revocable at any time.
- **NIST SP 800-63B:** Aligns with MFA enforcement, session expiration, and re-authentication requirements.
- **HIPAA BAA alignment:** Strong controls and auditability for systems that may touch PHI.
