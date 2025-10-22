# Duplication Remediation Backlog

This backlog is sourced from `task go:duplication` (which runs `jscpd` inside `internal/`) and additional scans across the CLI packages. Each entry below corresponds to a major duplication hotspot that we want to tackle with isolated agent worktrees.

**Workflow**
- Claim the next open task ID, create a worktree with `scripts/new-agent-worktree.sh`, and record your name/branch in the Status line.
- When you finish, update the entry with the result (`✅ merged`, `🔄 follow-up needed`, etc.) and remove the worktree.
- Feel free to add new items if you discover additional duplication while working on a task.

## Production Go Code (Priority A)

### 001 – `internal/gateway/proxy/http_proxy.go`
- **Status:** ✅ merged — commit `0ea4b06` (merge of `f587d59`)
- **Why flagged:** Duplicate request/response forwarding branches around lines 269–350 (jscpd shows 10–12 line overlaps within the same file and with `middleware/debug_logging.go`).
- **Goal:** Extract shared helper(s) for header copying, body streaming, and response logging so the proxy handler has a single canonical path.
- **Notes:** Keep CSP, audit logging, and denial logic intact; add focused unit tests if factoring changes control flow.
- **Outcome:** Extracted new `internal/gateway/httputil` helpers, updated proxy + middleware to reuse them, added comprehensive tests, and added `-buildvcs=false` to Go build tasks to support worktrees.

### 002 – `internal/gateway/middleware/security.go` & `internal/gateway/handlers/static.go`
- **Status:** ✅ merged — commit `23e56d4` (merge of `b95f071`)
- **Why flagged:** CSP / header configuration duplicated between middleware and static handler (23-line overlap reported).
- **Goal:** Centralize security header construction (likely in middleware package) so both routes reuse it.
- **Outcome:** Added shared `middleware/util.go` with `ClientIPFromRequest`, removed duplicates from security/static handlers, and introduced thorough tests covering header/IP extraction scenarios.
- **Checks:** `task go:test`, `task go:duplication`.

### 003 – `internal/gateway/middleware/mfa_enrollment.go`
- **Status:** ✅ merged — commit `2bb3c11` (merge of `854bf07`)
- **Why flagged:** Repeated validation/enrollment blocks (two 26-line clones) plus overlap with `handlers/mfa_helpers.go`.
- **Goal:** Factor shared enrolment orchestration and error handling into helpers; ensure gRPC/HTTP responses retain same messaging.
- **Outcome:** Extracted shared user-load helper, centralized enforcement checks in `db/mfa.go`, updated middleware/handlers to call the shared logic, and reduced duplication without altering behaviour (DB migration tests still fail due to pre-existing missing `locked_at` column).

### 004 – `internal/gateway/handlers/deploy_approval_admin.go`
- **Status:** ✅ merged — commit `dfbdb5a`
- **Why flagged:** Multiple large clones (22–40 lines) for list/create/update flows.
- **Goal:** Introduce reusable query/build helpers (e.g., filter builders, paginator) so the handler methods are thin.
- **Tests:** Existing handler tests should be rewritten to cover new helpers—keep behaviour identical.
- **Outcome:** Added shared auth/validation/audit helpers in `deploy_approval_helpers.go`, refactored admin handlers to use them, and verified with `task go:test`.

### 005 – `internal/gateway/handlers/auth_mfa_management.go`
- **Status:** ☐ unassigned
- **Why flagged:** Several 30–40 line duplicates for enabling/disabling MFA methods.
- **Goal:** Extract a method-orchestrating helper (e.g., `withMethod(userID, methodType, func(*MFAMethod) error)`) and consolidate response writers.

### 006 – `internal/gateway/db/mfa.go`
- **Status:** ☐ unassigned
- **Why flagged:** Repeated SQL fragments for MFA method queries and updates (42-line and 29-line clones).
- **Goal:** Build parameterised helpers (e.g., generic upsert, select) or use query builders to kill the copy/paste.
- **Notes:** Touch migrations/tests carefully—DB semantics must not change.

### 007 – `internal/gateway/db/sessions.go`
- **Status:** ✅ merged — commit `f8d9b7b`
- **Why flagged:** Two large clones around session insert/update logic (43-line blocks).
- **Goal:** Share transaction scaffolding between create/update paths. Consider moving to common `withTx` helper.
- **Outcome:** Introduced `sessionScanner` helpers to centralize row parsing and refactored the session create/update paths to use them. Verified via `task go:test` after merge.

### 008 – `internal/gateway/db/deploy_approval_requests.go`
- **Status:** ☐ unassigned
- **Why flagged:** Duplicated query building for approval updates (13–14 line clones).
- **Goal:** Consolidate into a single `updateStatus` helper with parameters (status, audit info, actor).

### 009 – `internal/gateway/db/audit_logs.go`
- **Status:** ☐ unassigned
- **Why flagged:** 16–20 line clones for audit log insertion.
- **Goal:** Create reusable insert builder or template method; maintain redact logic.

### 010 – `internal/gateway/db/users.go`
- **Status:** ☐ unassigned
- **Why flagged:** Repeated user lookup snippets (16 line clone, plus overlaps with later blocks).
- **Goal:** Deduplicate SELECT + scan logic; consider moving to generic helper.

### 011 – `internal/gateway/email/postmark.go`
- **Status:** ☐ unassigned
- **Why flagged:** Two 19-line blocks sending templated emails.
- **Goal:** Parameterize template selection; share request-building code.

