# Convox Gateway TODO

## Build Health (2025-09-05)
- [x] Web linting: now clean (Biome passes). Changes:
  - Replaced Radix namespace imports with named imports in UI components.
  - Fixed TypeScript anys, unused params/privates, a11y and style nits; extracted regex constants in tests.
  - `web/package.json` scripts: `lint:fix` and `lint:unsafe` added.
- [ ] Go linting: `staticcheck` reports unused func `sha256Hash` at `internal/gateway/auth/jwt.go:80`. Remove or use it.
- [ ] Tests: All Go packages pass except `internal/gateway/auth`.
  - [ ] Failing test `TestOAuthHandler_UsesCustomBaseURL`: tries to reach `http://mock-oauth:3001` (DNS not available outside Docker). Options:
    - Use `localhost` and run `dev/mock-oauth/server.js` during tests, or
    - Stub OIDC discovery/HTTP client in `NewOAuthHandler`, or
    - Skip test unless `MOCK_OAUTH_BASE_URL` is resolvable.
- [x] Safe test wrapper: add guard file creation in `scripts/safe-test.sh` to satisfy `convoxguard` checks.

### Dev Environment Ports
- [x] Parameterize docker-compose ports via env with safe defaults:
  - `MOCK_OAUTH_PORT` (default 3345), `MOCK_CONVOX_PORT` (default 5443), `GATEWAY_PORT` (default 8447), `WEB_PORT` (default 5173)
- [x] Update compose env references (issuer/redirect, RACK_HOST) to use variables.
- [ ] Document `.env` use (optional) to persist local port choices.

## Phase 1: SQLite Database Implementation ✅ IN PROGRESS

### Database Setup
- [ ] Replace config.yml with SQLite database at `/app/data/db.sqlite`
- [ ] Auto-create database on first boot if it doesn't exist
- [ ] Seed admin user from `ADMIN_EMAIL` and `ADMIN_NAME` environment variables

### Database Schema
- [ ] Users table (email, name, roles, created_at, updated_at)
- [ ] API tokens table (token_hash, user_id, name, permissions, created_at, expires_at)
- [ ] Audit logs table (see below)

### User Management
- [ ] Migrate from config.yml to SQLite for user storage
- [ ] Add API token generation for CI/CD pipelines
- [ ] Add API token management endpoints (create, list, revoke)

## Phase 2: Audit Logging System

### Audit Log Schema
```sql
CREATE TABLE audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    user_email TEXT NOT NULL,
    user_name TEXT,
    action_type TEXT NOT NULL,  -- "convox_api", "user_management", "auth"
    action TEXT NOT NULL,        -- e.g., "env.get", "user.create", "auth.failed"
    resource TEXT,               -- e.g., "myapp", "staging", "user@example.com"
    details TEXT,                -- JSON with command details
    ip_address TEXT,
    user_agent TEXT,
    status TEXT,                 -- "success", "denied", "error", "blocked"
    response_time_ms INTEGER
);
```

### Audit Log Categories

#### Convox API Calls
- `nathan@docspring.com read the value of SECRET_TOKEN` (convox env get SECRET_TOKEN)
- `nathan@docspring.com read all environment variables` (convox env)
- `david@docspring.com set 3 environment variables: STRIPE_TOKEN, GOOGLE_TOKEN, FOOBARBAZ` (convox env set STRIPE_TOKEN=REDACTED ...)
- `DocSpring Bot requested rack information` (convox rack)
- `DocSpring Bot built a new release` (convox build)
- `ops@docspring.com attempted to delete app (DENIED)` (convox apps delete - blocked by RBAC)

#### User Management Actions
- `admin@docspring.com created user viewer@docspring.com with roles: viewer`
- `admin@docspring.com updated roles for deployer@docspring.com: [ops, deployer] -> [admin]`
- `admin@docspring.com removed user contractor@docspring.com`
- `admin@docspring.com created API token "CI/CD Pipeline" for role: cicd`
- `admin@docspring.com revoked API token "Old Pipeline Token"`
- `admin@docspring.com exported audit logs for date range 2024-01-01 to 2024-01-31`

#### Authentication & Authorization Events
- `unknown@hacker.com failed authentication - invalid JWT token`
- `expired@docspring.com authentication blocked - token expired`
- `viewer@docspring.com authorization denied - insufficient permissions for convox apps delete`
- `suspended@docspring.com authentication blocked - user account suspended`
- `attacker@evil.com multiple failed login attempts - rate limited`
- `john@docspring.com successful login via Google OAuth`
- `ci-bot@docspring.com authenticated via API token "GitHub Actions"`

