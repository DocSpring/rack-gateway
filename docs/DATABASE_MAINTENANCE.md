# Database Maintenance Commands

The `rack-gateway` binary has a set of maintenance subcommands for managing the Postgres schema and resetting development databases. These subcommands live in `cmd/gateway/main.go` and reuse the same environment-driven connection logic as the API server.

## `rack-gateway migrate`

Use this command whenever you need to apply pending migrations without starting the API server. It:

- Builds the Postgres DSN from the standard variables (`RGW_DATABASE_URL`, `GATEWAY_DATABASE_URL`, `DATABASE_URL`, or libpq-style `PG*`).
- Applies every migration in `internal/gateway/db/migrations/` exactly once via the `schema_migrations` ledger.
- Leaves existing data untouched and is safe to run repeatedly; it will only bring the schema up to date.

Example:

```bash
rack-gateway migrate
```

If the database cannot be reached or a migration fails, the command exits with a non-zero status and prints the failure reason.

## `rack-gateway reset-db`

This command **drops all gateway tables and recreates them from scratch**. Use it for disposable development databases or in exceptional production incidents where you explicitly want to wipe all data.

Safety guards enforced by the binary:

- You must export `RESET_RACK_GATEWAY_DATABASE=DELETE_ALL_DATA` or the command aborts immediately.
- The migration metadata (`rgw_internal_metadata`) must indicate a development database **or** you must set `DISABLE_DATABASE_ENVIRONMENT_CHECK`. This prevents accidental production wipes.
- When the database has never been initialized, you must run with `DEV_MODE=true` (or set the disable flag) so fresh installs default to development.
- After the reset, the command reapplies the full migration set and writes the current environment flag based on `DEV_MODE`.

Example development reset:

```bash
export RESET_RACK_GATEWAY_DATABASE=DELETE_ALL_DATA
export DEV_MODE=true
rack-gateway reset-db
```

Example production/staging emergency reset:

```bash
export RESET_RACK_GATEWAY_DATABASE=DELETE_ALL_DATA
export DISABLE_DATABASE_ENVIRONMENT_CHECK=1
rack-gateway reset-db
```

Only use this when you truly want to delete **all** gateway data (users, tokens, audit logs). The command exits with an error if any guard fails or if a drop/migration step fails. After completion it reapplies migrations and runs the normal bootstrap seeding (admin accounts, seeded roles, protected env vars).

### Task shortcuts (Docker dev stack)

When you're running the local Docker dev profile, the Taskfile provides convenience wrappers:

- `task docker:db:migrate` executes `rack-gateway migrate` inside the `gateway-api-dev` container (it ensures the service is running first).
- `task docker:db:reset` runs the destructive reset with the required guard (`RESET_RACK_GATEWAY_DATABASE=DELETE_ALL_DATA`). Set `DISABLE_DATABASE_ENVIRONMENT_CHECK=1` in your shell before invoking the task if you need to bypass the environment check.

Both tasks expect the Compose dev services to be available; they will start the gateway container if needed but leave the rest of the stack running.

## `rack-gateway help`

`rack-gateway help` (or `-h`/`--help`) prints a concise summary of the available top-level commands:

```
rack-gateway commands:
  (no args)            Start the API server
  migrate             Apply database migrations
  reset-db            Drop and recreate the database (requires env guards)
```

Keep this document in sync with new subcommands so operators and other contributors have a single reference for safe database operations.
