#!/usr/bin/env bash
set -euo pipefail

WEB_PORT="${WEB_PORT:-5173}"
API_PORT="${GATEWAY_PORT:-8447}"
OAUTH_PORT="${MOCK_OAUTH_PORT:-3345}"

retry() {
  local url="$1"
  local i=0
  until curl -fsS "$url" >/dev/null 2>&1; do
    i=$((i+1))
    if [ "$i" -gt 60 ]; then
      echo "Timed out waiting for $url"
      exit 1
    fi
    sleep 1
  done
}

echo "Waiting for Web UI on http://localhost:${WEB_PORT}/"
retry "http://localhost:${WEB_PORT}/"

echo "Waiting for Gateway on http://localhost:${API_PORT}/.gateway/health"
retry "http://localhost:${API_PORT}/.gateway/health"

echo "Waiting for Mock OAuth on http://localhost:${OAUTH_PORT}/health"
retry "http://localhost:${OAUTH_PORT}/health"

echo "All services are up"