### Audit Log Features
- [ ] Log all Convox API requests with user attribution
- [ ] Log all user management actions (create, update, delete users)
- [ ] Log all authentication attempts (success and failure)
- [ ] Log all authorization denials with reason
- [ ] Log API token usage and management
- [ ] Redact sensitive values (passwords, tokens) in audit logs
- [ ] Parse Convox commands to extract meaningful action descriptions
- [ ] Store command parameters (with redaction)
- [ ] Track source IP and user agent for security analysis
- [ ] Add audit log viewing endpoint for admin UI
- [ ] Create audit log retention policy (default 90 days)
- [ ] Add audit log export functionality (CSV, JSON)

## Phase 3: RBAC Enhancements

### New Role: CI/CD Pipeline
- [ ] Create `cicd` role with limited permissions:
  - `convox rack` (read-only)
  - `convox ps` (read-only)
  - `convox build` (create builds)
  - `convox run command` with restricted commands:
    - `bin/pre_release`
    - `run_as_deploy rake checks:verify_dns checks:generate_test_submission`
    - `rake stripe:prepare`
  - `convox releases promote`

### Enhanced Permission System
- [ ] Add command parameter validation to RBAC
- [ ] Block `convox run web` and `convox run worker` for all roles
- [ ] Restrict `convox run command "rails console"` to admin role only
- [ ] Add allowlist for specific rake tasks and commands per role

## Phase 4: Slack Notifications

### Slack Integration
- [ ] Add Slack webhook URL configuration (environment variable)
- [ ] Create notification service for sending Slack messages
- [ ] Add notification templates for different event types

### Events to Notify
- [ ] Environment variable reads (SECRET_*, API_*, TOKEN_*)
- [ ] Environment variable writes (all)
- [ ] Build creation
- [ ] Release promotion
- [ ] Command execution (convox run)
- [ ] Failed authentication attempts
- [ ] New user added/removed
- [ ] API token created/revoked

### Notification Format
```
🔐 *Environment Variable Access*
User: nathan@docspring.com
Action: Read SECRET_TOKEN
App: myapp
Rack: production
Time: 2024-01-15 10:30:45 UTC
```

## Phase 5: CLI User Management Commands

### New CLI Commands for Admins
- [ ] `convox-gateway users list` - List all users with their roles
- [ ] `convox-gateway users add <email> <name> --roles admin,deployer` - Add a new user
- [ ] `convox-gateway users update <email> --roles viewer,ops` - Update user roles
- [ ] `convox-gateway users remove <email>` - Remove a user
- [ ] `convox-gateway users show <email>` - Show detailed user info and permissions
- [ ] `convox-gateway tokens create --name "CI/CD Pipeline" --role cicd` - Create API token
- [ ] `convox-gateway tokens list` - List all API tokens
- [ ] `convox-gateway tokens revoke <token-id>` - Revoke an API token
- [ ] `convox-gateway audit <email>` - Show audit logs for a specific user
- [ ] `convox-gateway audit --since 24h` - Show recent audit logs

### CLI Command Security
- [ ] Require admin role for all user management commands
- [ ] Add confirmation prompts for destructive operations
- [ ] Log all admin CLI actions to audit log
- [ ] Return clear error messages for permission denied

## Phase 6: Real-time Permission Updates

### Instant Permission Revocation
- [ ] Implement session invalidation on permission change
- [ ] Add cache invalidation for user roles
- [ ] WebSocket or polling for real-time permission updates
- [ ] Force re-authentication on permission downgrade

### Testing Requirements
- [ ] Test: User with revoked permissions gets 403 immediately
- [ ] Test: User with changed role loses access to restricted commands instantly
- [ ] Test: API token revocation takes effect immediately
- [ ] Test: No grace period for permission changes
- [ ] Integration test: Change permissions mid-session and verify denial

## Phase 7: Mock Google OAuth for Development

### Mock OAuth Server
- [ ] Create mock Google OAuth server service in Docker Compose
- [ ] Implement `/authorize` endpoint that shows mock login page
- [ ] Implement `/token` endpoint for token exchange
- [ ] Support PKCE flow with code_verifier and code_challenge
- [ ] Return mock user info (configurable via environment)

