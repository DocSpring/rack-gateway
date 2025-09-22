# Convox Gateway - Technical Details

IMPORTANT: Read [docs/CONVOX_REFERENCE.md](docs/CONVOX_REFERENCE.md) and [README.md](README.md) first for context on how Convox actually works and current project status.

## 🔌 PORT CONFIGURATION - SINGLE SOURCE OF TRUTH

**All development ports are defined in `mise.toml`. NEVER hardcode ports elsewhere.**

| Service          | Port | Environment Variable | Description              |
| ---------------- | ---- | -------------------- | ------------------------ |
| **Gateway API**  | 8447 | `GATEWAY_PORT`       | Main API server          |
| **Web Frontend** | 5223 | `WEB_PORT`           | Vite dev server          |
| **Mock OAuth**   | 3345 | `MOCK_OAUTH_PORT`    | Mock Google OAuth server |
| **Mock Convox**  | 5443 | `MOCK_CONVOX_PORT`   | Mock Convox API server   |

**URLs in Development:**

- Gateway API: `http://localhost:8447`
- Web UI: `http://localhost:5223`
- Mock OAuth: `http://localhost:3345`
- Mock Convox API: `http://localhost:5443`

**Configuration References:**

- `mise.toml` - Defines all port environment variables
- `web/vite.config.ts` - Uses `process.env.GATEWAY_PORT` for proxy
- `Procfile.dev` - Uses `$MOCK_OAUTH_PORT` and `$MOCK_CONVOX_PORT`
- [docs/CONFIGURATION.md](docs/CONFIGURATION.md) - Full list of environment variables

## ⚠️ QUALITY CHECKLIST - MUST PASS BEFORE MARKING TASKS COMPLETE

**NEVER mark a task as "completed" unless ALL of these pass:**

### 🔧 Build Requirements

**⛔ FORBIDDEN: Never use `go build` directly - creates unwanted binaries in root**

- `task all` - All linters, typechecks, unit tests, builds, and E2E tests pass

### 📏 Code Quality

- `go vet ./...` - No vet issues
- `golangci-lint run` or equivalent linting passes
- `cd web && pnpm lint` - Frontend linting passes
- No TypeScript compilation errors
- No unused imports or variables

### 🧪 Integration Tests

- `./dev.sh` - Development environment starts successfully
- `curl http://localhost:8447/.gateway/api/health` - Gateway health check passes
- `curl http://localhost:3345/health` - Mock OAuth health check passes
- `curl http://localhost:5443/health` - Mock Convox health check passes

### 🚀 Production Readiness

- `task docker` - Docker build command passes

**If ANY of these fail, the task is NOT complete. Fix all issues before marking done.**

## Project Overview

This is an authentication and authorization proxy for self-hosted Convox racks. It sits between users and the Convox API, adding:

- Google Workspace OAuth authentication
- Role-based access control (RBAC)
- Complete audit logging with automatic secret redaction
- Multi-rack support (staging, US, EU, etc.)

## Architecture

```
Developer Machine -> Gateway Server (Admin-run) -> Convox Racks
                     |
                     v
                  Audit Logs -> CloudWatch

Flow:
1. Developer runs: convox-gateway apps
2. CLI loads JWT from ~/.config/convox-gateway/config.json
3. CLI sets RACK_URL with JWT and calls real convox CLI
4. Request goes to Gateway API Server with JWT auth
5. Gateway validates JWT and checks RBAC permissions
6. Gateway proxies to real Convox rack using actual token
7. Gateway logs request to CloudWatch
8. Response flows back through gateway to developer
```

## Key Implementation Details

### Distributed Architecture

**Gateway Server (Admin-managed):**

- Runs on dedicated infrastructure
- Has access to real Convox rack credentials
- Configured via environment variables (RACK*URL*_, RACK*TOKEN*_)
- Handles OAuth, RBAC, and audit logging

**Developer CLI (User-installed):**

- Installed as binary at `/usr/local/bin/convox-gateway`
- Wraps the real `convox` CLI
- Stores config in `~/.config/convox-gateway/config.json`
- Never has direct access to Convox rack tokens

### Authentication Flow

1. User runs `convox-gateway login staging https://convox-gateway.example.com`
2. CLI opens browser for Google OAuth (PKCE flow)
3. User authenticates with Google Workspace account
4. Gateway validates domain (@example.com)
5. Gateway issues JWT token (30 day TTL)
6. CLI stores gateway URL and token in `~/.config/convox-gateway/config.json`

### Authorization (RBAC)

