# Web E2E Tests - Claude Development Guide

## Overview

Web E2E tests use Playwright to test the complete application stack including the gateway API, web frontend, mock OAuth server, mock Convox API, and PostgreSQL database.

## Architecture

The E2E tests run against a **self-contained Docker test stack**:

- **Gateway API + Web Assets**: Single Docker image (`rack-gateway-api:test`) that includes:
  - Gateway API server (Go)
  - Built web assets served from `dist/` directory
- **Mock OAuth Server**: Simulates Google OAuth for authentication
- **Mock Convox API**: Simulates Convox rack API responses
- **PostgreSQL Database**: Test database (`gateway_test`)

All services run in Docker with isolated test ports to avoid conflicts with dev environment.

## Running Tests

### Full E2E Suite (Recommended)

```bash
task web:e2e
```

This command:

1. Rebuilds the Docker test image (includes gateway + web assets)
2. Starts the test stack with `docker compose`
3. Waits for services to be healthy
4. Runs all Playwright E2E tests
5. Reports results

**Use this when:**

- You've changed application code (`.tsx`, `.ts`, `.go` files)
- You want to run all E2E tests
- You're verifying a complete feature
- Before marking work as complete

### Rebuilding Test Stack Manually

```bash
task docker:test:up
```

Rebuilds and starts the test stack without running tests. The stack stays running until you stop it.

**Use this when:**

- You want to iterate on test files without rebuilding the app
- You need to debug tests manually
- You want to keep the stack running for multiple test runs

### Running Individual Tests (Fast Iteration)

Once the test stack is running (`task docker:test:up`), you can run individual tests:

```bash
# Run a specific test file
bunx playwright test e2e/account-security.spec.ts

# Run tests matching a pattern
bunx playwright test --grep "user can manage MFA enrollment"

# Run with UI mode for debugging
bunx playwright test --ui

# Run with headed browser (see what's happening)
bunx playwright test --headed e2e/login.spec.ts
```

**IMPORTANT**: Running individual tests does NOT rebuild the application. If you've changed app code (not just test files), you MUST rebuild first with `task docker:test:up` or `task web:e2e`.

**Use individual test runs when:**

- ✅ Iterating on test selectors or assertions (test code only)
- ✅ Debugging test failures
- ✅ Writing new test cases
- ✅ The application code hasn't changed

**You MUST use `task web:e2e` or rebuild when:**

- ❌ You've modified React components (`.tsx`)
- ❌ You've changed API handlers (`.go`)
- ❌ You've updated routing logic
- ❌ Any application code has changed

## Test Stack URLs

When tests run, services are available at:

- **Gateway API**: `http://localhost:9447`
- **Web UI**: `http://localhost:9447/app/`
- **Mock OAuth**: `http://localhost:9345`
- **Mock Convox API**: `http://localhost:5443`

The `PLAYWRIGHT_BASE_URL` is automatically set to `http://localhost:9447` by the test configuration.

### Parallel Shards (Local vs CI)

- **Local (`task web:e2e`)**: spins up **7 gateway instances** (`gateway-api-test[1-7]`) backed by **7 isolated databases** (`gateway_test`, `gateway_test_2`, …, `gateway_test_7`). Playwright fans out across the shards with seven workers by default, eliminating test cross-talk while keeping the runtime short.
- **CI (`task web:e2e` on GitHub Actions)**: automatically drops to a **single gateway/database** to reduce resource usage while preserving deterministic behaviour. Shard counts can be overridden via `WEB_E2E_SHARDS` if needed.

Each shard exposes the gateway + SPA on successive ports starting at `9447`. The Playwright fixtures automatically route each worker to its assigned shard and switch the per-worker database URL, so tests stay independent without any extra plumbing in spec files.

## Writing Tests

### Test Structure

```typescript
import { expect, test } from "./fixtures";
import { login } from "./helpers";

test("descriptive test name", async ({ page }) => {
  // Login helper handles OAuth flow
  await login(page);

  // Navigate and test
  await page.goto(WebRoute("rack"));
  await expect(page.getByRole("heading", { name: "Rack" })).toBeVisible();
});
```

### Test Fixtures

The `fixtures.ts` file provides:

- **WebAuthn mocking**: Prevents real hardware security key prompts
- **Error collection**: Captures console errors and page errors
- **Response logging**: Logs 4xx/5xx responses for debugging
- **HTML snapshots**: Saves page HTML on test failure
- **Error suppression**: Filters expected 401s and MFA-related errors

### Helper Functions

`helpers.ts` provides common test utilities:

#### `login(page, options?)`

Handles the complete OAuth login flow:

```typescript
// Login as default admin user with MFA enrollment
await login(page);

// Login as different user
await login(page, { userCardText: "Test User" });

// Login without auto-enrolling MFA (for testing enrollment flow)
await login(page, { autoEnrollMfa: false });
```

#### `ensureMfaEnrollment(page)`

Programmatically enrolls user in TOTP MFA:

```typescript
await ensureMfaEnrollment(page);
```

#### `resetMfaFor(email)`

Removes all MFA methods for a user (useful for testing enrollment flows):

```typescript
await resetMfaFor("admin@test.com");
```

#### `enforceMfaFor(email)`

Sets MFA enforcement flag for a user:

```typescript
await enforceMfaFor("admin@test.com");
```

#### `clearStepUpSessions()`

Expires all step-up authentication sessions:

```typescript
await clearStepUpSessions();
```

### Route Helpers

Use `WebRoute()` and `APIRoute()` helpers for consistent URLs:

```typescript
import { WebRoute, APIRoute } from "@/lib/routes";

// Web routes
await page.goto(WebRoute("rack"));
await page.goto(WebRoute("users"));
await page.goto(WebRoute("account/security"));

// API routes (for fetch calls in tests)
const response = await page.request.get(APIRoute("auth/mfa/status"));
```

