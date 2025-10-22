## Task 007 – internal/gateway/db/sessions.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/007-db-sessions`
Branch: `agents/007-db-sessions`

### Scope
- Eliminate the duplicated transaction blocks for session create/update flows in `internal/gateway/db/sessions.go` (≈43-line clones).
- Introduce shared helpers to manage transaction setup, row scanning, and expiration calculations so CRUD operations reuse a single implementation path.
- Preserve existing session semantics (revocation, TTL updates, MFA metadata). Update the session test suites to cover the new helpers.

### Suggested approach
1. Extract a `withSessionTx` helper or a reusable query builder that accepts callbacks for the specific mutation logic.
2. Centralize column lists and scanning into a dedicated struct to avoid repeating manual assignments.
3. Ensure the refactor keeps proper audit logging and error wrapping for session actions; extend tests covering session rotation and revocation.

### Required checks
- `task go:test`
- Any session-specific tests affected by the change (skip `task duplication`; supervisor runs it centrally)

Fix lint or formatting issues before committing.
