#!/usr/bin/env bash
set -euo pipefail

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
  docker compose stop "$service" 2>/dev/null || true
  docker compose rm -f "$service" 2>/dev/null || true
done

echo "Starting postgres..."
docker compose up -d postgres

# Wait for postgres to be ready
echo "Waiting for Postgres..."
for i in {1..30}; do
  if docker compose exec -T postgres pg_isready -U postgres >/dev/null 2>&1; then
    echo "Postgres is ready"
    break
  fi
  sleep 1
done

# Drop and recreate databases
for dbname in "${DATABASES[@]}"; do
  echo "Resetting database: ${dbname}"
  docker compose exec -T postgres psql -U postgres -d postgres -c "DROP DATABASE IF EXISTS \"${dbname}\";"
  docker compose exec -T postgres psql -U postgres -d postgres -c "CREATE DATABASE \"${dbname}\";"
done

echo "Database(s) reset."

# Start gateway services and run migrations
echo "Starting gateway services..."
for service in "${SERVICES[@]}"; do
  docker compose up -d "$service"
done

echo "Waiting for services to be ready..."
sleep 3

echo "Running migrations..."
for i in "${!DATABASES[@]}"; do
  dbname="${DATABASES[$i]}"
  service="${SERVICES[$i]}"
  echo "Running migrations on ${dbname}..."
  docker compose exec -T "$service" ./convox-gateway-api migrate
done

echo "Database reset complete with migrations applied."
