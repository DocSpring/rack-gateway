#!/usr/bin/env bash
set -euo pipefail

# Docker Up Script
# Equivalent to: task docker:up
# Starts the full dev stack with all services

# Default values (can be overridden by environment variables)
WEB_PORT="${WEB_PORT:-5223}"
GATEWAY_PORT="${GATEWAY_PORT:-8447}"
MOCK_OAUTH_PORT="${MOCK_OAUTH_PORT:-3345}"
MOCK_CONVOX_PORT="${MOCK_CONVOX_PORT:-5443}"
SKIP_BUILD="${SKIP_BUILD:-false}"

# Additional configuration (can be overridden by environment variables)
TEST_GATEWAY_PORT="${TEST_GATEWAY_PORT:-9447}"
ORIGINAL_WEB_E2E_SHARDS="${WEB_E2E_SHARDS:-}"

determine_web_e2e_shards() {
  if [ -n "$ORIGINAL_WEB_E2E_SHARDS" ]; then
    echo "$ORIGINAL_WEB_E2E_SHARDS"
    return
  fi
  if [ "${CI:-}" = "true" ]; then
    echo 1
  else
    echo 3
  fi
}

# Service configuration (can be overridden by environment variables)
DEPENDENCY_SERVICES="${DEPENDENCY_SERVICES:-postgres}"
SERVICES="${SERVICES:-postgres mock-oauth mock-convox web-dev gateway-api-dev}"
STACK="${STACK:-dev}"
PROFILE="${PROFILE:-dev}"

# Support for task variables (when called from task runner)
if [ -n "${WAIT_WEB_PORT:-}" ]; then
  WEB_PORT="$WAIT_WEB_PORT"
fi
if [ -n "${WAIT_GATEWAY_PORT:-}" ]; then
  GATEWAY_PORT="$WAIT_GATEWAY_PORT"
fi
if [ -n "${WAIT_MOCK_OAUTH_PORT:-}" ]; then
  MOCK_OAUTH_PORT="$WAIT_MOCK_OAUTH_PORT"
fi

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --skip-build)
      SKIP_BUILD="true"
      shift
      ;;
    --web-port)
      WEB_PORT="$2"
      shift 2
      ;;
    --gateway-port)
      GATEWAY_PORT="$2"
      shift 2
      ;;
    --mock-oauth-port)
      MOCK_OAUTH_PORT="$2"
      shift 2
      ;;
    --mock-convox-port)
      MOCK_CONVOX_PORT="$2"
      shift 2
      ;;
    --help|-h)
      echo "Usage: $0 [OPTIONS]"
      echo ""
      echo "Start the full dev stack with all services"
      echo ""
      echo "Options:"
      echo "  --skip-build              Skip building images"
      echo "  --web-port PORT           Set web frontend port (default: 5223)"
      echo "  --gateway-port PORT       Set gateway API port (default: 8447)"
      echo "  --mock-oauth-port PORT    Set mock OAuth port (default: 3345)"
      echo "  --mock-convox-port PORT   Set mock Convox port (default: 5443)"
      echo "  --help, -h                Show this help message"
      echo ""
      echo "Environment variables:"
      echo "  WEB_PORT                  Web frontend port"
      echo "  GATEWAY_PORT              Gateway API port"
      echo "  MOCK_OAUTH_PORT           Mock OAuth port"
      echo "  MOCK_CONVOX_PORT          Mock Convox port"
      echo "  SKIP_BUILD                Skip building images (true/false)"
      echo ""
      echo "Examples:"
      echo "  $0                        # Start with default ports"
      echo "  $0 --skip-build           # Start without rebuilding"
      echo "  $0 --web-port 3000        # Use custom web port"
      exit 0
      ;;
    *)
      echo "Unknown option: $1"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

# Determine sharded configuration for test stack
DATABASES=()
GATEWAY_PORTS=()
if [ "$STACK" = "test" ]; then
  WEB_E2E_SHARDS="$(determine_web_e2e_shards)"
  export WEB_E2E_SHARDS

  # Align default gateway/web ports with test stack base
  GATEWAY_PORT="$TEST_GATEWAY_PORT"
  WEB_PORT="$TEST_GATEWAY_PORT"

  IFS=' ' read -r -a SERVICE_ARRAY <<<"$SERVICES"
  DATABASES+=("gateway_dev")
  for idx in $(seq 1 "$WEB_E2E_SHARDS"); do
    if [ "$idx" -eq 1 ]; then
      service_name="gateway-api-test"
      dbname="gateway_test"
    else
      service_name="gateway-api-test-$idx"
      dbname="gateway_test_$idx"
      case " ${SERVICE_ARRAY[*]} " in
        *" $service_name "*) ;;
        *) SERVICE_ARRAY+=("$service_name") ;;
      esac
    fi

    port=$((TEST_GATEWAY_PORT + idx - 1))
    GATEWAY_PORTS+=("$port")
    DATABASES+=("$dbname")
  done
  SERVICES="${SERVICE_ARRAY[*]}"

  # Export comma-separated list for downstream consumers
  if [ "${#GATEWAY_PORTS[@]}" -gt 0 ]; then
    E2E_GATEWAY_PORTS="$(IFS=,; echo "${GATEWAY_PORTS[*]}")"
    export E2E_GATEWAY_PORTS
  fi
else
  DATABASES+=("gateway_dev" "gateway_test")
  GATEWAY_PORTS+=("$GATEWAY_PORT")
fi
WEB_E2E_SHARDS="${WEB_E2E_SHARDS:-1}"

if [ "${#GATEWAY_PORTS[@]}" -gt 0 ]; then
  GATEWAY_PORT_DISPLAY="$(IFS=,; echo "${GATEWAY_PORTS[*]}")"
