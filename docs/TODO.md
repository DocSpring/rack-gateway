# Convox Gateway TODO (trimmed)

The project is feature-complete for our needs. Remaining actionable items are minimal and focused on polish and CI.

## Completed recently

- Email notifications via Postmark (and dev logger): new user, token created (owner + admins BCC).
- CSRF, cookie/session hardening, admin UI refinements, audit retention/cleanup.
- WebSocket proxy for exec/logs with command capture.
- CLI improvements: logout removes rack, new `web` command to open UI.
- Mock Convox: added POST `/apps/{app}/releases` returning a `Release` (fixes `convox env set`), proper promote endpoint.
- Audit action mapping fixes: `releases.get`, `releases.create`, `releases.promote`.
- CI: CLI E2E covers login, rack/apps/ps, exec/run, and env set flows.

## Short backlog

- CI: Ensure pipelines can connect through the gateway and deploy.
  - Document usage in your pipeline: set `RACK_URL=https://convox:<api-token>@<gateway-host>` and run `convox ...`.
  - Optionally use the wrapper: `convox-gateway convox deploy` after `convox-gateway login`.
- Audit UI: consider server-side pagination for very large datasets.
- RBAC mapping: continue to refine/verify edge routes; add tests as needed.

## Nice-to-have

- Log level toggle for DB stdout mirror vs request logs.

If you want to expand scope later (metrics, SSO providers, etc.), capture that in a new proposal. For now, this TODO reflects a stable, shippable state.
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
- `TOKEN_DEFAULT_EXPIRY_DAYS` - Remove; tokens do not expire by default
- `SLACK_CHANNEL` - Override default Slack channel (default: #infrastructure)