- Database-backed RBAC manager (Postgres)
- Roles: viewer, ops, deployer, admin
- Permission mapping to Convox routes/actions, e.g., `convox:{resource}:{action}`
- Admin role has wildcard access

### Proxy Behavior

- Never exposes real Convox rack tokens to clients
- Injects rack token from environment variables
- Adds tracing headers: X-User-Email, X-Request-ID
- Forwards all methods: GET, POST, PUT, PATCH, DELETE
- Full WebSocket proxy support for `convox exec` and logs (subprotocol preserved)

### Security Features

[... omitted for brevity ...]

- `~/.config/convox-gateway/config.json` - Gateway URLs and JWT tokens per rack

## Environment Variables

See [docs/CONFIGURATION.md](docs/CONFIGURATION.md) for the complete and current list of configuration options.

### Local Development Configuration

**IMPORTANT: This project uses mise for environment variable management, NOT .env files.**

- `mise.toml` - Project-level configuration (checked into git)
- `mise.local.toml` - Local overrides (gitignored, create your own)
- `mise.local.toml.example` - Template for local configuration

Environment variables are automatically loaded when you `cd` into the project directory via mise. No need to source .env files or manually export variables.

Example `mise.local.toml`:

```toml
[env]
APP_JWT_KEY = "your-local-jwt-key"
GOOGLE_CLIENT_ID = "your-oauth-client-id"
GOOGLE_CLIENT_SECRET = "your-oauth-secret"
GOOGLE_ALLOWED_DOMAIN = "yourexample.com"
```

## Deployment Notes

### Convox Deployment

- Use `convox.yml` for app definition
- Enable sticky sessions for OAuth flow
- Set all env vars via `convox env set`

### CloudWatch Integration

- Structured JSON logs to stdout
- Automatic ingestion via Convox
- Create metric filters for denied requests
- Alert on spikes in rbac_decision="deny"

## Code Structure

```
internal/
  gateway/
    auth/     - OAuth + JWT handling
    rbac/     - RBAC manager and policies
    proxy/    - Request forwarding logic
    audit/    - Structured logging + redaction
    config/   - Configuration management
    ui/       - Admin web interface
```

## Known Limitations

1. **No User Self-Service** - Admin must add users manually
2. **Basic UI** - Minimal functionality, needs enhancement
3. **No Metrics** - Should add Prometheus/OpenTelemetry

## Security Considerations

1. **JWT Key Rotation** - Not implemented, needed for production
2. **Token Refresh** - No refresh tokens, users re-auth after 30 days
3. **Audit Log Encryption** - Relies on CloudWatch/KMS
4. **Rate Limiting** - Not implemented, needed for production
5. **Internal Rack TLS** - Rack-to-gateway traffic uses TLS on 5443 with certificate verification disabled because racks expose self-signed/internal endpoints only. Treat this as an intentional private-network trade-off; do not re-flag `InsecureSkipVerify` in `internal/gateway/httpclient`.

## Next Steps for Production

1. Verify actual Convox API behavior
2. Implement JWT key rotation
3. Add Prometheus metrics
4. Enhance web UI
5. Add integration tests with mock Convox
6. Set up CI/CD pipeline
7. Add OpenTelemetry tracing

## Web Testing Policy (Vite + TanStack)

- Prefer fast feedback: write unit tests and run type checks before E2E.
- Always run `cd web && pnpm typecheck` and keep types clean.
- Unit tests should cover:
  - Router basepath handling for `/.gateway/web`, including `/login` and `/auth/callback` routes.
  - Auth flows and API adapters (mock network; do not depend on browser).
  - Critical UI/behavior for Users, Tokens, and Audit pages.
- When a web E2E test fails, first reproduce the failure with a focused unit test; fix it there, then re‑run E2E.
- Do not run `docker compose` manually; use `task` targets (e.g., `task web:test`, `task e2e:web:release`).

## Refactor & Organization Policy (Important)

- Never optimize for “don’t break what’s working” when the structure is wrong. Prefer the obviously better organization and implement it decisively.
- Proactively refactor for clarity and maintainability without waiting for prompts when the intent is clear.

When in doubt, choose the straightforward, well‑named, maintainable structure — even if it means removing or renaming existing files. Don’t defer obvious organization or code quality improvements.

## 📋 Task Commands - ALWAYS USE THESE, NEVER RAW COMMANDS

**CRITICAL: Always use `task` commands instead of raw commands. Never use grep, pipes, or manual command construction.**

**IMPORTANT: All task commands automatically handle their dependencies. You NEVER need to manually rebuild or restart services before running tests. For example:**
- `task web:e2e:dev` automatically rebuilds the gateway, restarts docker containers, and runs tests
- `task go:test` automatically downloads dependencies and runs tests
- `task docker:up` automatically builds all images before starting containers

