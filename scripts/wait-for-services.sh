#!/usr/bin/env bash
set -euo pipefail

WEB_PORT="${WEB_PORT:-5223}"
# Default to the SPA mount path used by both dev and preview
WEB_UI_PATH="${WEB_UI_PATH:-/app/}"
GATEWAY_PORT="${GATEWAY_PORT:-8447}"
OAUTH_PORT="${MOCK_OAUTH_PORT:-3345}"
SHARD_PORTS_RAW="${E2E_GATEWAY_PORTS:-}"
# Max total wait (seconds)
MAX_WAIT_SECONDS="${MAX_WAIT_SECONDS:-120}"

retry() {
  local url="$1"
  local i=0
  until curl -fsS "$url" >/dev/null 2>&1; do
    i=$((i+1))
    if [ "$i" -gt "$MAX_WAIT_SECONDS" ]; then
      echo "Timed out waiting for $url"
      exit 1
    fi
    sleep 1
  done
}

if [ -n "$SHARD_PORTS_RAW" ]; then
  IFS=',' read -r -a SHARD_PORTS <<<"$SHARD_PORTS_RAW"
  if [ "${#SHARD_PORTS[@]}" -eq 0 ]; then
    SHARD_PORTS=("$GATEWAY_PORT")
  fi

  for port in "${SHARD_PORTS[@]}"; do
    port="$(echo "$port" | tr -d ' ')"
    if [ -z "$port" ]; then
      continue
    fi
    echo "Waiting for Gateway on http://127.0.0.1:${port}/api/v1/health"
    retry "http://127.0.0.1:${port}/api/v1/health"

    echo "Waiting for Web UI on http://127.0.0.1:${port}${WEB_UI_PATH}"
    retry "http://127.0.0.1:${port}${WEB_UI_PATH}"
  done
else
  echo "Waiting for Gateway on http://127.0.0.1:${GATEWAY_PORT}/api/v1/health"
  retry "http://127.0.0.1:${GATEWAY_PORT}/api/v1/health"

  echo "Waiting for Web UI on http://127.0.0.1:${WEB_PORT}${WEB_UI_PATH}"
  retry "http://127.0.0.1:${WEB_PORT}${WEB_UI_PATH}"
fi

echo "Waiting for Mock OAuth on http://127.0.0.1:${OAUTH_PORT}/health"
retry "http://127.0.0.1:${OAUTH_PORT}/health"

if [ -z "$SHARD_PORTS_RAW" ]; then
  echo "Waiting for Gateway via Vite proxy at http://127.0.0.1:${WEB_PORT}/api/v1/health"
  if [ "${CHECK_VITE_PROXY:-true}" = "true" ]; then
    retry "http://127.0.0.1:${WEB_PORT}/api/v1/health"
  else
    echo "Skipping Vite proxy check (CHECK_VITE_PROXY=${CHECK_VITE_PROXY:-unset})"
  fi
else
  echo "Skipping Vite proxy check for sharded test stack"
fi

echo "All services are up"
