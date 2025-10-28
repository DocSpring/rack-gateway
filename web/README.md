# Rack Gateway Web UI

Admin UI and client for Rack Gateway. Provides user management, API token management, and audit viewing.

## Development

- Run the full dev stack with Docker Compose:
  - `task dev`
  - Web UI: `http://localhost:${WEB_PORT:-5223}`
  - Gateway API: `http://localhost:${GATEWAY_PORT:-8447}`

- Run unit tests:
  - `bunx vitest --run`

- Run end‑to‑end tests (requires dev stack):
  - `task e2e:web:release`

## Tech Stack

- React + TypeScript (Vite)
- TanStack Query for data fetching
- Vitest for unit/integration tests
- Playwright for E2E
