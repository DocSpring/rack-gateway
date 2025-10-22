## Task 005 – internal/gateway/handlers/auth_mfa_management.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/005-auth-mfa-management`
Branch: `agents/005-auth-mfa-management`

### Scope
- Deduplicate the enable/disable MFA handler flows flagged by jscpd in `internal/gateway/handlers/auth_mfa_management.go` (30–40 line clones).
- Extract shared orchestration helpers for loading users/methods, validating state, and formatting responses so each handler focuses on routing.
- Preserve audit logging, RBAC enforcement, and error messaging semantics. Update unit tests that cover MFA management endpoints to match the new helpers.

### Suggested approach
1. Introduce helpers such as `withMFAMethod(ctx, userID, methodType, func(...) error)` or a response builder shared across enable/disable paths.
2. Factor any repeated DB lookups or logging into well-named functions inside the handlers package.
3. Extend existing tests (or add new ones) to cover the helper paths, especially edge cases like already-enabled methods or mismatched method types.

### Required checks
- `task go:test`
- Any MFA-specific test suites touched by the refactor (skip `task duplication`; supervisor runs it centrally)

If lint or formatting issues arise, resolve them before committing.
