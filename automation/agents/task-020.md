## Task 020 – internal/gateway/app/app.go vs internal/gateway/routes/routes.go

Worktree: `/Users/ndbroadbent/code/rack-gateway-worktrees/020-app-routes-bootstrap`
Branch: `agents/020-app-routes-bootstrap`

### Scope
- Eliminate the duplicated dependency wiring between `app.New()` / `App.setupRouter()` and `routes.Setup` (jscpd spotlights 12-line clones around route registration and middleware setup).
- Ensure route registration continues to live under `routes` while `app` focuses on dependency construction.
- Preserve existing middleware order (Sentry, request ID, security headers, etc.).

### Suggested approach
1. Move the overlapping bootstrap logic into a shared builder (e.g., a struct that collects dependencies and returns configured router + handlers). Keep the separation of concerns clear so tests remain straightforward.
2. Consider introducing a small `routes.Config` constructor in `app` instead of manually mirroring fields.
3. Update affected tests (app integration and route tests) to reflect any new helper signatures.

### Required checks
- `task go:test`
- Any integration tests you modify (e.g., app setup tests)
