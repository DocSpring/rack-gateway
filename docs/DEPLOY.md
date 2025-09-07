# Deployment Guide (Convox)

Deploy the gateway and UI using the `convox.yml` in this repo — no separate manifest needed.

## Prerequisites

- Convox CLI, authenticated against your rack (e.g., `staging`)
- A Convox API token for the rack you want the gateway to proxy to

## 1) Create the app

```
convox apps create convox-gateway
```

## 2) Set environment

All commands assume you are running from this repo root so Convox picks the app name from the directory (no `-a` needed).

### 2.1 Configure Google OAuth (one‑time)

Create an OAuth client in Google Cloud Console for your Workspace domain:

1. OAuth consent screen:

- User Type: Internal (recommended for Google Workspace)
- Scopes: openid, email, profile

2. OAuth client ID (APIs & Services → Credentials → Create → OAuth client ID):

- Application type: Web application
- Authorized JavaScript origins:
  - https://$WEB_DOMAIN (e.g., https://portal.example.com)
- Authorized redirect URIs:
  - https://$GATEWAY_DOMAIN/.gateway/web/callback

1. Copy the values:

- GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET
- Set GOOGLE_ALLOWED_DOMAIN to your Workspace domain (e.g., company.com)
- Leave GOOGLE_OAUTH_BASE_URL empty for Google (defaults to accounts.google.com)

### 2.2 Generate APP_JWT_KEY

Generate a strong APP_JWT_KEY (256‑bit, base64). Examples:

- macOS/Linux (OpenSSL):

```
openssl rand -base64 32
```

```
convox env set \
  APP_JWT_KEY=$(openssl rand -base64 32) \
  GOOGLE_CLIENT_ID=... \
  GOOGLE_CLIENT_SECRET=... \
  GOOGLE_ALLOWED_DOMAIN=yourcompany.com \
  REDIRECT_URL=https://gateway.example.com/.gateway/web/callback \
  ADMIN_USERS=admin@yourcompany.com \
  RACK_HOST=https://api.target-rack.convox.cloud \
  RACK_TOKEN=xxxxx

# Optional email
convox env set \
  POSTMARK_API_TOKEN=xxxx \
  POSTMARK_FROM=no-reply@docspring.com
```

See docs/CONFIGURATION.md for all options.

## 3) Domains

Provide domains via environment or CI vars so Convox substitutes them in `convox.yml`:

- `GATEWAY_DOMAIN` → gateway service (e.g., gateway.example.com)
- `WEB_DOMAIN` → web service (e.g., portal.example.com)

## 4) Deploy

```
convox deploy -a convox-gateway
```

This builds:

- `gateway` (Dockerfile.gateway) — API/proxy on port 8080
- `web` (web/Dockerfile) — Nginx SPA on port 80, proxies `/api/` to `gateway`

## 5) Verify

```
curl -s https://$GATEWAY_DOMAIN/.gateway/health
```

Open https://$WEB_DOMAIN and sign in.

## 6) Audit retention (built in)

The `convox.yml` in this repo includes a daily timer that runs the built‑in cleanup command:

```
timers:
  audit_cleanup:
    schedule: "0 3 * * *"
    service: gateway
    command: "convox-gateway audit-cleanup --days 90"
```

Adjust the schedule or days as needed. You can also set `AUDIT_LOG_RETENTION_DAYS` to trigger a cleanup on boot.

## 7) CI/CD

```
convox apps create convox-gateway || true
convox env set ...
convox deploy
```

Use gateway-issued API tokens for your app deploys to run `convox` via the gateway.
