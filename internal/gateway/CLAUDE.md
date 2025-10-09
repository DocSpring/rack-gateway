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

Key files:

- `auth/oauth.go` - OAuth handlers
- `auth/session_manager.go` - Session creation and validation
- `auth/service.go` - Combined auth service

### `/rbac` - Role-Based Access Control

Database-backed RBAC with PostgreSQL:

- **Roles**: viewer, ops, deployer, admin
- **Permissions**: `convox:{resource}:{action}` (e.g., `convox:apps:delete`)
- **Admin role**: Wildcard access to all resources

Key files:

- `rbac/rbac.go` - RBAC manager interface
- `rbac/postgres.go` - PostgreSQL implementation
- `rbac/middleware.go` - RBAC enforcement middleware

### `/proxy` - Convox API Proxy

Request forwarding with WebSocket support:

- Never exposes real Convox rack tokens to clients
- Injects rack token from environment variables
- Adds tracing headers: `X-User-Email`, `X-Request-ID`
- Forwards all methods: GET, POST, PUT, PATCH, DELETE
- Full WebSocket proxy support for `convox exec` and logs

Key files:

- `proxy/proxy.go` - HTTP proxy handler
- `proxy/websocket.go` - WebSocket proxy

### `/audit` - Audit Logging

Structured logging with automatic secret redaction:

- Logs all API requests to structured JSON
- Automatic secret redaction (passwords, tokens, keys)
- Includes RBAC decisions (allow/deny)
- CloudWatch integration via stdout

Key files:

- `audit/logger.go` - Audit log creation
- `audit/redactor.go` - Secret redaction

### `/middleware` - HTTP Middleware

Common middleware:

- `security.go` - CSP headers, CORS, security headers
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

# Reset database (requires DEV_MODE=true or DISABLE_DATABASE_ENVIRONMENT_CHECK=1)
RESET_RACK_GATEWAY_DATABASE=DELETE_ALL_DATA rack-gateway reset-db
```

See `docs/DATABASE_MAINTENANCE.md` for details.

## Testing

### Unit Tests

```bash
task go:test
```

Uses isolated test database (`gateway_test`).

### E2E Tests

```bash
task go:e2e
```

Tests complete CLI flows with real gateway server.

## Configuration

All configuration via environment variables. See `docs/CONFIGURATION.md` for complete list.

**Critical variables:**

- `RACK_HOST` - Convox rack API URL
- `RACK_TOKEN` - Convox rack API token
- `APP_SECRET_KEY` - Secret for session tokens and CSRF protection
- `GOOGLE_CLIENT_ID` - OAuth client ID
- `GOOGLE_CLIENT_SECRET` - OAuth client secret
- `GOOGLE_ALLOWED_DOMAIN` - Allowed email domain (e.g., "example.com")

## Security Considerations

### Content Security Policy (CSP)

The gateway enforces strict CSP:

- No inline scripts (except with nonce)
- No inline styles (except with nonce)
- Nonce is generated per request and passed to React via meta tag

See `middleware/security.go` for CSP configuration.

### CSRF Protection

- All state-changing requests require CSRF token
- Token stored in cookie and validated via header
- Proxy routes (Convox API) require either:
  - Valid CSRF token (for browser requests)
  - Authorization header (for CLI requests)

### TLS Configuration

**Internal Rack TLS**: Rack-to-gateway traffic uses TLS on 5443 with certificate verification disabled because racks expose self-signed/internal endpoints only. This is an intentional private-network trade-off - do not re-flag `InsecureSkipVerify` in `internal/gateway/httpclient`.

## Development

### Building

```bash
task go:build
# Creates: bin/rack-gateway-api
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
- **Secret redaction**: Always use audit logger for sensitive data
