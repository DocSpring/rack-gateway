# Rack Gateway - Technical Details

IMPORTANT: Read [docs/CONVOX_REFERENCE.md](docs/CONVOX_REFERENCE.md) and [README.md](README.md) first for context on how Convox actually works and current project status.

## 🚨 MISSION-CRITICAL AUTHENTICATION GATEWAY

**This is production infrastructure that controls access to sensitive systems.**

- **Purpose**: SOC 2 compliant authentication/authorization gateway for production infrastructure
- **Stakes**: Million-dollar fines, regulatory compliance, protection of sensitive data
- **Audit**: Will undergo professional security audit and penetration testing
- **Standards**: Zero compromises on code quality, testing, or security

## 🔒 NON-NEGOTIABLE QUALITY STANDARDS

### 1. File Length Limits - STRICTLY ENFORCED

- **Maximum 500 lines per Go code file** - no exceptions
- **Maximum 1000 lines per Go test file** - no exceptions
- **Maximum 500 lines per TypeScript/TSX code file** - no exceptions
- **Maximum 1000 lines per TypeScript test file** - no exceptions
- If a file approaches these limits, **refactor immediately** or split tests into multiple files
- Break large files into smaller, focused modules
- Enforced by `task file-length` in CI and pre-commit hooks

### 2. Function Length Limits - STRICTLY ENFORCED

- **Maximum 100 lines per function** (Go)
- **Maximum 50 statements per function** (Go)
- **Maximum cognitive complexity of 15** (Go)
- Enforced by golangci-lint (`funlen`, `gocognit`, `gocyclo`)

### 3. Code Duplication - ZERO TOLERANCE

- **Zero code duplication allowed** in production code
- Threshold: 10 lines or 50 tokens (enforced by jscpd)
- Enforced in CI and pre-commit hooks
- Extract common code immediately when duplication is detected
- Test files may have some duplication (excluded from jscpd)

### 4. Linting - ALL RULES ENABLED

**Go (golangci-lint v2):**

- **Comprehensive linter set** - 20+ linters enabled
- **Line length: 120 characters maximum**
- **NO `//nolint` comments allowed** - fix the code, don't suppress warnings
- **NO disabling linting rules** - if code doesn't pass, refactor it
- Forbidden patterns:
  - `panic` - use error returns
  - `fmt.Print*` (except in CLI output) - use proper logging
  - `time.Sleep` - use context with timeout

**Web (Biome):**

- **NO `// biome-ignore` comments allowed** - zero tolerance
- Fix the code, don't suppress the linter
- Enforced by `task web:check-ignores` in CI and pre-commit hooks

### 5. Git Hooks - ALWAYS ENFORCED

**NEVER bypass git hooks:**

- ❌ **NEVER use `git commit --no-verify`** - no exceptions
- ❌ **NEVER use `git push --no-verify`** - no exceptions

**Pre-commit hooks enforce:**

- Code formatting (`task fmt`, `task web:lint:fix`)
- Linting with auto-fix
- Module tidying (`task go:mod:tidy`, `pnpm install`)
- Code duplication check (`task duplication`)
- File length check (`task file-length`)
- Zero biome-ignore comments (`task web:check-ignores`)

**Pre-push hooks enforce:**

- Full CI suite (`task ci`)
- All tests must pass
- All builds must succeed

### 6. Security Scanning

**Required security checks:**

- `govulncheck` - Check for known Go vulnerabilities
- `shellcheck` - Validate all shell scripts
- `npm audit` - Check for npm package vulnerabilities

## 🚨 PROJECT PHILOSOPHY - READ THIS FIRST

**This is a greenfield project with ZERO active deployments.**

- **NO legacy code support** - We delete old code, we don't maintain it
- **NO backwards compatibility** - We break things freely to make them better
- **NO migration paths** - There's nothing to migrate from
- **NO "for compatibility" code** - Especially not for tests

**When tests fail after a refactor:**

- ✅ **CORRECT**: Update the tests to match the new code
- ❌ **WRONG**: Add compatibility shims to make old tests pass

