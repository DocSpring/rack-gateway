## Task 008 – internal/gateway/db/deploy_approval_requests.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/008-deploy-approval-db`
Branch: `agents/008-deploy-approval-db`

### Scope
- Reduce the repeated update/select logic inside `internal/gateway/db/deploy_approval_requests.go` flagged by jscpd (13–14 line clones).
- Introduce shared helpers for status transitions (approve/reject/cancel) so the DB layer has single entry points.
- Keep behaviour identical—RBAC/audit expectations must not change.

### Suggested approach
1. Extract helper(s) that accept status + metadata parameters and reuse them across update methods.
2. Ensure new helper(s) live in the same package; update tests to cover them if necessary.
3. Watch the file length (500 lines max) and reuse existing json marshaling utilities.

### Required checks
- `task go:test`
- Any additional DB-focused tests touched by the refactor (skip `task duplication`; supervisor runs it centrally)

If lint or formatting issues arise, resolve them before committing.
