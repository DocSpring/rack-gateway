## Task 002 – security headers deduplication

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/002-security-headers`
Branch: `agents/002-security-headers`

### Scope
- Remove the duplicated CSP/header construction between `internal/gateway/middleware/security.go` and `internal/gateway/handlers/static.go`.
- Introduce a shared helper that builds the security header set once (likely under the middleware package) and reuse it from both code paths.
- Ensure all existing security headers (CSP, HSTS, X-Frame-Options, etc.) remain intact and configurable.

### Suggested approach
1. Extract header-building logic into `securityHeaders(config)` or similar.
2. Update both middleware and static handler to call the helper.
3. Add/adjust tests verifying headers in both contexts (`security_test.go`, static handler tests if available).

### Required checks
- `task go:test`
- Any additional targeted tasks needed for the refactor (skip `task duplication`; supervisor runs it centrally)

Maintain zero lint violations and obey file/function length budgets.
