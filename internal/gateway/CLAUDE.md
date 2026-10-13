# Gateway API Server - Claude Development Guide

## Overview

The gateway API server is the core authentication and authorization proxy that sits between users and the Convox Rack API.

## Architecture

**Single-Tenant Design**: Each gateway instance is deployed alongside exactly ONE Convox rack.

- Has access to exactly ONE Convox rack's credentials via environment variables
- Configured via `RACK_HOST` and `RACK_TOKEN`
- Handles OAuth, RBAC, and audit logging
- Proxies all Convox API requests with authentication

## Key Components

### `/auth` - Authentication

OAuth 2.0 and session management:

- **OAuth flow**: PKCE-based Google OAuth 2.0
- **Session tokens**: Opaque tokens stored in database, revocable at any time
- **Domain validation**: Only allows configured Google Workspace domain
- **Sessions**: Cookie-based sessions for web UI, session tokens for CLI
- **Step-up window**: only a verified factor opens one (`UpdateSessionMFAVerified`). A login that needs no MFA
  challenge, or skips it with a trusted device, is marked verified with `ExemptSessionFromMFAChallenge`, which
  opens none; so is an exempt CLI login (`cli_login_states.mfa_exempt`). Trusting a device doesn't restart the window.
  Adding an MFA method is `MFAStepUpOnceEnrolled`: free for a first factor, step-up once the user has one

Key files:

- `auth/oauth.go` - OAuth handlers
- `auth/session_manager.go` - Session creation and validation
- `auth/service.go` - Combined auth service

### `/rbac` - Role-Based Access Control

Casbin policies built from `rbac/roles_config.go`, checked against each caller's current database roles:

- **Roles**: viewer, ops, deployer, admin (people) and cicd (API tokens only)
- **Permissions**: `{scope}:{resource}:{action}` (e.g., `convox:app:delete`, `gateway:user:read`)
- **Admin role**: Wildcard access to all resources
- **Principals**: `rbac.Authorize(principal, permission)`; an API token needs the permission itself AND
  its owner's current role must allow it

**Every authenticated `/api/v1` route MUST be declared in `rbac/gateway_routes.go`.** The `Authorize`
middleware (`middleware/authorize.go`) denies any route missing from the table, and
`routes/route_coverage_test.go` fails CI for a registered route without a policy (or a policy without a
route). Use the builders in `rbac/http_routes.go`:

- `newHTTPRoute` - humans holding every listed permission
- `newTokenRoute` - same, and API tokens may call it (keep this list tiny; CI only)
- `newSelfRoute` - any signed-in human; the handler must scope data to the caller
- `newSelfRead` - a `newSelfRoute` GET without a permission; it names its own audit entries (`info.read`), since
  there's no permission to name them after. A route with neither fails `TestEveryRouteHasAnAuditName`
- `newOwnUserRoute` - `/users/:email/...` routes a user may call for their own account; anyone else
  needs the listed permissions

Permissions on a route also select its MFA level (`rbac/mfa_requirements.go`). Handlers that act on
another user's resources (e.g. API tokens) must still check ownership themselves.

Key files:

- `rbac/rbac.go` - RBAC manager (`Authorize`)
- `rbac/roles_config.go` - Role permissions
- `rbac/gateway_routes.go` - Route policy table
- `middleware/authorize.go` - Route authorization middleware

### `/proxy` - Convox API Proxy

Request forwarding with WebSocket support:

- Never exposes real Convox rack tokens to clients
- Injects rack token from environment variables
- Forwards only an allowlist of client headers (`proxy/forward_headers.go`); add a header there only
  when the Convox API reads it. Privileged run options need `convox:process:run_privileged`
- Forwards only the query parameters the Convox SDK sends (`proxy/forward_query.go`) and refuses
  requests with any other: the rack reads options such as the exec command, release env and build
  manifest from the query string too, which would bypass the gateway's checks
