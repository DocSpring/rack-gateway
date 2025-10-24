## Task 017 – internal/gateway/proxy/token_permissions.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/017-token-permissions-dedupe`
Branch: `agents/017-token-permissions-dedupe`

### Scope
- Kill the duplicated deploy-approval lookup logic inside `evaluateAPITokenPermission` (jscpd reports ~12 line clones across the switch cases for build/process/release paths).
- Ensure the helper preserves HTTP status codes and error messaging (`deployApprovalError`).
- Keep existing debug logging minimal; feel free to funnel it through the helper instead of repeating format strings.

### Suggested approach
1. Extract a helper that constructs `db.DeployApprovalLookup` structs based on request context (build ID, process ID, release ID). The helper should return both the lookup and any early-deny error so each case reduces to 2–3 lines.
2. Centralize the “already processed” checks (object uploaded, build created) into dedicated validation helpers returning `deployApprovalError` instances.
3. Update or extend tests under `internal/gateway/proxy` (look for existing token permission tests) to cover at least one approval-gated path after refactor.

### Required checks
- `task go:test`
- Any targeted proxy tests you add
