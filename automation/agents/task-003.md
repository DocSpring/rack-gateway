## Task 003 – MFA enrollment flow de-duplication

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/003-mfa-enrollment`
Branch: `agents/003-mfa-enrollment`

### Scope
- Resolve the duplicated enrollment logic in `internal/gateway/middleware/mfa_enrollment.go` and overlap with `internal/gateway/handlers/mfa_helpers.go`.
- Factor out shared validation/enrollment orchestration so both middleware paths reuse the same helper(s).
- Preserve current behaviour for MFA enrollment responses and error messages.

### Suggested approach
1. Identify repeated blocks (two ~26-line clones) that prepare enrollment payloads and handle errors.
2. Extract helper(s) (e.g. `buildEnrollmentContext`, `withEnrollmentTransaction`) that can be called from both spots.
3. Ensure tests cover the helper; update existing tests (`mfa_stepup_*.go`, `handlers/mfa_*_test.go`) accordingly.

### Required checks
- `task go:test`
- Any additional targeted tasks required for MFA enforcement changes (skip `task duplication`; supervisor runs it centrally)

Do not compromise rate limits, logging, or security checks.
