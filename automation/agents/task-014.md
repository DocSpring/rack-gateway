## Task 014 – internal/gateway/handlers/settings_helpers.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/014-settings-helpers-dedupe`
Branch: `agents/014-settings-helpers-dedupe`

### Scope
- Reduce the duplication between `updateSettings`, `deleteSettings`, and `getSingleSettingResponse` when validating keys, fetching defaults, and composing responses.
- Ensure both global and app settings flows continue to honour the `settingsOperations` contract and return the same HTTP status codes/messages.
- Maintain audit attribution (user ID capture) and the existing error strings so frontend tests keep passing.

### Suggested approach
1. Extract helper(s) for validating requested keys against the allowed set and operations to eliminate repeated loops.
2. Introduce a shared function that builds the response map from a list of keys while handling default/lookup errors consistently.
3. Update tests in `internal/gateway/handlers/settings_test.go` (or add new ones) to exercise the helpers across both global and app contexts.

### Required checks
- `task go:test`
- Any newly added unit tests for settings helpers

Respect the 500-line limit and avoid introducing additional interfaces unless they simplify the duplication removal.
