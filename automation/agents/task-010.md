## Task 010 – internal/gateway/db/users.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/010-db-users`
Branch: `agents/010-db-users`

### Scope
- Remove the copy/pasted SELECT + scan logic inside `internal/gateway/db/users.go` (16-line clone plus overlaps).
- Introduce helper(s) for user row scanning and reuse them across fetch/update functions.

### Suggested approach
1. Factor out a shared `scanUser` helper that handles nullable fields and role decoding.
2. Update `GetUser`, `ListUsers`, and other entry points to use the helper without changing return types.
3. Ensure new helper lives in a separate file if needed to stay under length limits.

### Required checks
- `task go:test`
- Any additional user-focused tests affected by the refactor (skip `task duplication`; supervisor runs it centrally)

Respect the file length limit (500 lines) and reuse existing JSON parsing helpers where possible.