- Build manifests are checked twice: as written (`proxy/manifest_parse.go`, `manifest_structure.go`,
  `manifest_interpolation.go`) and as the rack will load them (`loadManifestAsRack`: Convox's own `manifest.Load`
  with the app's env and the build args substituted). Promotes are checked with the release's env
  (`proxy/promote_images.go`). Keep the `github.com/convox/convox` module in step with the racks' Convox version
- Refuse a request after looking at the rack with `refuse(status, err)` (`proxy/error_handling.go`), so the client
  gets that status and the audit log a deny, never a 500
- Sets `X-Convox-Actor` (user email or `token:<name>`); internal identity headers sent by clients are
  stripped by `middleware/internal_headers.go`
- Protected env vars (the app's `protected_env_vars`) are refused on release
  create unless the request names them in `X-Allow-Protected-Env` (`env set/unset --allow-protected`), the
  caller is a user (never an API token) holding `convox:env:set_protected`, and inline MFA is verified on
  that request even within the step-up window (`proxy/env_protected.go`). An inline proof is verified at
  most once per request (`markInlineMFAVerified`): TOTP codes are single-use. Audit entries for protected
  changes (`env.set_protected`, `env.unset_protected`) never contain values. The web env editor
  (`handlers/api_handler_env.go`) still refuses protected keys
- Proxy MFA (`proxy/mfa_verification.go`): a step-up permission is covered by the session's step-up window, so a
  CLI command needs one proof; an MFAAlways permission needs a proof verified on that request
  (`verifyRequestMFA`, shared with protected env changes). The CLI proxy also requires MFA enrollment from every
  session, whatever its channel (`middleware.RequireMFAEnrollment`)
- MFA settings are read when needed (`settings.Service.GetMFASettings`, one query; the middleware reads them once
  per request), never kept from startup, so a change applies at once on every instance
- Client IPs: use `c.ClientIP()` in gin handlers and `netutil.ClientIP(r)` elsewhere. Both trust
  `X-Forwarded-For` only from `TRUSTED_PROXY_CIDRS`. Never parse forwarding headers yourself
- Forwards all methods: GET, POST, PUT, PATCH, DELETE. Request bodies are capped at 1 MiB as they're read
  (archive uploads stream through), and a body on GET, HEAD or DELETE is refused (`proxy/forward_helpers.go`).
  Release and build list `limit` must be 1-100 (`proxy/forward_query.go`)
- Full WebSocket proxy support for `convox exec` and logs. Messages stream through (1 MiB cap from the client,
  16 MiB from the rack); the gateway pings the client and ends a session whose client stops answering; each
  user or API token may hold 10 sessions at once (`proxy/websocket_stream.go`, `proxy/websocket_sessions.go`)
- Rack HTTP clients share one connection pool per TLS config (`httpclient`); rackcert keeps the same
  `*tls.Config` until the pinned certificate changes, and must not hand out copies

Key files:

- `proxy/handler.go` - HTTP proxy handler
- `proxy/forward_headers.go` - Header allowlist and gateway headers
- `proxy/websocket.go` - WebSocket proxy

### `/audit` - Audit Logging

Structured logging:

- Logs all API requests to structured JSON
- Includes RBAC decisions (allow/deny)
- CloudWatch integration via stdout
- The logger doesn't redact: whatever writes an entry's `Details` keeps secrets out of it, and Slack copies
  `Details` verbatim. Env change entries get their details from `envutil.AuditVisibleKeys.AuditDetails`
  (every value `[REDACTED]` unless the app's `audit_visible_env_vars` lists the key and it is neither secret
  nor protected); rack param changes log key names only; sensitive query parameters go through `RedactQuery`
- Everything fails closed, with no exceptions (reads, logins, MFA and logouts included): nothing may act unless its
  audit entry was written first. `middleware.AuditBeforeActing` (every gateway API route) and the proxy's
  `recordAttempt` (every rack request) write an `attempted` entry first and refuse with 503 when it can't be
  written; the auth handlers do the same around session creation, CLI login MFA and logout
  (`handlers/auth_audit.go`). Write a request's other entries with `LogRequestEntry(ctx, ...)` so they get its
  request ID (its results). Route names come from `rbac/audit_names.go` (a route's permission, `auditActions`,
  or the route's `AuditAction`). A background job that changes state appends its audit events in the same
  transaction as the change (`db.ExpireDeployApprovalRequests`), and passes them to `audit.RecordedInTransaction`.
  Any failed write fails the health check for a minute (`audit/health.go`)
- Never change how a released version writes audit events (`audit.append_audit_event`'s signature, the audit
  roles' grants) in one migration: the previous release keeps serving requests between `migrate` and the promote,
  and it refuses every request it can't record, the promote included. Add the new function alongside the old one,
  and drop the old one in a later release
- Denials (gateway `Authorize`, proxy RBAC/approval/destructive refusals) go through `RecordDenial`: throttled per
  principal + action per minute in memory (one gateway per rack). The request is refused either way; a denial that
  can't be written is logged and sent to Sentry

Key files:

- `audit/logger.go` - Audit log creation
- `audit/attempt.go`, `audit/denials.go` - Attempt/result pairing, throttled denials
- `audit/redaction.go` - Query string redaction
- `envutil/audit.go` - Env change audit details
- `db/audit_append.go`, `db/audit_hash.go`, `db/audit_normalize.go` - Hash-chained append (hash v2). The row is
  normalized to its stored form, hashed, then stored, so the stored row always re-hashes to `event_hash`. Never
  change the v2 encoding (existing rows would stop verifying); a new audit column needs a new hash version
- `jobs/audit/anchor_writer.go` - Hourly S3 Object Lock anchors. Every write (job or `write-anchor`) holds the
  audit anchor advisory lock (`db.WithAuditAnchorLock`) from the existence check through the Put: anchors are
  permanent, so an overlapping write would be a permanent verification failure. `write-anchor` uses
  `WriteCurrentAnchor` (the database's clock, never the host's). The Put is sent once (`putAnchorOnce`: an SDK
  retry after a lost response adds a second version); the job's retry re-checks first. An attempt starting past
  `AnchorWriteWindow`, or before the job was created, is refused (`anchor_window.go`). An existing key must be an
  anchor the writer could have written (`anchor_existing.go`: stored no earlier than its hour, this chain and
  hour, covering every event written before it), or it's `ErrUnexpectedAnchor`. `auditverify` checks
  `LastModified` against `ts_utc` with the same constants, so keep them in step
- `auditverify/` - `rack-gateway-api verify-audit` (chain + anchor verification). Never trust the database's own
  hash v2 start: it comes from `hash_version`, which anyone with database write access can change
- `jobs/verifyaudit/` - The same verification as an hourly River job, configured by `jobs.AuditVerifyOptions` (shared
  with the command). Full check on start, while failing and daily; otherwise the events since the last passing run.
  What it has verified and alerted is in memory only (a database writer must not be able to skip checks or silence
  alerts); `audit_verification_status` is display-only (Audit Logs page). Failures go to Sentry every run and to
  Slack as audit events: `audit_log.verification_failed` (first failure, more failures, daily reminder) and
  `audit_log.verification_recovered` (passing again). A passing run records no event. Every run pings
  `AUDIT_VERIFY_HEARTBEAT_URL`, so a heartbeat monitor alerts when runs stop.
- `heartbeat/`, `jobs/uptime/` - Heartbeat pings (https URLs from env vars, never logged: they carry the monitor's
  key). `UPTIME_HEARTBEAT_URL` is pinged on start and every `UPTIME_HEARTBEAT_INTERVAL_MINUTES` while the health
  check would pass.

### `/middleware` - HTTP Middleware

Common middleware:

- `security_headers.go` - CSP and other security headers
- `csrf.go` - CSRF protection for web requests
- `session.go` - Session management

### `/ui` - Admin Web Interface

Embedded SPA serving:

- Serves pre-built React SPA from `web/dist`
- Handles routing for SPA (all routes return index.html)
- Static asset serving with gzip/brotli compression

## Database

**PostgreSQL** is required.

**Development environment:**

- Database: `gateway_dev`
- Connection: `postgres://postgres:postgres@postgres:5432/gateway_dev?sslmode=disable`
- Docker container: `rack-gateway-postgres-1`
- Host port: `55432`

**Test environment:**

- Database: `gateway_test`
- Connection: `postgres://postgres:postgres@postgres:5432/gateway_test?sslmode=disable`

### Migrations

Migrations are in `internal/gateway/migrations/`:

```bash
# Apply migrations
rack-gateway migrate

# Reset database (requires DEV_MODE=true in a testbypass build, or DISABLE_DATABASE_ENVIRONMENT_CHECK=1)
RESET_RACK_GATEWAY_DATABASE=DELETE_ALL_DATA rack-gateway reset-db
```

See `docs/src/content/docs/operations/database-maintenance.mdx` for details.

## Testing

### Unit Tests

```bash
task go:test
```

Each test gets its own copy of a migrated template database (`testutil/dbtest`), so tests never share data.

### E2E Tests

```bash
task go:e2e
```

Tests complete CLI flows with real gateway server.

## Configuration

All configuration via environment variables. See `docs/src/content/docs/configuration/environment-variables.mdx` for complete list.

**Critical variables:**

- `RACK_HOST` - Convox rack API URL
- `RACK_TOKEN` - Convox rack API token
- `APP_SECRET_KEY` - Secret for session tokens and CSRF protection. The server refuses the published dev value
  (`mise.toml`) and anything under 32 bytes, for it and `AUDIT_HMAC_SECRET`, in a release build or against a
  production database (`app/production_guard.go`)
- `COLUMN_ENCRYPTION_KEY` - Key for encrypted columns (`mfa_methods.secret`, the Slack bot token); base64 of
  32 bytes, required in every mode (dev/test use the published key in `mise.toml`, refused against a
  production database). See `colcrypt/`. Read and write these columns only through the `db` package
  (`CreateMFAMethod`, `CreateSlackIntegration`, the scan paths): it encrypts on write, decrypts on read and
  sets `SecretErr`/`BotTokenErr` instead of an empty secret when a value can't be decrypted. Test tooling
  that touches them directly uses `cmd/totp-gen encrypt` (shell) or `web/e2e/column-crypto.ts`
- `GOOGLE_CLIENT_ID` - OAuth client ID
- `GOOGLE_CLIENT_SECRET` - OAuth client secret
- `GOOGLE_ALLOWED_DOMAIN` - Allowed email domain (e.g., "example.com")

## Security Considerations

### Content Security Policy (CSP)

The gateway enforces strict CSP:

- No inline scripts (except with nonce)
- No inline styles (except with nonce)
- Nonce is generated per request and passed to React via meta tag
- `connect-src 'self'` (plus Sentry when configured), `base-uri 'self'`, `form-action 'self'` and
  `frame-ancestors 'none'`

See `middleware/security_headers.go` for CSP configuration, and `web/CLAUDE.md` for what it means for the SPA.

### CSRF Protection

- All state-changing requests require CSRF token
- Token stored in cookie and validated via header
- Proxy routes (Convox API) require either:
  - Valid CSRF token (for browser requests)
  - Authorization header (for CLI requests)

### Test Bypasses (`testbypass` build tag)

`DEV_MODE`, `E2E_TEST_MODE`, `COOKIE_SECURE=false`, the AWS and Postmark endpoint overrides and a non-https
`GOOGLE_OAUTH_BASE_URL` switch off or redirect security controls so the dev and E2E stacks can run against mocks.
They are compiled in only with the `testbypass` build tag (`testbypass/enabled.go`). Release builds, including the
published Docker image, are built without it (`testbypass/disabled.go`): every bypass reads as off, and
`rack-gateway-api` refuses to start or run any command while one is set. A `testbypass` build still refuses them
against a database marked production (`app/production_guard.go`).

- **Read bypass state only through `testbypass`** (`DevMode()`, `E2ETestMode()`, `InsecureCookies()`,
  `S3EndpointOverride()`, `PostmarkAPIBase()`), or `config.Config.DevMode`, which `config.Load` sets from it.
  Never read `DEV_MODE`, `E2E_TEST_MODE`, `COOKIE_SECURE`, `AWS_ENDPOINT_URL*` or `POSTMARK_API_BASE` anywhere
  else. (`GOOGLE_OAUTH_BASE_URL` is ordinary config: an https OIDC issuer is fine in production.)
- A new test-only switch that weakens auth, TLS, cookies, origin checks, MFA or an external endpoint belongs in
  `testbypass`: add it to both `enabled.go` and `disabled.go`, and to `Active` so release builds refuse it.
- Everything that runs with `DEV_MODE` builds with the tag: `task go:build` (`bin/rack-gateway-api`), `task go:test`,
  air (`.air-gateway.toml`), `Dockerfile.gateway-dev`, and the test and preview compose services
  (`GO_BUILD_TAGS=testbypass`). `task docker:build` and the Release workflow build without it.
- `rack-gateway-api build-info` prints `test bypasses: compiled out` for a release build; the Release workflow
  checks it, and `task docker:release:smoke` (`scripts/release-smoke.sh`) boots the release image with a
  production configuration.

### TLS Configuration

**Internal Rack TLS**: Rack-to-gateway traffic uses TLS on 5443 with certificate verification disabled because racks expose self-signed/internal endpoints only. This is an intentional private-network trade-off - do not re-flag `InsecureSkipVerify` in `internal/gateway/httpclient`.

## Development

### Building

```bash
task go:build
# Creates: bin/rack-gateway-api (a testbypass build: never deploy it)
```

**⛔ NEVER use `go build` directly** - it creates unwanted binaries in root.

### Running Locally

```bash
# Via task (recommended):
task dev

# Direct (not recommended):
./bin/rack-gateway-api
```

### Hot Reload

The project uses `air` for hot reload in development:

```bash
air
```

Configuration in `.air.toml`.

## Important Notes

- **Go handlers must never render HTML**: All web views are rendered via the SPA
- **Never expose rack tokens**: Always inject from environment, never from client
- **Audit everything**: All requests should be logged with RBAC decisions
- **Emails name their gateway**: the delivery sender (`email.IdentifiedSender`) prefixes every subject with
  `Rack Gateway (<RACK_DISPLAY_NAME>):` and adds a footer with the gateway URL, so write subjects without it
- **Slack messages name their gateway**: the `slack` package's `identify` starts every message's fallback text with
  `Rack Gateway (<RACK_DISPLAY_NAME>):` and puts that label, linked to the gateway URL, in the first block, so build
  messages without it. Email and Slack share the name from `config.GatewayIdentity`
- **Secret redaction**: Always use audit logger for sensitive data
