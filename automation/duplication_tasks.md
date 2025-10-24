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
- **Status:** 🔁 follow-up needed — residual clones still detected between list/approve flows (jscpd run 2024-10-18)
- **Why flagged:** Multiple large clones (22–40 lines) for list/create/update flows.
- **Goal:** Introduce reusable query/build helpers (e.g., filter builders, paginator) so the handler methods are thin.
- **Tests:** Existing handler tests should be rewritten to cover new helpers—keep behaviour identical.
- **Outcome:** Added shared auth/validation/audit helpers in `deploy_approval_helpers.go`, refactored admin handlers to use them, and verified with `task go:test`.

### 005 – `internal/gateway/handlers/auth_mfa_management.go`
- **Status:** 🔁 follow-up needed — jscpd still reports duplicates around method orchestration (run 2024-10-18)
- **Why flagged:** Several 30–40 line duplicates for enabling/disabling MFA methods.
- **Goal:** Extract a method-orchestrating helper (e.g., `withMethod(userID, methodType, func(*MFAMethod) error)`) and consolidate response writers.
- **Outcome:** Added shared helpers in `mfa_helpers.go` (`loadMFAUserContext`, `parseIDParam`, `loadMFAMethod`, `loadTrustedDevice`) and `auth_helpers.go` (`handleMFADisablement`, `auditMFAUpdate`). Refactored DeleteMFAMethod, UpdateMFAMethod, RevokeTrustedDevice, and UpdatePreferredMFAMethod to use these helpers. Eliminated 30-40 line clones while preserving audit logging, RBAC enforcement, and error messages. All handler tests pass.

### 006 – `internal/gateway/db/mfa.go`
- **Status:** 🔁 follow-up needed — shared query helpers still leave 13-line clones (jscpd 2024-10-18)
- **Why flagged:** Repeated SQL fragments for MFA method queries and updates (42-line and 29-line clones).
- **Goal:** Build parameterised helpers (e.g., generic upsert, select) or use query builders to kill the copy/paste.
- **Notes:** Touch migrations/tests carefully—DB semantics must not change.
- **Outcome:** Added shared scanning helpers and extracted trusted device operations into dedicated files, keeping functionality intact and passing `task go:test`.

### 007 – `internal/gateway/db/sessions.go`
- **Status:** ✅ merged — commit `f8d9b7b`
- **Why flagged:** Two large clones around session insert/update logic (43-line blocks).
- **Goal:** Share transaction scaffolding between create/update paths. Consider moving to common `withTx` helper.
- **Outcome:** Introduced `sessionScanner` helpers to centralize row parsing and refactored the session create/update paths to use them. Verified via `task go:test` after merge.

- **Status:** ✅ merged — commit `ddc0d21`
- **Why flagged:** Duplicated query building for approval updates (13–14 line clones).
- **Goal:** Consolidate into a single `updateStatus` helper with parameters (status, audit info, actor).
- **Outcome:** Extracted shared `updateApprovalStatus` helper, eliminating 30 lines of duplication across approve/reject methods (both by ID and by PublicID). Split scanning logic into `deploy_approval_scan.go` to maintain <500 line limit per file (469 + 223 lines). Passes golangci-lint, no new duplication detected. Note: Pre-existing test failures related to missing `locked_at` column in migrations are unrelated to this refactoring.

### 009 – `internal/gateway/db/audit_logs.go`
- **Status:** ✅ merged — included in this supervisor iteration
- **Why flagged:** 16–20 line clones for audit log insertion.
- **Goal:** Create reusable insert builder or template method; maintain redact logic.
- **Outcome:** Added shared filter/query helpers and split scanning into `audit_logs_queries.go`, eliminating duplicate insert/filter logic while preserving tamper-evident guarantees (`task go:test`).

### 010 – `internal/gateway/db/users.go`
- **Status:** 🔁 follow-up needed — `users_scan.go` still has overlapping scan helpers (jscpd 2024-10-18)
- **Why flagged:** Repeated user lookup snippets (16 line clone, plus overlaps with later blocks).
- **Goal:** Deduplicate SELECT + scan logic; consider moving to generic helper.
- **Outcome:** Added shared scanning helpers in `users_scan.go`, updated user lookup/list functions to reuse them, and confirmed behaviour with `task go:test`.

### 011 – `internal/gateway/email/postmark.go`
- **Status:** ✅ merged — commit `7c45e90`
- **Why flagged:** Two 19-line blocks sending templated emails.
- **Goal:** Parameterize template selection; share request-building code.
- **Outcome:** Introduced `PostmarkSender.sendEmail` to build the payload + request once, and rewired `Send`/`SendMany` to call it (preserving HTML/Bcc handling and existing error flow).

### 012 – `internal/gateway/github/client.go`
- **Status:** ✅ merged — commit `fa48cf0`
- **Why flagged:** 15-line duplication for request execution.
- **Goal:** Wrap API calls in reusable `doRequest` helper with consistent error decoration.
- **Outcome:** Added `Client.doRequest` for shared header setup, error handling, and JSON decoding; refactored the four duplicated call sites to use it and introduced extensive httptest coverage for the helper.

- **Status:** ✅ merged — commit `1bb90ec`
- **Why flagged:** Several 17–27 line clones across POST/PUT flows.
- **Goal:** Factor option-building & response shaping into helper functions; unify error pathways.
- **Outcome:** Introduced `enforceIntegrationPermission`, `loadSlackIntegration`, and `createSlackClient` helpers to centralize RBAC checks, integration lookup, and client creation; refactored all Slack handlers to use them and added focused gin/dbtest coverage for authorization and error paths.