**When code needs to change:**

- ✅ **CORRECT**: Delete the old code, write the new code, update everything that breaks
- ❌ **WRONG**: Keep the old code around "just in case" or "for backward compatibility"

This project moves fast and breaks things. That's intentional. If you find yourself writing "backward compatibility" or "legacy support" code, **STOP** - you're doing it wrong.

## Documentation Structure

This file contains high-level architecture and task commands. Component-specific details are in:

- **`web/CLAUDE.md`** - Web frontend (React, Vite, Playwright testing, CSP)
- **`internal/gateway/CLAUDE.md`** - Gateway API server (Go, auth, RBAC, proxy)
- **`cmd/rack-gateway/CLAUDE.md`** - CLI client (multi-rack, OAuth flow)
- **`mock-oauth/CLAUDE.md`** - Mock OAuth server for testing
- **`docs/`** - Detailed documentation (configuration, database, Convox reference)

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

## 🛠 Task Runner Overview

**Taskfile Structure:**

- `Taskfile.yml` - Root taskfile that includes all others
- `Taskfile.go.yml` - Go-specific tasks (build, test, lint, e2e)
- `Taskfile.web.yml` - Web frontend tasks (build, test, lint, e2e)
- `Taskfile.db.yml` - Database tasks (migrate, reset)
- `Taskfile.docker.yml` - Docker orchestration tasks
- `Taskfile.mock-oauth.yml` - Mock OAuth server tasks
- `taskfiles/docker-stack.yml` - Shared Docker stack helpers

**Key Workflows:**

- `task ci` → runs `lint`, `generate:test`, and `build`, guaranteeing every codegen step, linter, unit test, and build artifact stays in sync.
- `task generate` (and any task that depends on it) runs `task go:generate` followed by `task web:generate`; this regenerates the OpenAPI spec, TypeScript models, API client, and other autogenerated files automatically.
- Every lint/test/build task fan outs through `task go:generate` and `task web:generate` first; the Task runner caches them on the relevant sources, so schema/type regeneration happens automatically and only when inputs change.
- Docker orchestration is surfaced through `task docker:*` helpers; use these instead of invoking `docker compose` directly (they coordinate dev vs preview stacks, readiness checks, and migrations).
- Database tasks in `Taskfile.db.yml` handle migrations and resets for dev/test databases.
- The mock OAuth service has its own Taskfile; use `task mock-oauth:*` commands (e.g., `task mock-oauth:lint`) when you need to touch that package.

## 🔍 DEBUGGING CI FAILURES

When GitHub Actions CI fails, use these commands to fetch and analyze logs locally:

```bash
# Wait for the CI run to complete
wait-for-github-actions

# If there's a failure, fetch the logs
fetch-github-actions-logs

# Search for errors in the downloaded logs
rg -i 'error|fail' tmp/ci-logs

# View a specific failing job's log
less tmp/ci-logs/<run-id>_<job-name>.log
```

The `fetch-github-actions-logs` script downloads logs for all failing jobs to `tmp/ci-logs/` where you can analyze them with `rg`, `grep`, or any text tools. The `tmp/` directory is gitignored.

**Workflow:**

1. Push changes and run `wait-for-github-actions` to monitor the CI run
2. If CI fails, run `fetch-github-actions-logs` to download failure logs
3. Use `rg` to search for the actual error messages
4. Fix the issues and repeat

## 🌳 AST-GREP - Code Search and Extraction

`ast-grep` is a structural code search tool that understands Go syntax via AST (Abstract Syntax Tree) matching.

### Three-Step Refactoring Technique

The pattern is simple and repeatable:

1. **Extract functions with ast-grep scripts** - Use exact function signatures to reliably extract code
2. **Fix imports automatically** - Run `goimports -w .` to add/remove imports
3. **Verify compilation** - Run `go build` to catch any issues

This approach is dramatically faster than manual refactoring and much more reliable.

#### Real-World Example: Breaking Down admin.go

