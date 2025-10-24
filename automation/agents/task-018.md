## Task 018 – internal/gateway/audit/logger.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/018-audit-logger-slack`
Branch: `agents/018-audit-logger-slack`

### Scope
- Remove the duplicated Slack notification goroutine between `LogDBEntry` and `LogDBEntryWithContext` (jscpd sees 11-line clones).
- Ensure panic recovery + stderr logging remain intact.
- Preserve the behaviour where context is marked via `MarkAuditLogCreated` before notifications fire.

### Suggested approach
1. Extract a private method (e.g., `notifySlackAsync(*db.AuditLog)`) that wraps the goroutine, panic recovery, and error logging. Guard it so it no-ops when `slackNotifier` is nil.
2. Call the helper from both public methods after the database write succeeds (and context update happens in the latter).
3. Add/adjust focused tests under `internal/gateway/audit` to verify the helper is invoked when a notifier is set (you can stub the notifier to capture calls).

### Required checks
- `task go:test`
- Package-level tests you add for the audit logger
