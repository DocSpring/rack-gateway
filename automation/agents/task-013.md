## Task 013 – internal/gateway/handlers/integrations_slack.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/013-integrations-slack-dedupe`
Branch: `agents/013-integrations-slack-dedupe`

### Scope
- Eliminate the repeated RBAC + integration loading flows across the Slack handlers (authorize, callback, get, update, delete, list, test).
- Centralise common steps like permission checks, integration retrieval/decryption, and JSON response helpers without changing the public API responses or audit logging.
- Preserve development-time logging but prefer structured logging utilities over scattered `fmt.Printf` where possible.

### Suggested approach
1. Introduce shared helper(s) on `AdminHandler` that wrap the RBAC enforcement pattern and integration loading, returning consistent error responses.
2. Extract token decryption + Slack client creation into a single helper so `ListSlackChannels` and `TestSlackNotification` no longer reimplement it.
3. Add/adjust handler tests (see `internal/gateway/handlers/integrations_slack_test.go`) to cover the new helpers, especially error cases for missing integration and permission denials.

### Required checks
- `task go:test`
- Targeted handler tests touching Slack integrations

After refactoring, update `automation/duplication_tasks.md` with the outcome and keep files under the 500-line limit.