**Step 1: Analyze the File Structure**

First, identify logical groupings by listing all methods:

```bash
ast-grep --pattern 'func (h *AdminHandler) $NAME($$) $$' internal/gateway/handlers/admin.go \
  --json=compact | jq -r '.[].text' | grep -E '^func \(h \*AdminHandler\)' | \
  sed 's/func (h \*AdminHandler) //' | sed 's/(.*//' | sort
```

This reveals clear categories like User Management, API Tokens, Sessions, Settings, and Audit Logging.

**Step 2: Run Extraction Scripts**

Example for user management:

```bash
#!/bin/bash
set -e
output="internal/gateway/handlers/admin_users.go"

cat > "$output" << 'EOF'
package handlers

import (
    "github.com/gin-gonic/gin"
)

EOF

for func in "ListUsers" "GetUser" "CreateUser" "notifyUserCreated" \
            "DeleteUser" "UpdateUserProfile" "UpdateUserRoles" \
            "LockUser" "UnlockUser"; do
  ast-grep --pattern "func (h *AdminHandler) $func(\$\$\$) \$\$\$" \
    internal/gateway/handlers/admin.go --json=compact | \
    jq -r '.[0].text' >> "$output"
  echo -e "\n" >> "$output"
done
```

**Tips:**

- Use exact names for reliability
- Use `$$` wildcards for params and bodies
- Keep imports minimal - let goimports handle them

**Step 3: Extract the Core Struct**

Extract the core struct and constructor into a lean core file, then let goimports fix the imports across all files.

**Step 4: Verify Everything**

Run all scripts, fix imports, and verify with:

```bash
goimports -w .
go build ./...
```

Any missing types or constants can then be added back manually.

#### Why This Works

- ✅ Extracts complete function bodies with all comments and formatting
- ✅ Works for functions of any size (even 200+ line functions)
- ✅ Preserves exact code structure
- ✅ goimports automatically fixes imports
- ✅ Can extract multiple functions to one file in a single script
- ✅ Uses jq to parse JSON output cleanly
- ✅ Repeatable process for any large file

### Key Learnings

**1. File Extension Matters**

- ast-grep ONLY processes `.go` files by default
- Files with `.bak` or other extensions are ignored
- Use `--no-ignore hidden` flag OR copy to a `.go` file first

**2. Pattern Syntax**

- `$VAR` - matches a single AST node (like `$FUNC`, `$TYPE`)
- `$$` - matches zero or more AST nodes (parameters, statements, etc.)
- Meta variables must be UPPERCASE: `$FUNC` ✅, `$func` ❌

**3. Limitations**

- `$$` CANNOT be used in return type positions - causes parse errors
- Pattern must be valid Go code that tree-sitter can parse
- For complex signatures, use concrete types instead of wildcards
- Struct extraction with `$$` doesn't work well - just copy the struct manually

**4. Working Examples**

```bash
# ✅ Extract a function with exact signature
ast-grep run -l go -p 'func (h *Handler) forwardRequest(w http.ResponseWriter, r *http.Request, rack config.RackConfig, path string, authUser *auth.AuthUser) (int, error)' internal/gateway/proxy/handler.go --json=compact | jq -r '.[0].text'

# ✅ Match function with concrete return type
ast-grep run -l go -p 'func (h *Handler) logAudit($R, $AL) error' -l go file.go

# ❌ FAILS - $$ in return type position
ast-grep run -l go -p 'func (h *Handler) $FUNC($$) $$ { $$ }' file.go
# Error: "Multiple AST nodes are detected"

# ❌ FAILS - .bak extension ignored
ast-grep run -l go -p 'package proxy' file.go.bak
# Returns nothing (file ignored)

# ✅ WORKS - proper .go extension
ast-grep run -l go -p 'package proxy' file.go
# file.go:1:package proxy
```

**5. Debug Mode**

Use `--debug-query=pattern` to see how ast-grep parses your pattern:

```bash
ast-grep run -p 'func (h *Handler) logAudit($$) $$' -l go --debug-query=pattern file.go
```

This shows the AST tree and reveals `ERROR` nodes where the pattern is malformed.

**6. Best Practices**

For extracting Go functions:

1. Know the exact signature (return types, parameter types)
2. Use specific types instead of `$$` for return values
3. Use `$$` ONLY for parameter lists and function bodies
4. Verify file has `.go` extension
5. Use `--json=compact | jq -r '.[0].text'` to get clean output
6. Always run `task go:imports` after extraction
7. For structs, just copy them manually - ast-grep struct extraction is unreliable

## ⚠️ QUALITY CHECKLIST - MUST PASS BEFORE MARKING TASKS COMPLETE

**NEVER mark a task as "completed" unless this passes:**

### Run Full CI Suite

```bash
task ci
```

This runs ALL linters, typechecks, unit tests, builds, and E2E tests. **Must pass completely before marking work complete.**

### 🔧 Build Requirements

**⛔ FORBIDDEN: Never use `go build` directly - creates unwanted binaries in root**

- If CI fails remotely, use `fetch-github-actions-logs` to download and analyze failure logs

### 🧪 Testing Rules for AI

**CRITICAL RULES - AI MUST FOLLOW:**

1. ❌ **NEVER run `task dev`** – that workflow wires up Overmind and long-lived services meant for humans only.
2. ✅ You **may** run targeted test tasks (`task go:test`, `task web:test`, `task web:e2e`, `task go:e2e`, etc.) whenever they help validate the change. Be mindful of runtime and resource usage, but there is no blanket prohibition.
3. ✅ `task lint:fix` (and other lint/format helpers) remain safe and encouraged.

**Preferred workflow:**

- Use `task go:test` for Go unit/integration coverage.
- Use `task web:test` for web unit coverage.
- When an end-to-end scenario needs verification, run the focused command (for example `task web:e2e -- --grep "manage MFA"`).
- Leverage `task ci` for the full pre-merge sweep once individual pieces are green.

**Two development modes:**

1. **Overmind dev mode (Procfile.dev)** - Used by `task dev`, runs locally with hot reload via air:

   - Gateway API: `http://localhost:8447`
   - Web UI: `http://localhost:5223`
   - Mock OAuth: `http://localhost:3345`
   - Mock Convox: `http://localhost:5443`
   - Database: Docker container (rack-gateway-postgres-1) on port 55432

2. **Docker dev mode** - Used by `task docker:up`, runs everything in Docker:
   - Gateway API: `http://localhost:8448` (docker profile uses different port)
   - Web UI: `http://localhost:5224`
   - Mock OAuth: `http://localhost:3346`
   - Mock Convox: `http://localhost:5444`
   - Database: Docker container (rack-gateway-postgres-1) on port 55432

**Health checks (Overmind mode):**

- `curl http://localhost:8447/api/v1/health` - Gateway health check passes
- `curl http://localhost:3345/health` - Mock OAuth health check passes
- `curl http://localhost:5443/health` - Mock Convox health check passes

### 🚀 Production Readiness

- `task docker` - Docker build command passes

**If ANY of these fail, the task is NOT complete. Fix all issues before marking done.**

> ⚠️ **Task Runner Caveat** > `task ci` streams a _lot_ of output and the CLI truncates earlier sections once buffers fill. If the command exits non-zero, assume something failed even if the tail end looks successful. Scroll back (or re-run with logging to a file) to find the first error—don’t treat a noisy, partially hidden failure as a pass.

When the aggregated output is overwhelming, break the workflow into focused passes before re-running `task ci`:

- `task lint:fix`
- `task go:test`
- `task web:test`
- `task go:e2e`
- `task web:e2e`

Only after every step above is green should you run `task ci` again as the final confirmation. If it still fails, rinse and repeat.

## Project Overview

This is an authentication and authorization proxy for self-hosted Convox racks. It sits between users and the Convox API, adding:

- Google Workspace OAuth authentication
- Role-based access control (RBAC)
- Complete audit logging with automatic secret redaction
- Deploy approval workflows for CI/CD

## Architecture Philosophy

**One Gateway Per Rack (Single-Tenant Design)**

Each gateway instance is deployed alongside a single Convox rack. The gateway is not a multi-tenant SaaS service - it's infrastructure that runs directly next to the Convox API it protects.

- **Gateway server**: Single-tenant, deployed per rack, proxies to exactly one Convox API
- **CLI client**: Multi-rack aware, can switch between multiple gateways using `--rack` flag
- **No remote rack management**: Each gateway only knows about its own rack
- **Future**: Will use SSM parameter store to fetch rack credentials directly from AWS

**Example deployment:**

```
Production Rack:
  Convox API (port 5443) <-> Gateway (port 8447)

Staging Rack:
  Convox API (port 5443) <-> Gateway (port 8447)

Developer CLI:
  ~/.config/rack-gateway/config.json stores:
    - production: https://gateway-prod.example.com (session token)
    - staging: https://gateway-staging.example.com (session token)
```

## Architecture

```
Developer Machine -> Gateway Server (per rack) -> Convox Rack API
                     |
                     v
                  Audit Logs -> CloudWatch

Flow:
1. Developer runs: rack-gateway apps --rack staging
2. CLI loads session token from ~/.config/rack-gateway/config.json
3. CLI sets RACK_URL with session token and calls real convox CLI
4. Request goes to Gateway API Server with session token auth
5. Gateway validates session token and checks RBAC permissions
6. Gateway proxies to THE Convox rack using actual token
7. Gateway logs request to CloudWatch
8. Response flows back through gateway to developer
```

## Key Implementation Details

See component-specific CLAUDE.md files for detailed implementation information:

- `internal/gateway/CLAUDE.md` - Auth, RBAC, proxy, audit logging
- `cmd/rack-gateway/CLAUDE.md` - CLI OAuth flow, multi-rack config
- `web/CLAUDE.md` - React SPA, CSP, testing

## Configuration

**Environment Management**: This project uses `mise` for environment variable management, NOT .env files.

- `mise.toml` - Project-level configuration (checked into git)
- `mise.local.toml` - Local overrides (gitignored, create your own)
- `mise.local.toml.example` - Template for local configuration

See [docs/CONFIGURATION.md](docs/CONFIGURATION.md) for complete environment variable reference.

## Code Structure

```
internal/gateway/         - Gateway API server (see internal/gateway/CLAUDE.md)
  auth/                   - OAuth + session management
  rbac/                   - RBAC manager and policies
  proxy/                  - Request forwarding logic
  audit/                  - Structured logging + redaction
  middleware/             - HTTP middleware (security, CSRF, sessions)
  ui/                     - Admin web interface (serves SPA)
cmd/rack-gateway/         - CLI client (see cmd/rack-gateway/CLAUDE.md)
web/                      - React SPA frontend (see web/CLAUDE.md)
mock-oauth/               - Mock OAuth server for testing (see mock-oauth/CLAUDE.md)
```

## Security & Production Readiness

See `internal/gateway/CLAUDE.md` for detailed security considerations including sessions, CSRF, CSP, and TLS configuration.

## Web Frontend

See `web/CLAUDE.md` for detailed web development guidelines, testing procedures, and CSP requirements.

## Refactor & Organization Policy (Important)

- Never optimize for “don’t break what’s working” when the structure is wrong. Prefer the obviously better organization and implement it decisively.
- Proactively refactor for clarity and maintainability without waiting for prompts when the intent is clear.

When in doubt, choose the straightforward, well‑named, maintainable structure — even if it means removing or renaming existing files. Don’t defer obvious organization or code quality improvements.

## 📋 Task Commands - ALWAYS USE THESE, NEVER RAW COMMANDS

**CRITICAL: Always use `task` commands instead of raw commands. Never use grep, pipes, or manual command construction.**

**IMPORTANT: All task commands automatically handle their dependencies. You NEVER need to manually rebuild or restart services before running tests. For example:**

