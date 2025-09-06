#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# Resolve ports from mise.toml or environment
MiseFile="mise.toml"
toml_get() {
  local key="$1" file="$2"
  if [[ -f "$file" ]]; then
    local line
    line="$(grep -E "^\s*${key}\s*=\s*" "$file" | head -n1)"
    if [[ -n "$line" ]]; then
      echo "$line" | sed -E 's/^[^=]+= *"?([^"#]+).*$/\1/' | xargs
      return 0
    fi
  fi
  return 1
}

GW_PORT="${GATEWAY_PORT:-}"
[[ -z "$GW_PORT" ]] && GW_PORT="$(toml_get GATEWAY_PORT "$MiseFile" || echo 8447)"

echo "Building CLI..."
make -s cli

echo "Starting CLI login (two-step)..."
AUTH_FILE="$(mktemp)"
echo "Running CLI login (no-open) and writing auth params to $AUTH_FILE ..."
set -m
./bin/convox-gateway login e2e "http://127.0.0.1:${GW_PORT}" --no-open --auth-file "$AUTH_FILE" &
CLI_PID=$!

# Wait for auth-file to be written
for i in $(seq 1 30); do
  [[ -s "$AUTH_FILE" ]] && break
  sleep 0.2
done

AUTH_URL=$(sed -n 's/^AUTH_URL=//p' "$AUTH_FILE")
STATE=$(sed -n 's/^STATE=//p' "$AUTH_FILE")
CODE_VERIFIER=$(sed -n 's/^CODE_VERIFIER=//p' "$AUTH_FILE")

if [[ -z "$AUTH_URL" || -z "$STATE" || -z "$CODE_VERIFIER" ]]; then
  echo "Auth URL not produced" >&2
  kill $CLI_PID || true
  exit 1
fi

echo "Driving OAuth authorization (headless)..."
curl -s -L "$AUTH_URL&selected_user=admin@company.com" -o /dev/null || true

echo "Waiting for CLI to complete..."
wait $CLI_PID

echo "Verifying CLI commands..."
./bin/convox-gateway rack
./bin/convox-gateway convox rack
./bin/convox-gateway convox apps || true

echo "CLI E2E completed successfully."