## Common Test Patterns

### Testing Protected Routes

```typescript
test("admin page requires authentication", async ({ page }) => {
  await page.goto(WebRoute("users"));

  // Should redirect to login
  await expect(page).toHaveURL(/app\/login/);
});
```

### Testing MFA Enforcement

```typescript
test("sensitive action requires MFA code", async ({ page }) => {
  await login(page);

  // Navigate to sensitive page
  await page.goto(WebRoute("api-tokens"));

  // Try to create token without MFA code
  await page.getByRole("button", { name: /Create Token/i }).click();

  // Should show MFA prompt
  await expect(page.getByText(/Enter your authentication code/i)).toBeVisible();
});
```

### Testing Forms

```typescript
test("user can create API token", async ({ page }) => {
  await login(page);

  await page.goto(WebRoute("api-tokens"));
  await page.getByRole("button", { name: /Create Token/i }).click();

  // Fill form
  await page.getByLabel(/Token Name/i).fill("test-token");
  await page.getByLabel(/Role/i).selectOption("deployer");

  // Submit
  await page.getByRole("button", { name: /Create/i }).click();

  // Verify success
  await expect(page.getByText(/Token created successfully/i)).toBeVisible();
});
```

### Testing Navigation

```typescript
test("sidebar navigation works", async ({ page }) => {
  await login(page);

  // Navigate via sidebar
  await page.getByRole("link", { name: /Users/i }).click();
  await expect(page).toHaveURL(WebRoute("users"));

  await page.getByRole("link", { name: /Audit Logs/i }).click();
  await expect(page).toHaveURL(WebRoute("audit-logs"));
});
```

## Database Helpers

Tests can interact with the test database via helper functions in `db.ts`:

```typescript
import { resetMfaForUser, enforceMfaForUser } from "./db";

// Reset MFA for testing enrollment flows
await resetMfaForUser("admin@test.com");

// Enforce MFA requirement
await enforceMfaForUser("admin@test.com");
```

## Debugging Tests

### View Browser Interactions

```bash
# Run with headed browser
bunx playwright test --headed e2e/login.spec.ts

# Run with Playwright Inspector
bunx playwright test --debug e2e/login.spec.ts

# Run with UI mode (recommended)
bunx playwright test --ui
```

### View Test Results

After tests run, Playwright generates:

- Screenshots on failure
- Videos of test execution
- HTML snapshots of pages
- Trace files for debugging

View results:

```bash
bunx playwright show-report
```

### Check Service Logs

If tests fail mysteriously, check Docker logs:

```bash
# All services
docker compose logs

# Specific service
docker compose logs gateway-api-test
docker compose logs mock-oauth-test

# Follow logs in real-time
docker compose logs -f gateway-api-test
```

### Common Issues

**"ERR_CONNECTION_REFUSED"**:

- Test stack isn't running
- Run `task docker:test:up` first

**"Element not found" / Timeout errors**:

- Application code changed but image not rebuilt
- Run `task docker:test:up` or `task web:e2e`

**"Cannot find module" errors**:

- Web dependencies out of sync
- Run `bun install` in the web directory

**Tests pass locally but fail in CI**:

- Check if test depends on timing or race conditions
- Use `waitFor` and `expect.poll` for async assertions
- Check CI logs with `fetch-github-actions-logs` script

## Test Organization

Tests are organized by feature:

- `login.spec.ts` - Login flow and OAuth
- `oauth-flow.spec.ts` - Complete OAuth flow with MFA enrollment
- `account-security.spec.ts` - MFA management, trusted devices
- `mfa-enrollment-required.spec.ts` - MFA enrollment enforcement
- `users-edit-profile.spec.ts` - User profile management
- `manage.spec.ts` - Admin user management
- `global-settings.spec.ts` - System-wide settings
- `env.spec.ts` - Environment variable management
- `audit-aggregation.spec.ts` - Audit log features
- `csrf-protection.spec.ts` - CSRF token validation
- `cli-dialog.spec.ts` - CLI installation dialog
- `app-tabs.spec.ts` - Application tab navigation
- `nav-active.spec.ts` - Active navigation highlighting

## Best Practices

1. **Always use helpers**: Use `login()`, `WebRoute()`, `APIRoute()` for consistency
2. **Wait for elements**: Use `await expect(...).toBeVisible()` instead of arbitrary timeouts
3. **Use semantic selectors**: Prefer `getByRole`, `getByLabel`, `getByText` over CSS selectors
4. **Clean up state**: Each test should be independent (helpers like `resetMfaFor` help with this)
5. **Test user flows, not implementation**: Test what users see and do, not internal state
6. **Keep tests focused**: One test should verify one user scenario
7. **Use descriptive test names**: Test names should describe the expected behavior

## CI/CD Integration

The E2E tests run automatically in GitHub Actions on every push. If CI fails:

1. Use `wait-for-github-actions` to monitor the run
2. Use `fetch-github-actions-logs` to download failure logs
3. Search logs with `rg -i 'error|fail' tmp/ci-logs`
4. Reproduce locally with `task web:e2e`

## Performance Tips

- **Parallel execution**: Playwright runs tests in parallel by default
- **Reuse authentication**: The `login()` helper is optimized to reuse sessions
- **Skip unnecessary waits**: Use Playwright's auto-waiting instead of `waitForTimeout`
- **Targeted test runs**: Run specific test files during development

## Related Documentation

- Main web guide: `/web/CLAUDE.md`
- Database maintenance: `/docs/DATABASE_MAINTENANCE.md`
- Task commands: Root `CLAUDE.md`
