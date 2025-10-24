## Task 019 – internal/gateway/app/sentry_helpers.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/019-app-sentry-helpers`
Branch: `agents/019-app-sentry-helpers`

### Scope
- Deduplicate the Sentry capture helpers (`CaptureError`, `CaptureHTTPError`, `CaptureMessage`, `CaptureHTTPMessage`) that differ only by hub/request source (jscpd flagged paired 10-line clones).
- Maintain the existing fallback semantics when the gin hub is unavailable.
- Keep exported API intact so other packages continue calling the same functions.

### Suggested approach
1. Introduce a shared helper (e.g., `withSentryScopeFromRequest`) that accepts either `*gin.Context` or `*http.Request` to set level/tags before invoking a callback.
2. Use lightweight adapter functions to call the shared helper with appropriate capture call (exception vs message), minimizing duplication while preserving error/message distinctions.
3. Add/adjust tests under `internal/gateway/app` to cover both gin and raw request paths after refactor.

### Required checks
- `task go:test`
- Any new unit tests you add for the Sentry helpers