### 🎯 Primary Commands

| Command | Description | When to Use |
|---------|------------|------------|
| `task all` | Run ALL linters, tests, and builds | **ALWAYS before marking any task complete** |
| `task dev` | Start dev stack and follow logs | Starting development environment |
| `task test` | Run all tests | Quick test run during development |
| `task lint` | Run all linters and typecheck | Before committing code |
| `task lint:fix` | Auto-fix linting issues | When linters report fixable issues |
| `task build` | Build all binaries | Creating release artifacts |

### 🐳 Docker Development

| Command | Description |
|---------|------------|
| `task docker:up` | Start full dev stack |
| `task docker:down` | Stop and remove containers |
| `task docker:reset` | Reset dev stack (recreate DB) |
| `task docker:logs` | Tail logs for dev stack |
| `task docker:wait` | Wait for stack readiness |

### 🔧 Go Development

| Command | Description |
|---------|------------|
| `task go:build` | Build all Go binaries |
| `task go:lint` | Run Go linters (vet/fmt/staticcheck) |
| `task go:test` | Run Go unit/integration tests (uses test DB) |
| `task go:e2e` | Run CLI E2E tests |

### 🌐 Web Development

| Command | Description |
|---------|------------|
| `task web:build` | Build web SPA |
| `task web:lint` | Run TypeScript/Biome checks |
| `task web:lint:fix` | Auto-fix web linting issues |
| `task web:test` | Run Vitest unit tests |
| `task web:e2e` | Run Playwright E2E tests |
| `task web:lint:typecheck` | TypeScript type checking only |

### 🧪 E2E Testing

| Command | Description |
|---------|------------|
| `task e2e` | Run ALL E2E tests |
| `task web:e2e:dev` | Web E2E against dev (Vite) |
| `task web:e2e:preview` | Web E2E against preview (compiled) |
| `task go:e2e:dev` | CLI E2E against dev |
| `task go:e2e:preview` | CLI E2E against preview |

### 🎭 Mock Services

| Command | Description |
|---------|------------|
| `task mock-oauth:dev` | Run mock OAuth server |
| `task mock-oauth:lint` | Lint mock OAuth code |
| `task mock-oauth:build` | Build mock OAuth server |

### ⚠️ Common Mistakes to Avoid

**NEVER DO THIS:**
```bash
# ❌ WRONG - Never use raw commands
go test ./...
go build ./cmd/gateway
cd web && pnpm test
grep "PASS" test_output.txt

# ❌ WRONG - Never pipe or filter test output
task test | grep -v "SKIP"
task go:test | head -20
```

**ALWAYS DO THIS:**
```bash
# ✅ CORRECT - Use task commands
task go:test
task build
task web:test
task all  # Run everything, see full output
```

## Pre-Push Checks

**Before ANY push or marking tasks complete:**

```bash
task all
```

This runs:
- Web Biome lint via `pnpm lint`
- Go vet/fmt/staticcheck
- Go unit and integration tests (uses isolated test databases)
- Web unit tests (Vitest)
- Web and CLI E2E tests (Playwright + scripts)
- Full builds of all components

**If `task all` doesn't pass completely, the task is NOT done.**

## Useful Commands for Development

```bash
# Run with hot reload (use task dev instead)
air

# Check for security issues
gosec ./...

# Generate mocks for testing
mockgen -source=internal/rbac/rbac.go -destination=internal/rbac/mock_rbac.go

# Profile memory usage
go tool pprof http://localhost:8080/debug/pprof/heap

# Check test coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Related Documentation

- [Convox Rack API](https://docs.convox.com/reference/rack-api) (if it exists)
- [Google OAuth 2.0](https://developers.google.com/identity/protocols/oauth2)
- [JWT Best Practices](https://tools.ietf.org/html/rfc8725)

## CRITICAL SAFETY WARNINGS

### NEVER DELETE THESE DIRECTORIES

**These contain LIVE Convox configuration backups that protect the user's actual production settings:**

- `/Users/*/Library/Preferences/convox.IMPORTANT_DO_NOT_DELETE_LIVE_BACKUP`
- Any backup directory in `/Users/*/Library/Preferences/`

The integration tests create backups of the real Convox CLI configuration to prevent data loss. If tests fail with "backup already exists", it means a previous test didn't clean up properly. The user must manually verify and move/restore the backup - NEVER automatically delete it!

## Important Instructions

Don't leave old code lying around. When you see it, tidy it.
