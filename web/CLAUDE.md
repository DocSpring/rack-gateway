# Web Frontend - Claude Development Guide

## Tech Stack

- **Framework**: React 18 with TypeScript
- **Build Tool**: Vite 7
- **Routing**: TanStack Router
- **State Management**: TanStack Query (React Query)
- **Styling**: Tailwind CSS
- **UI Components**: Custom components built on Radix UI primitives
- **Testing**: Vitest (unit) + Playwright (E2E)
- **Linting**: Biome

## Testing

### Unit Tests (Vitest)

Run unit tests:

```bash
task web:test
# or directly:
bun run test
```

**What to test:**

- Router basepath handling for `/app`, including `/login` and `/auth/callback` routes
- Auth flows and API adapters (mock network; do not depend on browser)
- Critical UI/behavior for Users, Tokens, and Audit pages

**Testing policy:**

- Prefer fast feedback: write unit tests and run type checks before E2E
- Always run `bun run typecheck` and keep types clean
- When a web E2E test fails, first reproduce the failure with a focused unit test; fix it there, then re-run E2E

**Vitest module mocking - CRITICAL PATH RULES:**

- **ALWAYS use relative paths in `vi.mock()`** - Vitest treats `'@/lib/api'` and `'../lib/api'` as DIFFERENT modules
- **Match the import path the component uses** - If the component imports `'../../lib/api'`, your mock MUST use `'../lib/api'` (relative to test file)
- **Use `importOriginal` to preserve exports** - This prevents "No export found" errors:
  ```typescript
  vi.mock('../lib/api', async (importOriginal) => {
    const actual = await importOriginal<typeof import('../lib/api')>()
    return {
      ...actual,
      api: { ...actual.api, get: vi.fn(), post: vi.fn() }
    }
  })
  ```
- **Why this matters**: TypeScript path aliases (`@/`) are resolved at build time, but Vitest's module system treats aliased and relative paths as separate module instances. If the component imports via relative path but your test mocks via alias, the mock won't be applied.

**React Query testing patterns:**

- **Child components reading from cache**: When a child component uses `useQuery({ queryKey: QUERY_KEYS.TOKENS })` without a `queryFn`, it's reading from the parent's query cache. Add `enabled: false` to prevent React Query from trying to fetch:
  ```typescript
  const { data: tokens = [] } = useQuery<APIToken[]>({
    queryKey: QUERY_KEYS.TOKENS,
    enabled: false, // Just reading from cache, not fetching
  })
  ```
- **Suppress React Query cache warnings in tests**: When queries with `enabled: false` generate console errors about missing queryFn, suppress them:
  ```typescript
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation((message) => {
      if (typeof message === 'string' && message.includes('No queryFn was passed')) {
        return
      }
      console.error(message)
    })
  })
  ```
- **Mock pattern for parallel queries**: Use `mockImplementation` with URL routing instead of sequential `mockResolvedValueOnce`:
  ```typescript
  vi.mocked(api.get).mockImplementation((url: string) => {
    if (url.includes('/permissions')) {
      return Promise.resolve(mockPermissionMetadata)
    }
    return Promise.resolve(mockTokens)
  })
  ```

### E2E Tests (Playwright)

**Running E2E tests manually:**

You can run specific E2E tests directly with Playwright for faster iteration when **updating test files only**:

```bash
# Run specific test file
cd web
PLAYWRIGHT_BASE_URL=http://localhost:9447 bunx playwright test e2e/account-security.spec.ts

# Run specific test by name
PLAYWRIGHT_BASE_URL=http://localhost:9447 bunx playwright test --grep "user can manage MFA enrollment"
```

**CRITICAL: If you're updating application code (not just test files), you MUST rebuild first:**

- `task web:e2e` automatically rebuilds the gateway and restarts containers
- Running Playwright manually does NOT rebuild - you must run `task web:e2e` or `task docker:test:up` first
- 🚫 **Never** pass `--debug` (or any flag that opens the inspector) when running Playwright in automation — it blocks execution waiting for user interaction and will hang unattended runs.
- ⏱️ Always run Playwright under an explicit timeout (the automation harness sets `timeout_ms` on every command) so a stalled browser never hangs the job.

**Tip: Run specific tests with rebuild:**

```bash
# Run specific test(s) with automatic rebuild
task web:e2e -- --grep "test name pattern"
```

This rebuilds the app and runs only the tests matching the pattern.

**When to use manual Playwright commands:**

- ✅ Iterating on test selectors or assertions
- ✅ Debugging test failures (faster feedback loop)
- ✅ Writing new test cases

**When you MUST use `task web:e2e`:**

- ✅ Testing changes to application code (components, pages, API handlers)
- ✅ After modifying any `.tsx`, `.ts`, or `.go` files
- ✅ Before marking work as complete

### Type Checking

Always run type checking before committing:

```bash
bun run typecheck
```

## Task Commands

| Command                   | Description                                           |
| ------------------------- | ----------------------------------------------------- |
| `task web:build`          | Build web SPA                                         |
| `task web:lint:fix`       | Auto-fix web linting issues (preferred over web:lint) |
| `task web:test`           | Run Vitest unit tests                                 |
| `task web:e2e`            | Run Playwright E2E tests                              |
| `task web:lint:typecheck` | TypeScript type checking only                         |

**Never use `task web:lint`** - always use `task web:lint:fix` instead.

## Content Security Policy (CSP)

This project has strict CSP requirements. All inline styles must use nonces:

- `window.__nonce__` is set in the HTML template
- `window.__webpack_nonce__` is also set for libraries that detect it
- React components that generate inline styles (like some third-party components) may violate CSP
- Always use native HTML elements when possible (e.g., `<select>` instead of custom select components)

**CSP-related issues:**

- If you see CSP errors in the console, check if third-party components are generating inline styles
- Use native HTML form elements to avoid CSP violations
- react-hot-toast is configured with CSS variables to avoid inline styles

## Important Instructions

- Go handlers must never render HTML or plain text responses to browsers. All web views are rendered via the SPA; the gateway should serve static assets only.
- Don't leave old code lying around. When you see it, tidy it.
- We never maintain backwards-compatibility shims or legacy fallbacks.

## Refactor & Organization Policy

- Never optimize for "don't break what's working" when the structure is wrong. Prefer the obviously better organization and implement it decisively.
- Proactively refactor for clarity and maintainability without waiting for prompts when the intent is clear.

When in doubt, choose the straightforward, well-named, maintainable structure — even if it means removing or renaming existing files. Don't defer obvious organization or code quality improvements.
