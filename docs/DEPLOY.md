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
  - https://$DOMAIN/.gateway/api/auth/web/callback
  - https://$DOMAIN/.gateway/api/auth/cli/callback

1. Copy the values:

- GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET
- Set GOOGLE_ALLOWED_DOMAIN to your Workspace domain (e.g., example.com)
- Leave GOOGLE_OAUTH_BASE_URL empty for Google (defaults to accounts.google.com)

### 2.2 Generate APP_SECRET_KEY

Generate a strong APP_SECRET_KEY (256‑bit, base64). Examples:

- macOS/Linux (OpenSSL):

```
openssl rand -base64 32
```

```
convox env set \
  RACK_TOKEN=xxxxx \
  DOMAIN=gateway.example.com \
  GOOGLE_ALLOWED_DOMAIN=yourexample.com \
  APP_SECRET_KEY=$(openssl rand -base64 32) \
  GOOGLE_CLIENT_ID=... \
  GOOGLE_CLIENT_SECRET=... \
  ADMIN_USERS=admin@yourexample.com \
  POSTMARK_API_TOKEN=xxxx
```

See docs/CONFIGURATION.md for all options.

## 3) Domains

Provide domains via environment or CI vars so Convox substitutes them in `convox.yml`:

- `DOMAIN` → gateway service (e.g., gateway.example.com)
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
curl -s https://$DOMAIN/.gateway/api/health
```

Open https://$WEB_DOMAIN and sign in.

## 6) CI/CD

```
convox apps create convox-gateway || true
convox env set ...
convox deploy
```

Use gateway-issued API tokens for your app deploys to run `convox` via the gateway.