else
  GATEWAY_PORT_DISPLAY="$GATEWAY_PORT"
fi

# Export environment variables for docker-compose
export WEB_PORT
export GATEWAY_PORT
export MOCK_OAUTH_PORT
export MOCK_CONVOX_PORT
export DOCKER_BUILDKIT=1
export COMPOSE_DOCKER_CLI_BUILD=1
export BUILDKIT_PROGRESS=plain

echo "Starting $STACK stack..."
echo "  Web port: $WEB_PORT"
echo "  Gateway port(s): $GATEWAY_PORT_DISPLAY"
echo "  Mock OAuth port: $MOCK_OAUTH_PORT"
echo "  Mock Convox port: $MOCK_CONVOX_PORT"
if [ "$STACK" = "test" ]; then
  echo "  Web E2E shards: $WEB_E2E_SHARDS"
fi
echo "  Skip build: $SKIP_BUILD"
echo "  Profile: $PROFILE"
echo "  Services: $SERVICES"
echo "  Dependency services: $DEPENDENCY_SERVICES"
echo ""

# Build flag
BUILD_FLAG=""
if [ "$SKIP_BUILD" != "true" ]; then
  BUILD_FLAG="--build"
fi

# Start dependency services first (if any)
if [ -n "$DEPENDENCY_SERVICES" ]; then
  echo "Starting dependency services: $DEPENDENCY_SERVICES"
  # shellcheck disable=SC2086
  docker compose --profile "$PROFILE" up -d $BUILD_FLAG $DEPENDENCY_SERVICES
fi

# Wait for postgres and ensure databases exist before starting main services
echo "Waiting for postgres and ensuring databases exist..."
if docker ps --format '{{.Names}}' | grep -q '^rack-gateway-postgres-1$'; then
  for _ in $(seq 1 20); do
    if docker compose exec -T postgres pg_isready -U postgres >/dev/null 2>&1; then
      for dbname in "${DATABASES[@]}"; do
        if ! docker compose exec -T postgres psql -U postgres -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${dbname}'" | grep -q 1; then
          echo "Creating database: $dbname"
          docker compose exec -T postgres psql -U postgres -d postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"${dbname}\";"
        else
          echo "Database $dbname already exists"
        fi
      done
      break
    fi
    echo "Waiting for postgres to be ready..."
    sleep 1
  done
fi

# Determine gateway service(s) in this stack (supports sharding for web e2e)
GATEWAY_SERVICES=()
for service in $SERVICES; do
  if [[ $service == gateway-api* ]]; then
    GATEWAY_SERVICES+=("$service")
  fi
done

# Run database migrations before bringing the gateway(s) online
if [ "${#GATEWAY_SERVICES[@]}" -gt 0 ]; then
  # Use the first gateway service to run migrations for all databases
  first_gateway="${GATEWAY_SERVICES[0]}"
  echo "Running migrations for all databases using $first_gateway..."

  # Build migration commands for all databases
  migration_script=""
  for gateway_service in "${GATEWAY_SERVICES[@]}"; do
    # Extract database name from service configuration
    if [ "$gateway_service" = "gateway-api-test" ]; then
      dbname="gateway_test"
    elif [[ "$gateway_service" =~ gateway-api-test-([0-9]+) ]]; then
      shard_num="${BASH_REMATCH[1]}"
      dbname="gateway_test_$shard_num"
    elif [ "$gateway_service" = "gateway-api-dev" ]; then
      dbname="gateway_dev"
    else
      continue
    fi

    migration_script="${migration_script}echo 'Migrating $dbname...'; DATABASE_URL=\"postgres://postgres:postgres@postgres:5432/${dbname}?sslmode=disable\" ./rack-gateway-api migrate > /dev/null 2>&1 && echo '  ✓ $dbname migrated' || echo '  ✗ $dbname migration failed'; "
  done

  docker compose --profile "$PROFILE" run --rm "$first_gateway" sh -c "$migration_script"
  echo "Database migrations completed"
fi

# Start main services
echo "Starting main services: $SERVICES"
# shellcheck disable=SC2086
docker compose --profile "$PROFILE" up -d $BUILD_FLAG $SERVICES

echo ""
echo "Dev stack started successfully!"
echo ""
echo "Services:"
if [ "${#GATEWAY_PORTS[@]}" -gt 1 ]; then
  echo "  Gateway API:"
  for port in "${GATEWAY_PORTS[@]}"; do
    echo "    - http://localhost:$port"
  done
else
  echo "  Gateway API: http://localhost:${GATEWAY_PORTS[0]}"
fi
echo "  Web UI: http://localhost:$WEB_PORT"
if [ "$STACK" = "test" ] && [ "${#GATEWAY_PORTS[@]}" -gt 1 ]; then
  for port in "${GATEWAY_PORTS[@]}"; do
    echo "    shard: http://localhost:$port/app/"
  done
fi
echo "  Mock OAuth: http://localhost:$MOCK_OAUTH_PORT"
echo "  Mock Convox: http://localhost:$MOCK_CONVOX_PORT"
echo ""
echo "Health checks:"
if [ "${#GATEWAY_PORTS[@]}" -gt 1 ]; then
  echo "  Gateway endpoints:"
  for port in "${GATEWAY_PORTS[@]}"; do
    echo "    - http://localhost:$port/api/v1/health"
  done
else
  echo "  Gateway: http://localhost:${GATEWAY_PORTS[0]}/api/v1/health"
fi
echo "  Mock OAuth: http://localhost:$MOCK_OAUTH_PORT/health"
echo ""
echo "To view logs: docker compose logs -f"
echo "To stop: docker compose down"
