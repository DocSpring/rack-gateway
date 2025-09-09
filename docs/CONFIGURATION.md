# Configuration

This document describes all configuration options for Convox Gateway, grouped by concern.

If you’re deploying to production, read this alongside [DEPLOY.md](DEPLOY.md).

## Table of Contents

- [Configuration](#configuration)
  - [Table of Contents](#table-of-contents)
  - [Core Server](#core-server)
  - [Rack Connectivity](#rack-connectivity)
  - [Cookies and Session](#cookies-and-session)
  - [Database and Auditing](#database-and-auditing)
  - [Email (Postmark)](#email-postmark)
  - [CLI Wrapper](#cli-wrapper)
  - [Notes](#notes)

## Core Server

- `PORT` (default: `8080`)
  - TCP port the API listens on.
- `APP_JWT_KEY` (required in production)
  - Secret used to sign JWTs for web sessions. Auto-generated in dev if missing.
- `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` (required)
  - OAuth client credentials for Google Workspace (or your OIDC provider).
- `GOOGLE_ALLOWED_DOMAIN` (required)
  - Enforces a single allowed email domain, e.g. `company.com`.
- `GOOGLE_OAUTH_BASE_URL` (default: `https://accounts.google.com`)
  - OIDC issuer base URL. Override in development for the mock OAuth server.
    Derived from `DOMAIN`. No separate redirect URL is needed.
  - OAuth callback URLs are derived from DOMAIN:
    - Web: `https://gateway.example.com/.gateway/api/auth/web/callback`
    - CLI: `https://gateway.example.com/.gateway/api/auth/cli/callback`
- `ADMIN_USERS` (optional)
  - Comma-separated emails to bootstrap admin access on first run.

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
- `FRONTEND_BASE_URL` (default: `/`)
  - Redirect target after successful web login.

## Database and Auditing

Postgres is required; set `DATABASE_URL` (or `PG*` variables like `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE`).

- `AUDIT_LOG_RETENTION_DAYS` (optional)
  - If set, the server purges audit rows older than N days at startup.
  - Also used by the `convox-gateway audit-cleanup --days N` command (see [DEPLOY.md](DEPLOY.md)).

## Email (Postmark)

- `POSTMARK_API_TOKEN` (optional)
  - Enables sending email via Postmark. If unset, emails are disabled unless dev logging is on.
- `POSTMARK_FROM` (recommended)
  - Sender address, e.g. `no-reply@company.com`. If unset, defaults to `no-reply@<GOOGLE_ALLOWED_DOMAIN>`.
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
  - Selects the rack for `convox-gateway convox …` when not using `--rack` or a current rack file.
- `GATEWAY_CLI_CONFIG_DIR` (dev/testing)
  - Override the CLI config directory (defaults to `~/.config/convox-gateway`).

## Notes

- Some platforms set `PORT`. The gateway binds to `PORT`; ensure your process manager maps it appropriately.
- In development, the gateway can auto-configure a `local` rack for convenience.