### 014 – `internal/gateway/handlers/settings_helpers.go`
- **Status:** ✅ merged — commit `c2035f4`
- **Why flagged:** 11–17 line clones inside helper functions.
- **Goal:** Share repeated `findSetting` / `composeResponse` logic via a single helper; tests exist in `settings_test.go` and should be updated.
- **Outcome:** Added `validateSettingKeys` and `buildSettingsResponse` helpers, refactored update/delete/get flows to reuse them, and preserved response semantics for unrestricted endpoints while returning full group payloads for scoped handlers.

### 015 – `internal/gateway/handlers/auth_mfa_verification.go`
- **Status:** ✅ merged — commit `639deff`
- **Why flagged:** 18-line clone of verification flow.
- **Goal:** Combine duplicated sections (likely GET vs POST) by extracting a base function that both call.
- **Outcome:** Introduced `verifyMFAAndComplete` helper orchestrating verification, trusted-device handling, session updates, and notifications; rewired TOTP/WebAuthn handlers to use it, added regression tests, and fixed login-complete notifier gating.

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

### 021 – `internal/gateway/handlers/sentry.go` ⇄ `internal/gateway/proxy/handler.go`
- **Status:** ☐ unassigned
- **Why flagged:** Shared Sentry capture logic duplicated between handler utilities and proxy error handling (12-line clone).
- **Goal:** Extract a common Sentry reporting helper so both call sites share tagging, request context, and user metadata wiring.

### 022 – `internal/gateway/handlers/admin_audit.go`
- **Status:** ☐ unassigned
- **Why flagged:** Audit list/build helpers still contain mirrored logic (13-line clone) for assembling responses and authorization checks.
- **Goal:** Consolidate audit response construction into a reusable helper that preserves filtering and pagination behaviour.

### 023 – `internal/gateway/db/audit_logs.go` ⇄ `internal/gateway/db/audit_logs_queries.go`
- **Status:** ☐ unassigned
- **Why flagged:** Query + scan paths are duplicated across the two files (16–18 line clones) for building audit rollups.
- **Goal:** Move shared SQL + scanning code into a single helper (or reduce file split) so append-only logic lives in one place.

### 024 – `internal/gateway/db/api_tokens.go`
- **Status:** ☐ unassigned
- **Why flagged:** Multiple large clones (12–51 lines) for token CRUD flows, especially around audit logging and redaction.
- **Goal:** Introduce shared builders for token responses and audit events, keeping RBAC + redaction semantics untouched.

### 025 – `internal/gateway/auth/service.go`
- **Status:** ☐ unassigned
- **Why flagged:** Session/context hydration duplicates utility logic already present in `middleware/util.go` (19–21 line clones).
- **Goal:** Extract shared request context helpers so auth service and middleware reuse the same code paths without copying.

### 026 – `internal/convox/commands.go` ⇄ `internal/gateway/rbac/http_routes.go`
- **Status:** ☐ unassigned
- **Why flagged:** Route-to-permission mapping appears in both the Convox command registry and RBAC configuration (11-line clone).
- **Goal:** Centralize command metadata so CLI + gateway reference a single shared capability map.

## CLI Packages (Priority B)

### 031 – `internal/cli/mfa_helpers.go` ⇄ `internal/cli/mfa_verify.go`
- **Status:** ☐ unassigned
- **Why flagged:** After the recent split, the WebAuthn assertion flow still exists in both files (16-line clone).
- **Goal:** Move shared WebAuthn assertion + submission logic into a single helper (probably in `internal/cli/mfa_shared.go`). Keep worker prompt template aligned.

### 032 – `internal/cli/gateway_deploy_approvals.go`
- **Status:** ☐ unassigned
- **Why flagged:** Multiple 10–12 line duplicates for command scaffolding.
- **Goal:** Extract shared option parsing / API invocation.

### 033 – `internal/cli/convox_env.go`
- **Status:** ☐ unassigned
- **Why flagged:** 34-line clone for environment diff handling.
- **Goal:** Build a reusable diff helper or data structure.

### 034 – `internal/cli/config.go` ⇄ `internal/cli/gateway_api_tokens.go`
- **Status:** ☐ unassigned
- **Why flagged:** 14-line overlap for config save/load.
- **Goal:** Centralize config serialization in a helper.

### 035 – `internal/cli/auth.go`
- **Status:** ☐ unassigned
- **Why flagged:** 12-line duplicate login flow.
- **Goal:** Share login prompt/response formatting across commands.

### 036 – `internal/cli/logging/logging.go` ⇄ `internal/gateway/logging/logging.go`
- **Status:** ☐ unassigned
- **Why flagged:** Logging helper copied between CLI and gateway.
- **Goal:** Extract a shared logging utility (maybe under `internal/logging`).

### 037 – `internal/cli/convox_processes.go`
- **Status:** ☐ unassigned
- **Why flagged:** Command output rendering duplicates table-building logic across multiple functions (10-line clones).
- **Goal:** Factor shared formatting helpers for process listings.

### 038 – `internal/cli/convox_builds.go` ⇄ `internal/cli/convox_deploy.go`
- **Status:** ☐ unassigned
- **Why flagged:** Build/deploy commands share identical argument parsing + API invocation (10-line clone).
- **Goal:** Extract a helper for build/deploy command scaffolding to avoid drift.

### 039 – `internal/cli/gateway_test_auth.go` ⇄ `internal/cli/mfa_helpers.go`
- **Status:** ☐ unassigned
- **Why flagged:** MFA helper scaffolding reappears in the gateway auth command support file (12-line clone).
- **Goal:** Consolidate shared MFA helper logic into one module consumed by both files.

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
