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
if [ -n "${SKIP_BUILD:-}" ]; then
  SKIP_BUILD="$SKIP_BUILD"
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
echo "  Gateway port: $GATEWAY_PORT"
echo "  Mock OAuth port: $MOCK_OAUTH_PORT"
echo "  Mock Convox port: $MOCK_CONVOX_PORT"
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
  docker compose --profile "$PROFILE" up -d $BUILD_FLAG $DEPENDENCY_SERVICES
fi

# Start main services
echo "Starting main services: $SERVICES"
docker compose --profile "$PROFILE" up -d $BUILD_FLAG $SERVICES

# Wait for postgres and create databases
echo "Waiting for postgres and creating databases..."
if docker ps --format '{{.Names}}' | grep -q '^rack-gateway-postgres-1$'; then
  for _ in $(seq 1 20); do
    if docker compose exec -T postgres pg_isready -U postgres >/dev/null 2>&1; then
      for dbname in gateway_dev gateway_test; do
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

echo ""
echo "Dev stack started successfully!"
echo ""
echo "Services:"
echo "  Gateway API: http://localhost:$GATEWAY_PORT"
echo "  Web UI: http://localhost:$WEB_PORT"
echo "  Mock OAuth: http://localhost:$MOCK_OAUTH_PORT"
echo "  Mock Convox: http://localhost:$MOCK_CONVOX_PORT"
echo ""
echo "Health checks:"
echo "  Gateway: http://localhost:$GATEWAY_PORT/api/v1/health"
echo "  Web UI: http://localhost:$WEB_PORT/app/"
echo "  Mock OAuth: http://localhost:$MOCK_OAUTH_PORT/health"
echo ""
echo "To view logs: docker compose logs -f"
echo "To stop: docker compose down"
