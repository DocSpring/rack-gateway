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

# Create gateway_dev database if it doesn't exist
compose_cmd exec -T postgres psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname = 'gateway_dev'" | grep -q 1 || \
  compose_cmd exec -T postgres psql -U postgres -c "CREATE DATABASE gateway_dev;"

# Setup audit roles in gateway_dev
echo "Setting up audit roles..."
DEV_DB_URL="postgres://postgres:postgres@localhost:55432/gateway_dev?sslmode=disable"
DATABASE_URL="$DEV_DB_URL" ./scripts/setup-audit-roles.sh

# Run migrations
if [ -f ./bin/rack-gateway-api ]; then
  echo "Running migrations on gateway_dev..."
  DATABASE_URL="$DEV_DB_URL" ./bin/rack-gateway-api migrate
fi
