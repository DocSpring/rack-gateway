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

GATEWAY_PORT="${GATEWAY_PORT:-}"
[[ -z "$GATEWAY_PORT" ]] && GATEWAY_PORT="$(toml_get GATEWAY_PORT "$MiseFile" || echo 8447)"

echo "Building CLI..."
make -s cli

login_cli_as() {
  local user_email="$1"
  local rack_name="${2:-e2e}"
  echo -e "${YELLOW}Starting CLI login for ${user_email} on rack ${rack_name}...${NC}"
  local AUTH_FILE
  AUTH_FILE="$(mktemp)"
  echo "  - Running CLI login (no-open) and writing auth params to $AUTH_FILE ..."
  set -m
  ./bin/convox-gateway login "${rack_name}" "http://127.0.0.1:${GATEWAY_PORT}" --no-open --auth-file "$AUTH_FILE" &
  local CLI_PID=$!
  # Wait for auth-file
  for _i in $(seq 1 50); do
    [[ -s "$AUTH_FILE" ]] && break
    sleep 0.1
  done
  local AUTH_URL
  AUTH_URL=$(sed -n 's/^AUTH_URL=//p' "$AUTH_FILE")
  if [[ -z "$AUTH_URL" ]]; then
    echo -e "${RED}Auth URL not produced" >&2
    kill $CLI_PID || true
    exit 1
  fi
  echo "  - Driving OAuth authorization for ${user_email} (headless)..."
  curl -s -L "$AUTH_URL&selected_user=${user_email}" -o /dev/null || true
  echo "  - Waiting for CLI to complete..."
  wait $CLI_PID
}

# verify_command <command> <expected1> <expected2> ...
function verify_command_status_and_output() {
  local command="$1" expected_status="$2" expected_output=("${@:3}")
  local shell_cmd="./bin/convox-gateway $command"

  echo -e "${BLUE}Running: $shell_cmd...${NC}"
  set +e
  local output
  output=$($shell_cmd 2>&1)
  local exit_status=$?
  set -e
  if [[ "$exit_status" == "$expected_status" ]]; then
    echo -e "${GREEN}$output${NC}"
  else
    echo -e "${RED}Expected status $expected_status, but got $exit_status" >&2
    echo -e "${RED}$output${NC}" >&2
    exit 1
  fi

  # Check that all expected strings are present in the output
  local missing=()
  for exp in "${expected_output[@]}"; do
    if ! echo "$output" | grep -q -F "$exp"; then
      missing+=("$exp")
    fi
  done

  if [[ ${#missing[@]} -gt 0 ]]; then
    echo -e "${RED}$command did not show expected strings: ${missing[*]}${NC}" >&2
    exit 1
  fi
}

function verify_command() {
  verify_command_status_and_output "$1" "0" "${@:2}"
}

function verify_command_failure() {
  verify_command_status_and_output "$1" "1" "${@:2}"
}

function logout_cli() {
  echo -e "${YELLOW}Logging out...${NC}"
  verify_command "logout" "Removed rack: e2e"
}

# Tests
# --------------------------------------------
echo "Running CLI tests..."

# Admin login
login_cli_as "admin@company.com" "e2e"


verify_command "rack" "Current rack: e2e" "Logged in as admin@company.com"
verify_command "convox rack" "mock-rack" "mock-rack.example.com"
verify_command "convox apps" "convox-gateway" "RAPI123456"
verify_command "convox apps info -a convox-gateway" \
  "Name        convox-gateway" "Status      running"
verify_command "convox ps" "p-web-1" "p-worker-1"

verify_command "convox run web 'echo hello'" \
  'Connected to mock exec for app=convox-gateway pid=proc-123456'

verify_command "convox exec p-worker-1 'echo hello'" \
  'Connected to mock exec for app=convox-gateway pid=p-worker-1'

# List environment for a known app
verify_command "convox env -a convox-gateway" \
  "DATABASE_URL=********************" "NODE_ENV=production" "PORT=3000"

# Fetch secret
verify_command "env get DATABASE_URL -a convox-gateway --secrets" \
  "postgres://user:pass@localhost/db"


# Test env set without promote
verify_command "convox env set -a convox-gateway FOO=bar" "Setting FOO... OK" "Release:"

# Test env set with promote
verify_command "convox env set -a convox-gateway FOO=bar --promote" \
  "Setting FOO... OK" "Release:" "Promoting "

logout_cli

# Login as deployer
# ---------------------------------------------
login_cli_as "deployer@company.com" "e2e"

# Can list processes
verify_command "convox ps" "p-web-1" "p-worker-1"

# List environment for a known app
verify_command "convox env -a convox-gateway" \
  "DATABASE_URL=********************" "NODE_ENV=production" "PORT=3000"

# Cannot fetch secret
verify_command_failure "env get DATABASE_URL -a convox-gateway --secrets" \
  "Error: failed to fetch env: forbidden"

# Test env set without promote
verify_command "convox env set -a convox-gateway FOO=bar" "Setting FOO... OK" "Release:"

# Should not be able to delete apps
verify_command_failure "convox apps delete convox-gateway" "ERROR: permission denied"

logout_cli

# Login as viewer
# ---------------------------------------------
login_cli_as "viewer@company.com" "e2e"

# Viewer can list processes
verify_command "convox ps" "p-web-1" "p-worker-1"

# Cannot fetch env
verify_command_failure "convox env" \
  "ERROR: permission denied"

# Cannot fetch secret
verify_command_failure "env get DATABASE_URL -a convox-gateway --secrets" \
  "Error: failed to fetch env: forbidden"

# Viewer should not be able to set env or delete apps
verify_command_failure "convox env set NOTALLOWED=1" "ERROR: permission denied"
verify_command_failure "convox apps delete convox-gateway" "ERROR: permission denied"


echo -e "${GREEN}CLI E2E completed successfully.${NC}"
