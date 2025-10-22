## Task 006 – internal/gateway/db/mfa.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/006-db-mfa`
Branch: `agents/006-db-mfa`

### Scope
- Remove the duplicated SQL fragments and transaction scaffolding inside `internal/gateway/db/mfa.go` that jscpd highlighted (29–42 line clones).
- Build parameterised helpers for common select/update patterns so we have a single entry point for MFA method queries, updates, and lock transitions.
- Keep all public `db.MFA*` APIs and behaviours identical. Add or update tests to verify rate limiting, locking, and method lifecycle after refactoring.

### Suggested approach
1. Identify recurring SQL snippets (e.g., select by user+type, conditional upserts) and encapsulate them with helper functions.
2. Use small private structs or query builders to avoid repeating column lists; ensure context cancellation and error wrapping remain intact.
3. Update tests under `internal/gateway/auth/mfa` or DB packages to exercise the new helpers, especially account locking and audit metadata.

### Required checks
- `task go:test`
- Any additional MFA/db-focused tests you touch (skip `task duplication`; supervisor runs it centrally)

Resolve lint/formatting issues before committing.
