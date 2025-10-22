## Task 004 – internal/gateway/handlers/deploy_approval_admin.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/004-deploy-approval-admin-dedupe`
Branch: `agents/004-deploy-approval-admin-dedupe`

### Scope
- Remove the repeated list/create/update handler branches flagged by jscpd in `internal/gateway/handlers/deploy_approval_admin.go`.
- Consolidate approval filtering, pagination, and response shaping into reusable helpers so each handler method focuses on routing/validation only.
- Preserve RBAC checks, audit logging, and existing JSON response structure. Update or extend tests around deploy approvals to cover the new helpers.

### Suggested approach
1. Extract shared query builders or orchestrators (e.g., `loadApproval(ctx, id)`, `buildApprovalResponse(*db.DeployApprovalRequest)`), and reuse them across the admin endpoints.
2. Keep business logic in the handler package; avoid leaking DB layer responsibilities back into HTTP handlers.
3. Add focused unit tests or expand existing ones to cover the shared helper behaviour and edge cases (pagination, status filters, etc.).

### Required checks
- `task go:test`
- Any deploy-approval specific tests touched by the refactor (skip `task duplication`; supervisor runs it centrally)

If additional lint or formatting issues arise, fix them before committing.
