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

E2E_DATABASE_NAME="${E2E_DATABASE_NAME:-gateway_test}"
E2E_GATEWAY_SERVICE="${E2E_GATEWAY_SERVICE:-gateway-api-test}"

psql_exec() {
  local sql="$1"
  local output
  set +e
  output=$(docker exec -i convox-gateway-postgres-1 psql -U postgres -d "$E2E_DATABASE_NAME" -At -F $'\t' -c "$sql" 2>&1)
  local status=$?
  set -e
  if [[ $status -ne 0 ]]; then
    echo "$output" >&2
    exit $status
  fi
}

declare -A MFA_TOTP_SECRETS=(
  ["admin@example.com"]="JBSWY3DPEHPK3PXP"
  ["deployer@example.com"]="KB6VQXGZLMN4Y3DC"
  ["viewer@example.com"]="NB2WY5DPFVXHI6ZT"
)

setup_user_mfa() {
  local email="$1"
  local secret="$2"
  local sql
  sql=$(cat <<SQL
DELETE FROM mfa_methods WHERE user_id = (SELECT id FROM users WHERE email = '${email}') AND type = 'totp';
INSERT INTO mfa_methods (user_id, type, label, secret, created_at, confirmed_at, last_used_at)
SELECT id, 'totp', 'CLI E2E', '${secret}', NOW(), NOW(), NOW() FROM users WHERE email = '${email}';
UPDATE users SET mfa_enrolled = TRUE, mfa_enforced_at = COALESCE(mfa_enforced_at, NOW()) WHERE email = '${email}';
SQL
  )
  psql_exec "$sql"
}

reset_user_mfa() {
  local email="$1"
  local sql
  sql=$(cat <<SQL
DELETE FROM mfa_methods WHERE user_id = (SELECT id FROM users WHERE email = '${email}');
DELETE FROM mfa_backup_codes WHERE user_id = (SELECT id FROM users WHERE email = '${email}');
DELETE FROM trusted_devices WHERE user_id = (SELECT id FROM users WHERE email = '${email}');
UPDATE users SET mfa_enrolled = FALSE, mfa_enforced_at = NULL WHERE email = '${email}';
SQL
  )
  psql_exec "$sql"
}

reset_all_mfa_state() {
  if ! docker ps --format '{{.Names}}' | grep -q '^convox-gateway-postgres-1$'; then
    return 0
  fi

  local sql
  sql=$(cat <<'SQL'
UPDATE user_sessions SET trusted_device_id = NULL, mfa_verified_at = NULL, recent_step_up_at = NULL;
TRUNCATE TABLE trusted_devices RESTART IDENTITY CASCADE;
TRUNCATE TABLE mfa_backup_codes RESTART IDENTITY CASCADE;
TRUNCATE TABLE mfa_methods RESTART IDENTITY CASCADE;
UPDATE users SET mfa_enrolled = FALSE, mfa_enforced_at = NULL;
UPDATE settings
   SET value = jsonb_set(value, '{require_all_users}', 'false'::jsonb, true),
       updated_at = NOW()
 WHERE key = 'mfa';
INSERT INTO settings (key, value, updated_at)
     VALUES ('mfa', jsonb_build_object('require_all_users', false), NOW())
ON CONFLICT (key) DO UPDATE
     SET value = jsonb_set(settings.value, '{require_all_users}', 'false'::jsonb, true),
         updated_at = NOW();
SQL
  )

  psql_exec "$sql"
}

trap 'reset_all_mfa_state || true' EXIT

