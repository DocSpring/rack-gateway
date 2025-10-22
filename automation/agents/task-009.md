## Task 009 – internal/gateway/db/audit_logs.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/009-db-audit`
Branch: `agents/009-db-audit`

### Scope
- Eliminate duplicated insert/update sequences in `internal/gateway/db/audit_logs.go` (16–20 line clones flagged by jscpd).
- Extract helper(s) for building audit log rows and aggregations without changing behaviour or tamper-proof guarantees.

### Suggested approach
1. Identify the shared insert logic (for audit events and aggregation) and centralize it in private helper functions.
2. Ensure helpers keep transaction boundaries and redaction rules intact.
3. Update/extend tests that touch audit logging to cover the new helper path.

### Required checks
- `task go:test`
- Any audit-specific tests impacted by the refactor (skip `task duplication`; supervisor runs it centrally)

Keep file length under 500 lines; split into additional files if needed.