### Mock OAuth Flow
1. User clicks "Sign in with Google" in dev mode
2. Redirected to mock OAuth server (http://localhost:8090/authorize)
3. Mock login page with preset test users:
   - admin@example.com (Admin role)
   - deployer@example.com (Deployer role)
   - viewer@example.com (Viewer role)
4. After selecting user, redirected back with auth code
5. Gateway exchanges code for mock JWT token
6. User is logged in with mock identity

### Mock OAuth Configuration
- [ ] Add `MOCK_OAUTH_ENABLED=true` environment variable
- [ ] Add `MOCK_OAUTH_URL=http://localhost:8090` for mock server
- [ ] Create docker-compose service for mock OAuth
- [ ] Add mock users configuration file
- [ ] Support both browser flow and CLI device code flow

### CLI Browser-Based Authentication Flow
- [ ] User runs: `convox-gateway login staging https://gateway.company.com`
- [ ] CLI generates random state token for CSRF protection
- [ ] CLI starts local HTTP server on random port (e.g., :8989) for callback
- [ ] CLI opens browser to: `https://gateway.company.com/auth/cli?state=<state>&callback_port=8989`
- [ ] User completes Google OAuth in browser
- [ ] Gateway redirects to: `http://localhost:8989/callback?token=<jwt>&state=<state>`
- [ ] CLI receives callback, validates state, extracts JWT
- [ ] CLI stores configuration in `CONVOX_GATEWAY_CLI_CONFIG_DIR/config.json`
- [ ] Browser shows success page: "Authorization complete. You can now close this window"
- [ ] CLI confirms: "Successfully logged in as user@company.com"

## Phase 8: Local Dev Rack E2E (Convox Development Rack)
- [x] Scaffold optional heavy E2E runner:
  - `scripts/e2e-devrack.sh` with checks for Convox, Docker, Minikube (>=1.29), Terraform.
  - `make e2e-devrack` target; gated by `E2E_DEV_RACK=1` env.
  - Installs/ensures rack `local`, deploys `convox-gateway`, runs `convox rack/system/ps` smoke checks.
- [ ] Decide on CI job: add a workflow that runs this behind a repository/branch flag (target runtime 3–5 minutes).


## Phase 8: Admin UI Enhancements

### Audit Log Viewer
- [ ] Create React component for viewing audit logs
- [ ] Add filtering by user, action, date range
- [ ] Add search functionality
- [ ] Export audit logs as CSV
- [ ] Real-time updates via WebSocket

### API Token Management UI
- [ ] Create/revoke API tokens
- [ ] View active tokens
- [ ] Set token expiration
- [ ] Define token permissions
- [ ] Show last used timestamp

### User Management UI
- [ ] List users with roles and last activity
- [ ] Add/edit/remove users
- [ ] Bulk operations (disable multiple users)
- [ ] Import users from CSV
- [ ] Show user session status

## Phase 9: Security Enhancements

### Rate Limiting
- [ ] Add rate limiting per user/token
- [ ] Different limits for read vs write operations
- [ ] Implement exponential backoff for failed auth

### Token Security
- [ ] Hash API tokens in database (never store plaintext)
- [ ] Implement token rotation reminders
- [ ] Add token usage analytics

## Implementation Order

1. **SQLite Database** (Priority 1)
   - Set up database connection
   - Create schema
   - Migrate user management from config.yml
   - Add database initialization on startup

2. **Audit Logging** (Priority 2)
   - Create audit log table
   - Add logging middleware
   - Implement log redaction
   - Create audit log queries

3. **API Tokens** (Priority 3)
   - Add token generation
   - Implement token authentication
   - Create CI/CD role and permissions

4. **Slack Notifications** (Priority 4)
   - Set up Slack client
   - Create notification templates
   - Add notification triggers

5. **UI Updates** (Priority 5)
   - Build audit log viewer
   - Add token management interface

## Testing Requirements

### Unit Tests
- [ ] Database initialization and migrations
- [ ] User CRUD operations
- [ ] API token validation
- [ ] Audit log creation and querying
- [ ] RBAC with command parameters
- [ ] Slack notification formatting

### Integration Tests
- [ ] Full CI/CD workflow with API token
- [ ] Audit log capture for all command types
- [ ] Permission denial logging
- [ ] Database persistence across restarts

## Migration Strategy

1. **Backward Compatibility**
   - Check for config.yml on startup
   - If exists, migrate to SQLite and rename to config.yml.migrated
   - Log migration status

2. **Data Migration**
   - Parse existing config.yml
   - Create users in database
   - Preserve roles and permissions
   - Generate migration report

## Environment Variables

### New Required Variables
- `ADMIN_EMAIL` - Initial admin user email
- `ADMIN_NAME` - Initial admin user name
- `SLACK_WEBHOOK_URL` - Slack incoming webhook for notifications
- `DATABASE_PATH` - SQLite database path (default: /app/data/db.sqlite)

### Optional Variables
- `AUDIT_LOG_RETENTION_DAYS` - Days to keep audit logs (default: 90)
- `TOKEN_DEFAULT_EXPIRY_DAYS` - Default API token expiry (default: 365)
- `SLACK_CHANNEL` - Override default Slack channel (default: #infrastructure)
