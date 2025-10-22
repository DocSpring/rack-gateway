## Task 011 – internal/gateway/email/postmark.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/011-email-postmark-dedupe`
Branch: `agents/011-email-postmark-dedupe`

### Scope
- Remove the duplicate request-building logic between `PostmarkSender.Send` and `PostmarkSender.SendMany`.
- Introduce a shared helper that composes the JSON payload and performs the HTTP request while preserving `MessageStream`, optional HTML body, and Bcc handling.
- Keep behaviour identical for noop / logger senders; only the Postmark-specific code needs refactoring.

### Suggested approach
1. Extract a private helper on `PostmarkSender` that accepts a primary recipient and optional Bcc slice, builds the payload map, marshals once, and posts to the Postmark API.
2. Ensure error handling remains the same (non-2xx status returns a formatted error) and avoid introducing new `//nolint` comments.
3. Add focused tests (e.g., using an `httptest` server) that assert the helper sets headers and payload correctly for single-recipient and multi-recipient cases.

### Required checks
- `task go:test`
- Any additional package-level tests added for the new helper

Aim to keep the file under 500 lines and document the helper with a brief comment if behaviour is non-obvious.
