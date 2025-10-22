## Task 001 – internal/gateway/proxy/http_proxy.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/001-proxy-dedupe`
Branch: `agents/001-proxy-dedupe`

### Scope
- Eliminate the duplicated request/response forwarding code blocks flagged by jscpd (~lines 269–350) inside `internal/gateway/proxy/http_proxy.go`.
- Remove the overlap with `internal/gateway/middleware/debug_logging.go` by centralising shared logic (header copying, logging payloads, replay-safe response handling).
- Maintain existing audit logging, CSP/security enforcement, and error handling semantics. Regression tests must continue to pass.

### Suggested approach
1. Create a small helper (e.g. `copyHeaders(dst, src http.Header)`, `forwardResponse(ctx, ... )`) inside the proxy package so both paths reuse it.
2. Ensure logging still redacts sensitive headers (`Authorization`, etc.).
3. Update any affected tests or add unit coverage to validate the refactored helper.

### Required checks
- `task go:test`
- Any additional targeted tasks noted during the refactor (skip `task duplication`; supervisor runs it centrally)

If additional lint failures arise, fix them before committing.
