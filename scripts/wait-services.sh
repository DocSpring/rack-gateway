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

echo "Waiting for Web UI on http://127.0.0.1:${WEB_PORT}/"
retry "http://127.0.0.1:${WEB_PORT}/"

echo "Waiting for Gateway on http://127.0.0.1:${API_PORT}/.gateway/health"
retry "http://127.0.0.1:${API_PORT}/.gateway/health"

echo "Waiting for Mock OAuth on http://127.0.0.1:${OAUTH_PORT}/health"
retry "http://127.0.0.1:${OAUTH_PORT}/health"

echo "Waiting for Gateway via Vite proxy at http://127.0.0.1:${WEB_PORT}/api/.gateway/health"
retry "http://127.0.0.1:${WEB_PORT}/api/.gateway/health"

echo "Waiting for direct /.gateway proxy at http://127.0.0.1:${WEB_PORT}/.gateway/health"
retry "http://127.0.0.1:${WEB_PORT}/.gateway/health"

echo "All services are up"
