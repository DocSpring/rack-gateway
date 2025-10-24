## Task 016 – internal/gateway/handlers/api.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/016-api-rack-context-dedupe`
Branch: `agents/016-api-rack-context-dedupe`

### Scope
- Remove the repeated rack setup / error-response scaffolding in `GetEnvValues`, `UpdateEnvValues`, and related helpers (jscpd flagged 12–17 line clones around `rackContext`, TLS errors, and audit wiring).
- Consolidate the duplicated audit logging blocks that differ only by resource/secrets flags.
- Preserve RBAC checks, secret masking, and audit response timing; only the duplication should disappear.

### Suggested approach
1. Introduce a private helper (e.g., `withRackContext(c, func(rackConfig config.RackConfig, tlsCfg *tls.Config) error)`) that wraps `rackContext` and emits the standard error responses. Reuse it from both env handlers.
2. Extract a small function to build the audit log payload (`newEnvAuditEntry(...)`) so success/denied paths stay consistent without copy/paste.
3. Keep handler length <100 lines by placing helpers near the file top or in a companion file if necessary. Update unit tests or add focused ones if behaviour changes.

### Required checks
- `task go:test`
- Any focused handler tests you add or update