- `task web:e2e` automatically rebuilds the gateway, restarts docker containers, and runs tests
- `task go:test` automatically downloads dependencies and runs tests
- `task docker:up` automatically builds all images before starting containers

### 🎯 Primary Commands

| Command         | Description                        | When to Use                                 |
| --------------- | ---------------------------------- | ------------------------------------------- |
| `task ci`       | Run ALL linters, tests, and builds | **ALWAYS before marking any task complete** |
| `task dev`      | Start dev stack and follow logs    | Starting development environment            |
| `task test`     | Run all tests                      | Quick test run during development           |
| `task lint`     | Run all linters and typecheck      | Before committing code                      |
| `task lint:fix` | Auto-fix linting issues            | When linters report fixable issues          |
| `task build`    | Build all binaries                 | Creating release artifacts                  |

### 🐳 Docker Development

| Command             | Description                   |
| ------------------- | ----------------------------- |
| `task docker:up`    | Start full dev stack          |
| `task docker:down`  | Stop and remove containers    |
| `task docker:reset` | Reset dev stack (recreate DB) |
| `task docker:logs`  | Tail logs for dev stack       |
| `task docker:wait`  | Wait for stack readiness      |

### 🔧 Go Development

| Command           | Description                                  |
| ----------------- | -------------------------------------------- |
| `task go:build`   | Build all Go binaries                        |
| `task go:lint`    | Run Go linters (vet/fmt/staticcheck)         |
| `task go:test`    | Run Go unit/integration tests (uses test DB) |
| `task go:e2e`     | Run CLI E2E tests                            |
| `task go:imports` | Fix all Go imports with goimports            |

### 🌐 Web Development

See `web/CLAUDE.md` for detailed commands and testing procedures.

| Command                   | Description                                           |
| ------------------------- | ----------------------------------------------------- |
| `task web:build`          | Build web SPA                                         |
| `task web:lint:fix`       | Auto-fix web linting issues (preferred over web:lint) |
| `task web:test`           | Run Vitest unit tests                                 |
| `task web:e2e`            | Run Playwright E2E tests (rebuilds containers)        |
| `task web:lint:typecheck` | TypeScript type checking only                         |

### 🧪 E2E Testing

| Command        | Description                          |
| -------------- | ------------------------------------ |
| `task e2e`     | Run ALL E2E tests                    |
| `task web:e2e` | Web E2E against dedicated test stack |
| `task go:e2e`  | CLI E2E against dedicated test stack |

### 🎭 Mock Services

| Command                 | Description             |
| ----------------------- | ----------------------- |
| `task mock-oauth:dev`   | Run mock OAuth server   |
| `task mock-oauth:lint`  | Lint mock OAuth code    |
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

# ❌ WRONG - Don't use lint without fix
task web:lint    # Use web:lint:fix instead
task go:lint     # Use go:lint:fix or lint:fix instead
```

**ALWAYS DO THIS:**

```bash
# ✅ CORRECT - Use task commands
task go:test
task build
task web:test
task ci  # Run everything, see full output

# ✅ CORRECT - Always use lint:fix, not lint
task lint:fix      # Fix all linting issues
task web:lint:fix  # Fix web linting issues
```

## Pre-Push Checks

**Before ANY push or marking tasks complete:**

```bash
task ci
```

This runs:

- Web Biome lint via `pnpm lint`
- Go vet/fmt/staticcheck
- Go unit and integration tests (uses isolated test databases)
- Web unit tests (Vitest)
- Web and CLI E2E tests (Playwright + scripts)
- Full builds of all components

**If `task ci` doesn't pass completely, the task is NOT done.**

## Database Maintenance

See `docs/DATABASE_MAINTENANCE.md` for complete database maintenance procedures including migrations and resets.

### Running SQL Queries

**Development database:**

```bash
docker exec -i rack-gateway-postgres-1 psql -U postgres -d gateway_dev -c "YOUR_SQL_QUERY"
```

**Test database:**

```bash
docker exec -i rack-gateway-postgres-1 psql -U postgres -d gateway_test -c "YOUR_SQL_QUERY"
```

**Examples:**

```bash
# Check deploy approval requests
docker exec -i rack-gateway-postgres-1 psql -U postgres -d gateway_dev -c "SELECT id, message, status, created_at FROM deploy_approval_requests ORDER BY created_at DESC LIMIT 5;"

