# Repository Guidelines

## Project Structure & Module Organization
- Go services: `cmd/gateway`, `cmd/cli`, `cmd/mock-convox`; shared code in `internal/…`.
- Web UI (Vite + React + TS): `web/` with tests in `web/src` and `web/e2e`.
- Config and dev data: `config/`, `data/`, `dev/`.
- Binaries output to `bin/` (`convox-gateway-api`, `convox-gateway`, `mock-convox`).

## Build, Test, and Development Commands
- Build all: `make build` (or `make all`).
- Go binaries: `make gateway`, `make cli`, `make mock`.
- Web app: `make web-build` (installs via `pnpm`, then builds).
- Run dev stack (Docker Compose): `make dev`; logs: `make dev-logs`; stop: `make dev-down`.
- Tests: `make test` (Go unit + integration + web); Go-only: `make test-go`, unit: `make test-unit`, integration: `make test-integration`; web: `make web-test`.
- Lint: `make lint` (Go vet/fmt/staticcheck + web Biome).

## Coding Style & Naming Conventions
- Go: standard formatting (`gofmt`), static analysis (`go vet`, `staticcheck`). Packages and files use lower_snake or lowercase; tests end with `_test.go`.
- Web: TypeScript, React, Vite. Use Biome for lint/format (`pnpm biome …`). Components in `web/src` use PascalCase filenames; hooks/utilities use camelCase.
- Hooks: `lefthook.yml` runs Go fmt/vet/tests and web Biome/typecheck on commit.

## Testing Guidelines
- Unit tests: place next to code as `*_test.go`. Run via `make test-unit`.
- Integration tests: `internal/integration` with `-tags=integration` (run `make test-integration`).
- Safe wrapper: Go tests run through `scripts/safe-test.sh` to back up/restore real Convox config. Do not bypass this locally.
- Web tests: `pnpm test` (Vitest), coverage: `pnpm test:coverage`, E2E: `pnpm e2e` (Playwright).

## Commit & Pull Request Guidelines
- Commits: concise imperative subject (<=72 chars), meaningful body when needed. Group related changes; avoid noisy reformat-only commits.
- PRs: clear description, scope of change, test coverage notes, and any config/env impacts. Link issues; add screenshots for UI changes. Ensure `make lint` and `make test` pass.

## Security & Configuration Tips
- Never commit secrets. Put local env in `mise.local.toml` (ignored). Example vars: `GOOGLE_CLIENT_ID`, `GOOGLE_ALLOWED_DOMAIN`, `CONVOX_GATEWAY_DB_PATH`.
- Be careful with `~/.convox`; tests temporarily relocate it. See `scripts/backup-convox-config.sh` and `scripts/restore-convox-config.sh`.