generate_totp_code() {
  local secret="$1"
  SECRET="$secret" python3 <<'PY'
import base64, hashlib, hmac, os, struct, time

secret = os.environ["SECRET"].strip().replace(' ', '').upper()
key = base64.b32decode(secret)
counter = int(time.time() // 30)
msg = struct.pack('>Q', counter)
digest = hmac.new(key, msg, hashlib.sha1).digest()
offset = digest[-1] & 0x0F
code = (struct.unpack('>I', digest[offset:offset + 4])[0] & 0x7FFFFFFF) % 1000000
print(f"{code:06d}")
PY
}

E2E_TS="$(date +%s%3N)"

# Ensure we use a specific test config dir for e2e tests
mkdir -p config/cli-e2e
export GATEWAY_CLI_CONFIG_DIR="config/cli-e2e"

# Run a subset of tests and skip the slow ones
STAGES="ADMIN API_TOKEN DEPLOYER VIEWER"

# If ONLY_x is set, skip all the others
for stage in $STAGES; do
  var="ONLY_${stage}_TESTS"
  val="$(eval "echo \${$var:-}")"
  if [ -n "$val" ]; then
    for other in $STAGES; do
      [ "$other" != "$stage" ] && eval "SKIP_${other}_TESTS=true"
    done
    break
  fi
done

# Ensure each SKIP_x has a default
for stage in $STAGES; do
  eval "SKIP_${stage}_TESTS=\${SKIP_${stage}_TESTS:-}"
done

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

GATEWAY_PORT="${GATEWAY_PORT:-${E2E_GATEWAY_PORT:-}}"
[[ -z "$GATEWAY_PORT" ]] && GATEWAY_PORT="$(toml_get E2E_GATEWAY_PORT "$MiseFile" || toml_get TEST_GATEWAY_PORT "$MiseFile" || toml_get GATEWAY_PORT "$MiseFile" || echo 8447)"

echo "Building CLI..."
task go:build:cli

login_cli_as() {
  local user_email="$1"
  local rack_name="${2:-e2e}"
  local secret="${MFA_TOTP_SECRETS[$user_email]:-}"

  echo -e "${YELLOW}Starting CLI login for ${user_email} on rack ${rack_name}...${NC}"
  local AUTH_FILE COOKIE_FILE HTML_FILE
  AUTH_FILE="$(mktemp)"
  COOKIE_FILE="$(mktemp)"
  HTML_FILE="$(mktemp)"

  echo "  - Running CLI login (no-open) and writing auth params to $AUTH_FILE ..."
  set -m
  ./bin/convox-gateway login "${rack_name}" "http://127.0.0.1:${GATEWAY_PORT}" --no-open --auth-file "$AUTH_FILE" >"$HTML_FILE" 2>&1 &
  local CLI_PID=$!
  for _i in $(seq 1 50); do
    [[ -s "$AUTH_FILE" ]] && break
    sleep 0.1
  done

  local AUTH_URL STATE
  AUTH_URL=$(sed -n 's/^AUTH_URL=//p' "$AUTH_FILE")
  STATE=$(sed -n 's/^STATE=//p' "$AUTH_FILE")
  if [[ -z "$AUTH_URL" || -z "$STATE" ]]; then
    echo -e "${RED}Auth URL or state not produced" >&2
    kill $CLI_PID || true
    exit 1
  fi

  echo "  - Driving OAuth authorization for ${user_email} (headless)..."
  curl -s -L -c "$COOKIE_FILE" -b "$COOKIE_FILE" -o /dev/null "${AUTH_URL}&selected_user=${user_email}" || true

  if [[ -n "$secret" ]]; then
    local totp_code
    totp_code=$(generate_totp_code "$secret")
    curl -s -c "$COOKIE_FILE" -b "$COOKIE_FILE" \
      -H "Content-Type: application/json" \
      --data "{\"state\":\"${STATE}\",\"code\":\"${totp_code}\"}" \
      "http://127.0.0.1:${GATEWAY_PORT}/.gateway/api/auth/cli/mfa" \
      -o /dev/null || true
  fi

  rm -f "$COOKIE_FILE" "$AUTH_FILE" "$HTML_FILE"

  echo "  - Waiting for CLI to complete..."
  wait $CLI_PID
  set +m
}

function verify_command_status_and_output() {
  command="$1" expected_status="$2" expected_output=("${@:3}")
  local shell_cmd="${WRAPPER_CMD:-./bin/convox-gateway} $command"

  echo -e "${BLUE}Running: $shell_cmd...${NC}"
  set +e
  local output
  # Apply a hard timeout to avoid hangs
  output=$(eval "$shell_cmd" 2>&1)
  local exit_status=$?
  set -e
  if [[ "$exit_status" == "$expected_status" ]]; then
    echo -e "${GREEN}$output${NC}"
  else
    echo -e "${RED}Expected status $expected_status, but got $exit_status" >&2
    echo -e "${RED}Output: $output${NC}" >&2
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

function verify_cgw_command() {
  verify_command_status_and_output "$1" "0" "${@:2}"
}

function verify_cgw_command_failure() {
  verify_command_status_and_output "$1" "1" "${@:2}"
}

function logout_cli() {
  echo -e "${YELLOW}Logging out...${NC}"
  verify_cgw_command "logout" "Removed rack: e2e"
}


# Tests
# --------------------------------------------
echo "Running CLI tests..."

if [ -z "$SKIP_ADMIN_TESTS" ] || [ -z "$SKIP_API_TOKEN_TESTS" ]; then

  rm -f "${GATEWAY_CLI_CONFIG_DIR:-config/cli-e2e}/config.json"

  echo -e "${YELLOW}Enabling MFA enforcement...${NC}"
  psql_exec "UPDATE settings SET value = jsonb_set(value, '{require_all_users}', 'true'::jsonb, true), updated_at = NOW() WHERE key = 'mfa';"

  echo -e "${YELLOW}Restarting ${E2E_GATEWAY_SERVICE} to apply MFA setting...${NC}"
  docker compose restart "${E2E_GATEWAY_SERVICE}" >/dev/null
  ./scripts/wait-for-services.sh

  for user_email in "admin@example.com" "deployer@example.com" "viewer@example.com"; do
    reset_user_mfa "$user_email"
  done

  echo -e "${YELLOW}Verifying CLI login fails until MFA enrollment...${NC}"
  AUTH_FILE="$(mktemp)"
  OUTPUT_FILE="$(mktemp)"
  COOKIE_FILE="$(mktemp)"
  set -m
  ./bin/convox-gateway login "e2e" "http://127.0.0.1:${GATEWAY_PORT}" --no-open --auth-file "$AUTH_FILE" >"$OUTPUT_FILE" 2>&1 &
  CLI_PID=$!
  for _i in $(seq 1 50); do
    [[ -s "$AUTH_FILE" ]] && break
    sleep 0.1
  done

  AUTH_URL=$(sed -n 's/^AUTH_URL=//p' "$AUTH_FILE")
  STATE=$(sed -n 's/^STATE=//p' "$AUTH_FILE")
  if [[ -z "$AUTH_URL" || -z "$STATE" ]]; then
    echo -e "${RED}CLI login did not produce AUTH_URL/STATE${NC}" >&2
    kill $CLI_PID || true
    exit 1
  fi

  # Simulate the browser selecting the unenrolled admin user
  curl -s -L -c "$COOKIE_FILE" -b "$COOKIE_FILE" "${AUTH_URL}&selected_user=admin@example.com" -o /dev/null || true

  set +e
  wait $CLI_PID
  CLI_STATUS=$?
  set -e
  set +m
  CLI_OUTPUT=$(cat "$OUTPUT_FILE")
  rm -f "$AUTH_FILE" "$OUTPUT_FILE" "$COOKIE_FILE"

  if [[ $CLI_STATUS -eq 0 ]]; then
    echo -e "${RED}CLI login succeeded unexpectedly when MFA enrollment is required.${NC}" >&2
    echo "$CLI_OUTPUT" >&2
    exit 1
  fi

  if ! echo "$CLI_OUTPUT" | grep -Fq "Error: login failed: You must set up multi-factor authentication before you can continue using the CLI."; then
    echo -e "${RED}CLI did not report MFA enrollment error as expected.${NC}" >&2
    echo "$CLI_OUTPUT" >&2
    exit 1
  fi

  # Clean up any pending login state from the failed attempt
  psql_exec "DELETE FROM cli_login_states"

  # Provision deterministic MFA methods for automated verification
  for user_email in "admin@example.com" "deployer@example.com" "viewer@example.com"; do
    setup_user_mfa "$user_email" "${MFA_TOTP_SECRETS[$user_email]}"
  done

  login_cli_as "admin@example.com" "e2e"
fi

if [ -z "$SKIP_ADMIN_TESTS" ]; then
  verify_cgw_command "rack" "Current rack: e2e" "Logged in as admin@example.com"
  verify_cgw_command "convox rack" "mock-rack" "mock-rack.example.com"
  verify_cgw_command "convox apps" "convox-gateway" "RAPI123456"
  verify_cgw_command "convox apps info" \
    "Name        convox-gateway" "Status      running"
  verify_cgw_command "convox ps" "p-web-1" "p-worker-1"

  verify_cgw_command "convox run web 'echo hello'" \
    'Connected to mock exec for app=convox-gateway pid=proc-123456' \
    '$ echo hello' \
    'hello' \
    'Exit code: 0' \
    'Session closed.'

  verify_cgw_command "convox exec p-worker-1 'echo hello'" \
    'Connected to mock exec for app=convox-gateway pid=p-worker-1' \
    '$ echo hello' \
    'hello' \
    'Exit code: 0' \
    'Session closed.'

  # List environment for a known app
  verify_cgw_command "convox env" \
    "DATABASE_URL=********************" \
    "NODE_ENV=production" \
    "PORT=3000"

  # Fetch secret with --secrets flag
  verify_cgw_command "env get DATABASE_URL --secrets" \
    "postgres://user:pass@localhost/db"

  verify_cgw_command "env set FOO=bar" \
    "Setting FOO..." "Release:"

  verify_cgw_command "convox restart" \
    "Restarting web... OK" \
    "Restarting worker... OK"

  # Test full build + release flow
  verify_cgw_command "convox deploy" \
    "Packaging source..." "Uploading source..." "Starting build..." \
    "Building app..." \
    "Step 1/1: mock build step" \
    "Build complete" \
    "Promoting RNEW" \
    "OK"

  # Check logs via websockets (this stream is long-lived; kill after 3s)
  WRAPPER_CMD="timeout 3s ./bin/convox-gateway" verify_command_status_and_output "convox logs" \
    "124" \
    "Promoting release" \
    "Release promoted successfully."

  if [ -n "$SKIP_API_TOKEN_TESTS" ]; then
    # Log out now if we're not running API token tests
    logout_cli
  fi
fi

if [ -z "$SKIP_API_TOKEN_TESTS" ]; then
  # Create a CI/CD API token and exercise pipeline-style commands using the raw token
  echo -e "${YELLOW}Creating CI/CD API token for pipeline simulation...${NC}"
  API_TOKEN_JSON=$(./bin/convox-gateway api-token create \
    --name "E2E CLI API Token ${E2E_TS}" \
    --role cicd \
    --output json)

  API_TOKEN=$(jq -r '.token' <<<"$API_TOKEN_JSON")
  API_TOKEN_PUBLIC_ID=$(jq -r '.api_token.public_id' <<<"$API_TOKEN_JSON")

  if [[ -z "$API_TOKEN" || -z "$API_TOKEN_PUBLIC_ID" ]]; then
    echo -e "${RED}Failed to parse API token response${NC}" >&2
    echo -e "${RED}API Token JSON: $API_TOKEN_JSON${NC}"
    exit 1
  fi

  echo -e "${GREEN}API Token ID: $API_TOKEN_PUBLIC_ID${NC}, Token: $API_TOKEN${NC}"

  PREAPPROVE_CODE=$(generate_totp_code "${MFA_TOTP_SECRETS[admin@example.com]}")
  verify_cgw_command \
    "deploy-approval pre-approve $API_TOKEN_PUBLIC_ID 'Pipeline deployment ${E2E_TS}' --mfa-code $PREAPPROVE_CODE" \
    "Deploy approval request" "pre-approved"

  # Normalize rack alias for deploy approvals to match mock rack name
  docker compose exec -T postgres psql -U postgres -d gateway_test \
    -c "UPDATE deploy_approval_requests SET rack = 'Test' WHERE id = (SELECT id FROM deploy_approval_requests WHERE target_api_token_id = (SELECT id FROM api_tokens WHERE public_id = '$API_TOKEN_PUBLIC_ID') ORDER BY id DESC LIMIT 1);"

  logout_cli

  export CONVOX_GATEWAY_API_TOKEN="$API_TOKEN"
  export CONVOX_GATEWAY_URL="http://127.0.0.1:${GATEWAY_PORT}"
  export CONVOX_GATEWAY_RACK="Test"

  echo -e "${YELLOW}Simulating CircleCI deploy workflow with API token permissions...${NC}"

  # Show rack info via API token
  verify_cgw_command \
    "convox rack" \
    "Name" \
    "Status"

  # Show processes via API token
  verify_cgw_command \
    "convox ps --app convox-gateway" \
    "p-web-1"

  # Create build and capture release identifier
  set +e
  build_output=$(./bin/convox-gateway convox build --app convox-gateway --description "cli-e2e" --id 2>&1)
  build_status=$?
  set -e
  if [[ $build_status -ne 0 ]]; then
    echo -e "${RED}convox build failed:${NC}\n$build_output" >&2
    exit 1
  fi
  echo "$build_output"
  RELEASE_ID=$(echo "$build_output" | tail -n1 | tr -d '[:space:]')
  if [[ -z "$RELEASE_ID" ]]; then
    echo -e "${RED}Failed to parse release id from build output${NC}" >&2
    exit 1
  fi

  # Run mock migration command on the new release
  verify_cgw_command \
    "convox run web --app convox-gateway --release $RELEASE_ID 'echo migrate'" \
    "migrate"

  # Promote the release
  verify_cgw_command \
    "convox releases promote $RELEASE_ID --app convox-gateway" \
    "OK"

  # Clean up the API token now that the pipeline simulation is complete
  unset CONVOX_GATEWAY_API_TOKEN
  unset CONVOX_GATEWAY_URL
  unset CONVOX_GATEWAY_RACK

  # Delete via admin login to validate token deletion flow
  login_cli_as "admin@example.com" "e2e"
  verify_cgw_command "api-token delete $API_TOKEN_PUBLIC_ID" "Deleted token $API_TOKEN_PUBLIC_ID"
  logout_cli
fi

if [ -z "$SKIP_DEPLOYER_TESTS" ]; then
  # Login as deployer
  # ---------------------------------------------
  login_cli_as "deployer@example.com" "e2e"

  # Can list processes
  verify_cgw_command "convox ps" "p-web-1" "p-worker-1"

  # List environment for a known app
  verify_cgw_command "convox env" \
    "DATABASE_URL=********************" "NODE_ENV=production" "PORT=3000"

  # Cannot fetch secret
  verify_cgw_command_failure "env get DATABASE_URL --secrets" \
    "Error: failed to fetch env: You don't have permission to view secrets."

  # (env set tests removed for deployer; protected env policy preservation)

  # Should not be able to delete apps
  verify_cgw_command_failure "convox apps delete convox-gateway" "ERROR: permission denied"

  logout_cli
fi

if [ -z "$SKIP_VIEWER_TESTS" ]; then
  # Login as viewer
  # ---------------------------------------------
  login_cli_as "viewer@example.com" "e2e"

  # Viewer can list processes
  verify_cgw_command "convox ps" "p-web-1" "p-worker-1"

  # Cannot fetch env
  verify_cgw_command_failure "convox env" \
    "ERROR: permission denied"

  # Cannot fetch secret
  verify_cgw_command_failure "env get DATABASE_URL --secrets" \
    "Error: failed to fetch env: You don't have permission to view environment variables."

  # Viewer should not be able to set env or delete apps
  verify_cgw_command_failure "convox env set NOTALLOWED=1" "ERROR: permission denied"
  verify_cgw_command_failure "convox apps delete convox-gateway" "ERROR: permission denied"
fi

echo -e "${GREEN}CLI E2E completed successfully.${NC}"