# Check users
docker exec -i rack-gateway-postgres-1 psql -U postgres -d gateway_dev -c "SELECT id, email, role FROM users;"

# Check API tokens
docker exec -i rack-gateway-postgres-1 psql -U postgres -d gateway_dev -c "SELECT id, name, created_by_email, role FROM api_tokens WHERE deleted_at IS NULL;"

# Check MFA methods for a user
docker exec -i rack-gateway-postgres-1 psql -U postgres -d gateway_dev -c "SELECT id, user_id, type, created_at FROM mfa_methods WHERE user_id = 1;"

# List all tables
docker exec -i rack-gateway-postgres-1 psql -U postgres -d gateway_dev -c "\dt"

# Describe table structure
docker exec -i rack-gateway-postgres-1 psql -U postgres -d gateway_dev -c "\d+ deploy_approval_requests"
```

## Important Instructions

- Don't leave old code lying around. When you see it, tidy it.
- We never maintain backwards-compatibility shims or legacy fallbacks.
- ALWAYS run `task ci` before claiming that any work is complete!

## Directory Structure

```
.
├── cmd/                          # Go binaries
│   ├── gateway/main.go           # Gateway API server entrypoint
│   ├── mock-convox/main.go       # Mock Convox API for testing
│   └── rack-gateway/main.go      # CLI client (see CLAUDE.md)
├── config/                       # Configuration files
│   ├── cli/                      # CLI config templates
│   └── cli-e2e/                  # E2E test configs
├── docs/                         # Documentation
│   ├── CONFIGURATION.md          # Environment variables
│   ├── CONVOX_REFERENCE.md       # Convox API reference
│   ├── DATABASE_MAINTENANCE.md   # Database operations
│   └── images/                   # Documentation images
├── internal/                     # Go internal packages
│   ├── cli/                      # CLI implementation (all commands)
│   │   ├── sdk/                  # SDK client
│   │   └── webauthn/             # WebAuthn for CLI
│   ├── convox/                   # Convox-specific utilities
│   ├── gateway/                  # Gateway server (see CLAUDE.md)
│   │   ├── app/                  # Application setup
│   │   ├── audit/                # Audit logging
│   │   ├── auth/                 # OAuth + sessions + MFA
│   │   ├── config/               # Configuration
│   │   ├── db/                   # Database layer and migrations
│   │   ├── email/                # Email notifications
│   │   ├── handlers/             # HTTP handlers (auth, API, MFA)
│   │   ├── middleware/           # HTTP middleware
│   │   ├── openapi/              # OpenAPI/Swagger generation
│   │   ├── proxy/                # Convox API proxy
│   │   ├── rbac/                 # Role-based access control
│   │   ├── routes/               # Route definitions
│   │   ├── security/             # Security notifications
│   │   └── token/                # API token service
│   ├── integration/              # Integration tests
│   └── tools/                    # Code generation tools
├── mock-oauth/                   # Mock OAuth server (see CLAUDE.md)
├── scripts/                      # Utility scripts
├── taskfiles/                    # Task runner configs
├── web/                          # React SPA frontend (see CLAUDE.md)
│   ├── e2e/                      # Playwright E2E tests
│   ├── public/                   # Static assets
│   ├── src/
│   │   ├── api/                  # API client (auto-generated)
│   │   ├── components/           # React components
│   │   ├── contexts/             # React contexts
│   │   ├── lib/                  # Utilities
│   │   └── pages/                # Page components
│   └── test-results/             # Playwright test artifacts
└── goober/                       # CSS-in-JS library (vendored)
```
