#!/usr/bin/env bash
set -euo pipefail

# Start postgres if not running
docker compose up -d postgres
# Wait for postgres to be ready
for _i in {1..30}; do
  if docker compose exec -T postgres pg_isready -U postgres >/dev/null 2>&1; then
    echo "Postgres is ready"
    break
  fi
  echo "Waiting for Postgres..."
  sleep 1
done

# Create gateway_dev database if it doesn't exist
docker compose exec -T postgres psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname = 'gateway_dev'" | grep -q 1 || \
  docker compose exec -T postgres psql -U postgres -c "CREATE DATABASE gateway_dev;"
