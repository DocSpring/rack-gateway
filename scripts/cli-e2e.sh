#!/usr/bin/env bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

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
for _i in $(seq 1 30); do
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


# verify_command <command> <expected1> <expected2> ...
function verify_command() {
  local command="$1" 
  local expected=("${@:2}")
  local shell_cmd="./bin/convox-gateway $command"
  echo -e "${BLUE}Running: $shell_cmd${NC}"
  local output
  output=$($shell_cmd)
  echo -e "${GREEN}$output${NC}"
  
  # Check that ALL expected strings are present in the output
  local missing=()
  for exp in "${expected[@]}"; do
    if ! echo "$output" | grep -q "$exp"; then
      missing+=("$exp")
    fi
  done
  
  if [[ ${#missing[@]} -gt 0 ]]; then
    echo -e "${RED}$command did not show expected strings: ${missing[*]}${NC}" >&2
    exit 1
  fi
}

echo "Verifying CLI commands..."
verify_command "rack" "Current rack: e2e" "Logged in as admin@company.com"
verify_command "convox rack" "mock-rack" "mock-rack.example.com"
verify_command "convox apps" "RAPI123456" "RWEB789012"
verify_command "convox ps" "p-web-1" "p-worker-1"

verify_command "convox run web 'echo hello'" \
  'Connected to mock exec for app=convox-gateway pid=proc-123456'

verify_command "convox exec p-worker-1 'echo hello'" \
  'Connected to mock exec for app=convox-gateway pid=p-worker-1'

echo "CLI E2E completed successfully."