### 012 – `internal/gateway/github/client.go`
- **Status:** ☐ unassigned
- **Why flagged:** 15-line duplication for request execution.
- **Goal:** Wrap API calls in reusable `doRequest` helper with consistent error decoration.

### 013 – `internal/gateway/handlers/integrations_slack.go`
- **Status:** ☐ unassigned
- **Why flagged:** Several 17–27 line clones across POST/PUT flows.
- **Goal:** Factor option-building & response shaping into helper functions; unify error pathways.

### 014 – `internal/gateway/handlers/settings_helpers.go`
- **Status:** ☐ unassigned
- **Why flagged:** 11–17 line clones inside helper functions.
- **Goal:** Share repeated `findSetting` / `composeResponse` logic via a single helper; tests exist in `settings_test.go` and should be updated.

### 015 – `internal/gateway/handlers/auth_mfa_verification.go`
- **Status:** ☐ unassigned
- **Why flagged:** 18-line clone of verification flow.
- **Goal:** Combine duplicated sections (likely GET vs POST) by extracting a base function that both call.

### 016 – `internal/gateway/handlers/api.go`
- **Status:** ☐ unassigned
- **Why flagged:** 12–17 line clones for request scaffolding.
- **Goal:** Evaluate if API command registration can share builder helpers.

### 017 – `internal/gateway/proxy/token_permissions.go`
- **Status:** ☐ unassigned
- **Why flagged:** 12-line duplicated block around validation.
- **Goal:** Replace with single helper used by both call sites.

### 018 – `internal/gateway/audit/logger.go`
- **Status:** ☐ unassigned
- **Why flagged:** 11-line clone inside log builder.
- **Goal:** Flatten repeated struct assembly.

### 019 – `internal/gateway/app/sentry_helpers.go`
- **Status:** ☐ unassigned
- **Why flagged:** 10-line duplication when populating tags.
- **Goal:** Provide a helper that formats DSN/environment consistently.

### 020 – `internal/gateway/app/app.go` vs `internal/gateway/routes/routes.go`
- **Status:** ☐ unassigned
- **Why flagged:** 12-line clone when registering routes.
- **Goal:** Centralize route registration in routes package.

## CLI Packages (Priority B)

### 021 – `internal/cli/mfa_helpers.go` ⇄ `internal/cli/mfa_verify.go`
- **Status:** ☐ unassigned
- **Why flagged:** After the recent split, the WebAuthn assertion flow still exists in both files (16-line clone).
- **Goal:** Move shared WebAuthn assertion + submission logic into a single helper (probably in `internal/cli/mfa_shared.go`). Keep worker prompt template aligned.

### 022 – `internal/cli/gateway_deploy_approvals.go`
- **Status:** ☐ unassigned
- **Why flagged:** Multiple 10–12 line duplicates for command scaffolding.
- **Goal:** Extract shared option parsing / API invocation.

### 023 – `internal/cli/convox_env.go`
- **Status:** ☐ unassigned
- **Why flagged:** 34-line clone for environment diff handling.
- **Goal:** Build a reusable diff helper or data structure.

### 024 – `internal/cli/config.go` ⇄ `internal/cli/gateway_api_tokens.go`
- **Status:** ☐ unassigned
- **Why flagged:** 14-line overlap for config save/load.
- **Goal:** Centralize config serialization in a helper.

### 025 – `internal/cli/auth.go`
- **Status:** ☐ unassigned
- **Why flagged:** 12-line duplicate login flow.
- **Goal:** Share login prompt/response formatting across commands.

### 026 – `internal/cli/logging/logging.go` ⇄ `internal/gateway/logging/logging.go`
- **Status:** ☐ unassigned
- **Why flagged:** Logging helper copied between CLI and gateway.
- **Goal:** Extract a shared logging utility (maybe under `internal/logging`).

## Tests & Supporting Code (Priority T)
Tests can tolerate some duplication, but the large clones make maintenance painful. These tasks focus on parameterising repeated fixtures.

### T001 – `internal/gateway/handlers/settings_test.go`
- **Status:** ☐ unassigned
- **Why flagged:** Dozens of 20–25 line clones; test cases differ only by input struct.
- **Goal:** Convert to table-driven tests that reuse helper functions.

### T002 – `internal/gateway/handlers/deploy_approval_requests_mfa_test.go`
- **Status:** ☐ unassigned
- **Why flagged:** Multiple 20+ line clones.
- **Goal:** Share scenario builders.

### T003 – `internal/gateway/auth/mfa/security_test.go`
- **Status:** ☐ unassigned
- **Why flagged:** Identical blocks for various failure cases.
- **Goal:** Table-drive the OTP attempt scenarios.

### T004 – `internal/gateway/middleware/security_test.go`
- **Status:** ☐ unassigned
- **Why flagged:** Triplicated CSP assertions.
- **Goal:** Add helper that takes expected headers and loops over states.

### T005 – `internal/gateway/handlers/integrations_slack_test.go`
- **Status:** ☐ unassigned
- **Goal:** Reduce duplicates by using shared fixtures for webhook setup.

### T006 – `internal/gateway/db/mfa.go` related tests
- **Status:** ☐ unassigned
- **Goal:** Once DB helpers are refactored (Task 006), ensure tests follow the new abstraction without duplicating SQL.

---

Add new entries as jscpd flags fresh clones. When a task is complete, flip the status checkbox, summarize the outcome, and link to the merge commit.
