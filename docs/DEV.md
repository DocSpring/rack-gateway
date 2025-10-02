# Development Setup Guide

Complete guide for setting up the Rack Gateway development environment.

## Prerequisites

- **Go 1.22+** - [Install Go](https://golang.org/doc/install)
- **Docker & Docker Compose** - [Install Docker](https://docs.docker.com/get-docker/)
- **Node.js 20+** - [Install Node.js](https://nodejs.org/en/download/)
- **pnpm** - `npm install -g pnpm`
- **mise** (recommended) - [Install mise](https://mise.jdx.dev/getting-started.html) for environment variable management

## Quick Start

```bash
# Clone the repository
git clone https://github.com/DocSpring/rack-gateway.git
cd rack-gateway

# Install dependencies
go mod download
cd web && pnpm install && cd ..

# Set up configuration (see Configuration section below)
cp mise.local.toml.example mise.local.toml

# Start development environment
task dev
```

The development environment will start:

- **Gateway API**: http://localhost:8447
- **Web UI**: http://localhost:5223
- **Mock Convox API**: http://localhost:5443

## Local Dev Walkthrough (Step-by-step)

Use this concise flow to spin up, explore endpoints, and test the CLI. Port selection is handled by mise configuration (no need to pass env vars on the command line).

1. Start services

```bash
task dev
```

2. Verify services

- Web UI: `http://localhost:$WEB_PORT` (default 5223)
- Gateway health: `curl http://localhost:$PORT/.gateway/api/health`
- Mock Convox: `curl http://localhost:$MOCK_CONVOX_PORT/health`

3. Build CLI and log in

```bash
task go:build:cli
./bin/rack-gateway login local http://localhost:$PORT
# Browser opens (mock OAuth). Complete login, then CLI stores token locally.
```

4. Try proxied Convox commands via gateway

```bash
./bin/rack-gateway convox rack
./bin/rack-gateway convox apps
./bin/rack-gateway convox ps -a myapp
```

5. Admin UI

- Open `http://localhost:$WEB_PORT`
- Manage users, tokens, and view audit logs

6. Stop/cleanup

```bash
task dev:down   # stop
task dev:logs   # view logs
```

## Architecture Overview

The Rack Gateway is split into multiple components:

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│  CLI (Client)   │───▶│  Gateway Server  │───▶│  Convox Rack    │
│  ~/.config/     │    │  Admin-managed   │    │  Real API       │
│  rack-gateway │    │  OAuth + RBAC    │    │                 │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                              │
                              ▼
                       ┌──────────────────┐
                       │   Web UI         │
                       │   User/Role Mgmt │
                       └──────────────────┘
```

### Component Separation

**Gateway Server**:

- Uses Postgres (set `DATABASE_URL` or `PG*` vars)
- Has access to real Convox rack tokens via environment variables
- Runs OAuth authentication and audit logging

**CLI Client** (`config/cli/` in dev, `~/.config/rack-gateway/` in production):

- Stores `config.json` with JWT tokens per rack
- Never has direct access to Convox rack credentials
- Environment variable: `GATEWAY_CLI_CONFIG_DIR=./config/cli` (dev only)

**Web UI**:

- Served at `/ui/*` by the Gateway server
- Admin interface for user and role management
- Uses the same OAuth and JWT authentication as CLI

## Configuration

### 1. Environment Variables (mise)

Create your local configuration:

```bash
cp mise.local.toml.example mise.local.toml
```

Edit `mise.local.toml` with your settings:

```toml
[env]
# Google OAuth credentials (see Google OAuth Setup below)
GOOGLE_CLIENT_ID = "your-client-id.apps.googleusercontent.com"
GOOGLE_CLIENT_SECRET = "your-client-secret"
GOOGLE_ALLOWED_DOMAIN = "yourdomain.com"

# Override JWT key for local development if needed
# APP_SECRET_KEY = "your-local-jwt-secret"
```

### 2. Database Configuration

Use Postgres in all environments. Configure via `DATABASE_URL`, or via libpq-style env (`PGHOST`, `PGPORT`, `PGUSER`, `PGDATABASE`, and optionally `PGSSLMODE`).

The database stores:

- Users and their roles
- API tokens for CI/CD
- Audit logs

## Google OAuth Setup

### Step 1: Create Google Cloud Project

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select existing one
3. Enable the Google+ API:
   - Go to "APIs & Services" → "Library"
   - Search for "Google+ API"
   - Click "Enable"

### Step 2: Configure OAuth Consent Screen

1. Go to "APIs & Services" → "OAuth consent screen"
2. Choose "Internal" for Google Workspace users only
3. Fill in required fields:
   - App name: "Rack Gateway"
   - User support email: your email
   - Developer contact: your email
4. Add scopes: `openid`, `email`, `profile`
5. Save and continue

### Step 3: Create OAuth Credentials

1. Go to "APIs & Services" → "Credentials"
2. Click "+ CREATE CREDENTIALS" → "OAuth client ID"
3. Choose "Web application"
4. Add authorized redirect URIs:

- `http://localhost:8447/.gateway/api/auth/cli/callback` (development)
- `https://your-production-domain.com/.gateway/api/auth/cli/callback` (production)

5. Save and copy the Client ID and Client Secret
6. Update your `mise.local.toml` with these values

### Step 4: Domain Restriction

The gateway automatically restricts access to users from your Google Workspace domain. Configure this in:

- `mise.local.toml`: `GOOGLE_ALLOWED_DOMAIN = "yourdomain.com"`

## Development Workflows

### Running Components Individually

```bash
# Build binaries
task build

# Run gateway server directly (useful for debugging)
./bin/rack-gateway-api

# Run CLI commands
./bin/rack-gateway login staging http://localhost:8447
./bin/rack-gateway convox apps
```

### Using Docker Compose

```bash
# Start all services
task dev

# View logs
task dev:logs

# Rebuild images
task docker

# Stop everything
task dev:down
```

### Testing the Full Flow

1. **Start development environment**:

   ```bash
   task dev
   ```

2. **Access the web UI**:

   - Open http://localhost:5223
   - Click "Login with Google"
   - Complete OAuth flow

3. **Test CLI authentication**:

   ```bash
   ./bin/rack-gateway login staging http://localhost:8447
   # This opens browser for OAuth

   ./bin/rack-gateway convox apps
   # Should proxy to mock Convox server
   ```

4. **Check audit logs**:
   ```bash
   docker compose logs gateway-api | grep audit
   ```

## Development Environment Ports

- **8447**: Gateway API server
- **5223**: Web UI (Vite dev server)
- **5443**: Mock Convox server

## Testing

### Unit Tests

```bash
task go:test
```

### Integration Tests

```bash
task go:test
```

Integration tests use different ports to avoid conflicts:

- Gateway API: 8448
- Mock Convox: 9090

### Web Tests

```bash
cd web
pnpm test        # Run once
pnpm test:ui     # Interactive UI
```

## Debugging

### Enable Debug Logging

Set in `mise.local.toml`:

```toml
[env]
LOG_LEVEL = "debug"
```

### Common Issues

**Port conflicts**: If ports 8447, 5223, or 5443 are in use, stop other services or update the ports in `docker-compose.yml`.

**OAuth errors**: Ensure redirect URIs match exactly in Google Cloud Console and your local setup.

**RBAC issues**: Ensure your email is allowed via roles in the database. For initial bootstrap in dev, set `ADMIN_USERS` to include your email.

**CLI config issues**: The CLI stores config in different locations:

- Development: `./config/cli/` (set by `GATEWAY_CLI_CONFIG_DIR`)
- Production: `~/.config/rack-gateway/`

## File Structure

```
config/
└── cli/                 # CLI development config (auto-created)
    └── config.json      # Current rack, JWT tokens, and gateway URLs

internal/
├── gateway/
│   ├── auth/           # OAuth and JWT handling
│   ├── rbac/           # Role-based access control
│   ├── proxy/          # Request forwarding
│   ├── audit/          # Audit logging
│   └── ui/             # Admin web interface
└── integration/        # Integration tests

cmd/
├── gateway/            # Gateway server main
├── cli/               # CLI tool main
└── mock-convox/       # Mock Convox server for testing

web/                   # React/TypeScript web UI
├── src/
├── public/
└── dist/             # Built assets (auto-generated)
```

## Next Steps

1. Set up your Google OAuth application
2. Add your OAuth credentials to `mise.local.toml`
3. Run `task dev` and test the full authentication flow
4. Try CLI commands: `./bin/rack-gateway login staging http://localhost:8447`

## Production Deployment

For production deployment instructions, see the main [README.md](../README.md).

## Getting Help

- Check the [troubleshooting section](README.md#troubleshooting) in README.md
- Review the [AGENTS.md](AGENTS.md) for technical implementation details
- Look at integration tests in `internal/integration/` for usage examples
