# Deployment Guide

This guide covers production-ready settings and a minimal Convox deployment. It assumes you already run a Convox rack and will restrict access via your Tailscale network.

## Overview

- Gateway HTTP API on `GATEWAY_PORT` (default 8080)
- OAuth/OIDC for web login, and JWT for API/CLI auth
- RBAC authorization aligned to Convox routes
- Audits persisted to SQLite and mirrored to stdout (JSON for CloudWatch)
- WebSocket proxy supports Convox exec/logs (subprotocol + headers)

## Configuration

Review the full list of environment variables and options in [docs/CONFIGURATION.md](docs/CONFIGURATION.md). The minimal example below demonstrates typical production variables.

## Persistence

The app uses SQLite at `/app/data/db.sqlite`. Attach a persistent volume to that path.

## Health

- Readiness/Liveness: `/.gateway/health` returns `{status:"ok"}`

## Auditing

- Every audit entry is written to the DB and to stdout as structured JSON (for CloudWatch ingestion).
- Control DB size by:
  - Setting `AUDIT_LOG_RETENTION_DAYS` (cleanup at startup), and/or
  - Scheduling the cleanup command (below).

## Scheduled Cleanup (Convox Timers)

Add a timer that runs daily:

```
timers:
  audit_cleanup:
    schedule: "0 3 * * *"   # 3:00 UTC daily
    service: gateway
    command: "convox-gateway audit-cleanup --days 90"
```

`--days` may be omitted if you set `AUDIT_LOG_RETENTION_DAYS`.

## Security Posture (Production)

- Admin API (`/.gateway/admin/*`) rejects cookie-only auth; use Authorization Bearer from the UI.
- Keep the app behind Tailscale (or internal-only) and terminate TLS at the edge.
- Strong `APP_JWT_KEY` required; do not run without it.
- Tokens are non-expiring by design; scope them minimally and rotate on suspicion.

## Minimal convox.yml (example)

```
services:
  gateway:
    build: .
    port: 8080
    environment:
      - GATEWAY_PORT=8080
      - CONVOX_GATEWAY_DB_PATH=/app/data/db.sqlite
      - APP_JWT_KEY
      - GOOGLE_CLIENT_ID
      - GOOGLE_CLIENT_SECRET
      - GOOGLE_ALLOWED_DOMAIN
      - REDIRECT_URL
      - RACK_HOST
      - RACK_TOKEN
      - AUDIT_LOG_RETENTION_DAYS=90
    resources:
      - data
    health:
      path: /.gateway/health
      interval: 10
      timeout: 5
      threshold: 3

resources:
  data:
    type: storage
    size: 1g

timers:
  audit_cleanup:
    schedule: "0 3 * * *"
    service: gateway
    command: "convox-gateway audit-cleanup --days 90"
```

Populate secrets via `convox env set` or your preferred secrets management.

## Logs

Stdout/stderr goes to Convox logs and CloudWatch. Audit lines are JSON-structured for easy indexing.
