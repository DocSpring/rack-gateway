#!/usr/bin/env bash
set -euo pipefail

# Use shared docker compose project if specified so worktrees reuse the main Postgres container.
compose_cmd() {
  if [ -n "${RGW_SHARED_DB_PROJECT:-}" ]; then
    docker compose --project-name "${RGW_SHARED_DB_PROJECT}" "$@"
  else
    docker compose "$@"
  fi
}

# Start postgres if not running
compose_cmd up -d postgres
# Wait for postgres to be ready
for _i in {1..30}; do
  if compose_cmd exec -T postgres pg_isready -U postgres >/dev/null 2>&1; then
    echo "Postgres is ready"
    break
  fi
  echo "Waiting for Postgres..."
  sleep 1
done

# Create gateway_test database if it doesn't exist
compose_cmd exec -T postgres psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname = 'gateway_test'" | grep -q 1 || \
  compose_cmd exec -T postgres psql -U postgres -c "CREATE DATABASE gateway_test;"
