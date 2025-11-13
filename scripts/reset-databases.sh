#!/usr/bin/env bash
set -euo pipefail

compose_cmd() {
  if [ -n "${RGW_SHARED_DB_PROJECT:-}" ]; then
    docker compose --project-name "${RGW_SHARED_DB_PROJECT}" "$@"
  else
    docker compose "$@"
  fi
}

# If arguments provided, reset specific database
# Otherwise reset all databases
if [ $# -eq 2 ]; then
  DB_NAME="$1"
  GATEWAY_SERVICE="$2"
  DATABASES=("$DB_NAME")
  SERVICES=("$GATEWAY_SERVICE")
else
  DATABASES=(gateway_dev gateway_test)
  SERVICES=(gateway-api-dev gateway-api-test)
fi

echo "Stopping gateway services..."
for service in "${SERVICES[@]}"; do
  compose_cmd stop "$service" 2>/dev/null || true
  compose_cmd rm -f "$service" 2>/dev/null || true
done

echo "Starting postgres..."
compose_cmd up -d postgres

# Wait for postgres to be ready
echo "Waiting for Postgres..."
for _ in {1..30}; do
  if compose_cmd exec -T postgres pg_isready -U postgres >/dev/null 2>&1; then
    echo "Postgres is ready"
    break
  fi
  sleep 1
done

# Drop and recreate databases
for dbname in "${DATABASES[@]}"; do
  echo "Resetting database: ${dbname}"
  compose_cmd exec -T postgres psql -U postgres -d postgres -c "DROP DATABASE IF EXISTS \"${dbname}\";"
  compose_cmd exec -T postgres psql -U postgres -d postgres -c "CREATE DATABASE \"${dbname}\";"
done

echo "Database(s) reset."

# Setup audit roles before migrations
echo "Setting up audit roles..."
for dbname in "${DATABASES[@]}"; do
  DATABASE_URL="postgres://postgres:postgres@localhost:55432/${dbname}?sslmode=disable" ./scripts/setup-audit-roles.sh
done

# Run migrations using local binary
echo "Running migrations..."
for dbname in "${DATABASES[@]}"; do
  echo "Running migrations on ${dbname}..."
  # Set DATABASE_URL for this specific database
  if [ "$dbname" = "gateway_dev" ]; then
    DATABASE_URL="postgres://postgres:postgres@localhost:55432/gateway_dev?sslmode=disable" ./bin/rack-gateway-api migrate
  elif [ "$dbname" = "gateway_test" ]; then
    DATABASE_URL="postgres://postgres:postgres@localhost:55432/gateway_test?sslmode=disable" ./bin/rack-gateway-api migrate
  fi
done

echo "Database reset complete with migrations applied."
