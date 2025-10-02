# Configuration

This document describes all configuration options for Rack Gateway, grouped by concern.

If you’re deploying to production, read this alongside [DEPLOY.md](DEPLOY.md).

## Table of Contents

- [Configuration](#configuration)
  - [Table of Contents](#table-of-contents)
  - [Core Server](#core-server)
    - [Local Development Ports](#local-development-ports)
    - [Web Frontend (runtime)](#web-frontend-runtime)
  - [Rack Connectivity](#rack-connectivity)
  - [Cookies and Session](#cookies-and-session)
  - [Database and Auditing](#database-and-auditing)
  - [Email (Postmark)](#email-postmark)
  - [CLI Wrapper](#cli-wrapper)
  - [Notes](#notes)

## Core Server

- `PORT` (default: `8080`)
  - TCP port the API listens on.
- `APP_SECRET_KEY` (required in production)
  - Secret used to sign JWTs for web sessions. Auto-generated in dev if missing.
- `SESSION_IDLE_TIMEOUT` (default: `5m`)
  - Sliding inactivity window for browser sessions. Accepts Go duration strings (e.g., `5m`, `15m`).
- `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` (required)
  - OAuth client credentials for Google Workspace (or your OIDC provider).
- `GOOGLE_ALLOWED_DOMAIN` (required)
  - Enforces a single allowed email domain, e.g. `example.com`.
- `GOOGLE_OAUTH_BASE_URL` (default: `https://accounts.google.com`)
  - OIDC issuer base URL. Override in development for the mock OAuth server.
    Derived from `DOMAIN`. No separate redirect URL is needed.
  - OAuth callback URLs are derived from DOMAIN:
    - Web: `https://gateway.example.com/.gateway/api/auth/web/callback`
    - CLI: `https://gateway.example.com/.gateway/api/auth/cli/callback`
- `ADMIN_USERS` (optional)
  - Comma-separated emails to bootstrap admin access on first run.
- `SENTRY_DSN` (optional)
  - When set, enables Sentry reporting for the gateway API server.
- `SENTRY_ENVIRONMENT` (optional)
  - Overrides the Sentry environment tag. Defaults to `development` when `DEV_MODE=true`, otherwise `production`.
- `SENTRY_RELEASE` (optional)
  - Optional release identifier propagated to Sentry for backend events. Defaults to the rack-provided `RELEASE` env var when unset.
- `SENTRY_JS_DSN` (optional)
  - When set, embeds the Sentry DSN into the served web SPA for browser error reporting.
- `SENTRY_JS_TRACES_SAMPLE_RATE` (optional)
  - Floating point sample rate (0-1) for browser performance tracing. Defaults to `0`.
- `ENABLE_SENTRY_TEST_BUTTONS` (optional)

  - When set to `true`, surfaces the Sentry diagnostics actions in the admin settings UI. Defaults to
    `false` to keep the test triggers hidden.

- `DISABLE_DEPLOY_APPROVALS` (optional)
  - When set to `true`, disables deploy approval enforcement. Useful for staging environments.
- `DEPLOY_APPROVAL_WINDOW` (default: `15m`)
  - Duration an approved request remains valid before expiring. Accepts Go duration strings (e.g., `10m`).

### Local Development Ports

All development and test ports are defined in `mise.toml`.

- `GATEWAY_PORT` (default: `8447`)
  - Gateway API port for the dev and preview stacks.
- `WEB_PORT` (default: `5223`)
  - Vite dev server port when running the dev stack.
- `MOCK_OAUTH_PORT` (default: `3345`)
  - Mock Google OAuth server port for dev/preview stacks.
- `MOCK_CONVOX_PORT` (default: `5443`)
  - Mock Convox rack API port for dev/preview stacks.
- `TEST_GATEWAY_PORT` (default: `9447`)
  - Gateway API port for the dedicated test stack (used by E2E suites).
- `TEST_MOCK_OAUTH_PORT` (default: `9345`)
  - Mock OAuth server port for the test stack.
- `TEST_MOCK_CONVOX_PORT` (default: `6443`)
  - Mock Convox rack API port for the test stack.

### Web Frontend (runtime)

- `SENTRY_JS_DSN` (optional)
  - See Core Server section; injected into the SPA at render time.
- `SENTRY_JS_TRACES_SAMPLE_RATE` (optional)
  - See Core Server section for details.

## Rack Connectivity

- `RACK_TOKEN` (required in production)
  - Convox rack API token (used as Basic Auth password).
- `RACK_USERNAME` (default: `convox`)
  - Basic Auth username for the rack.
- `RACK_HOST` is set automatically but can be overridden.

## Cookies and Session

- `COOKIE_SECURE` (default: `true`, automatically disabled when `DEV_MODE=true`)
  - Controls the `Secure` attribute for auth and CSRF cookies.
- `DEV_MODE` (default: `false`)
  - Enables developer-friendly behavior (e.g., non-secure cookies, dev email logging fallback, dev racks).

## Database and Auditing

Postgres is required; set `DATABASE_URL` (or `PG*` variables like `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE`).

- Local development uses two logical databases:
  - `gateway_dev` for the dev/preview stacks.
  - `gateway_test` for the isolated test stack. Task automation ensures both exist when the Docker stack starts.
- `TEST_DATABASE_URL` – optional override for test workflows (defaults to the same host as `DATABASE_URL` but points at `gateway_test`). The Go and web test harnesses automatically prefer this when present.

- `LOG_RETENTION_DAYS` (required for production deploys)

  - Number of days to retain CloudWatch logs for the gateway service.
  - Recommended value: `2557` (7 years).
  - This must be set before deployment on Convox to configure log retention properly.

- Protected Env Vars

  - Stored in the `settings` table under the key `protected_env_vars` (JSON array of variable names, e.g. `["DATABASE_URL","RACK_TOKEN"]`).
  - Protected keys are always masked in API responses and cannot be changed via the gateway, even by admins.
  - Seed on first boot from `DB_SEED_PROTECTED_ENV_VARS` (comma-separated), when the setting is not yet present.

- Destructive Actions Guard
  - Stored under the key `allow_destructive_actions` (boolean; default `false` if unset).
  - When `false`, the gateway blocks destructive API calls (e.g., app deletes, process terminations) even for admins.
  - To perform a destructive operation, temporarily set it to `true` via the admin settings API, perform the action, then set it back to `false`.

## Email (Postmark)

- `POSTMARK_API_TOKEN` (optional)
  - Enables sending email via Postmark. If unset, emails are disabled unless dev logging is on.
- `POSTMARK_FROM` (recommended)
  - Sender address, e.g. `no-reply@example.com`. If unset, defaults to `no-reply@<GOOGLE_ALLOWED_DOMAIN>`.
- `POSTMARK_STREAM` (default: `outbound`)
  - Postmark message stream.
- `POSTMARK_API_BASE` (advanced, default: `https://api.postmarkapp.com`)
  - Override the Postmark API base URL.
- `DEV_EMAIL_LOG` (default: `false`)
  - When `true` (or if `DEV_MODE=true` and no Postmark token), emails are logged to stdout instead of sent. Useful for local testing.

Email events:

- New user added: user receives an access email; admins receive a notification (admins BCC’d, inviter as primary recipient).
- API token created: owner receives a token creation email; admins receive a notification (admins BCC’d, inviter as primary recipient).

## CLI Wrapper

- `GATEWAY_RACK` (optional)
  - Selects the rack for `rack-gateway convox …` when not using `--rack` or the stored current rack in config.json.
- `GATEWAY_CLI_CONFIG_DIR` (dev/testing)
  - Override the CLI config directory (defaults to `~/.config/rack-gateway`).
- `FORCE_CSP_IN_DEV` (dev/testing)
  - When set to `true` alongside `DEV_MODE=true`, emit production CSP headers for local testing.

## Notes

- Some platforms set `PORT`. The gateway binds to `PORT`; ensure your process manager maps it appropriately.
- In development, the gateway can auto-configure a `local` rack for convenience.
