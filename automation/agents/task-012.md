## Task 012 – internal/gateway/github/client.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/012-github-client-dedupe`
Branch: `agents/012-github-client-dedupe`

### Scope
- Deduplicate the repeated GitHub request setup/response parsing logic across `verifyCommitOnBranch`, `getBranch`, `findPRForBranch`, and `PostPRComment`.
- Provide a reusable helper that prepares requests with auth headers, executes them, enforces expected status codes, and optionally decodes JSON into a target struct.
- Retain existing error messages (including body strings) so callers continue to surface actionable details.

### Suggested approach
1. Create a `doRequest` helper that accepts method, path, body (optional), expected status code(s), and an optional decode target; centralise header setting and error decoration inside it.
2. Update the four call sites to use the helper, ensuring special cases (e.g., `http.StatusNotFound` handling in `getBranch` / `verifyCommitOnBranch`) stay intact.
3. Add unit tests with an `httptest.Server` that cover success, non-200 responses, and JSON decode errors to validate the helper.

### Required checks
- `task go:test`
- Any new helper-specific tests

Keep the helper private to the package and maintain the existing 30s timeout configuration on the client.
