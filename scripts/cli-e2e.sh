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
  output=$(docker exec -i rack-gateway-postgres-1 psql -U postgres -d "$E2E_DATABASE_NAME" -At -F $'\t' -c "$sql" 2>&1)
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

reset_all_test_state() {
  if ! docker ps --format '{{.Names}}' | grep -q '^rack-gateway-postgres-1$'; then
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
-- Allow specific commands for E2E tests (deploy approvals)
INSERT INTO settings (key, value, updated_at)
     VALUES ('approved_commands', '{"commands": ["echo hello", "echo migrate"]}'::jsonb, NOW())
ON CONFLICT (key) DO UPDATE
     SET value = '{"commands": ["echo hello", "echo migrate"]}'::jsonb,
         updated_at = NOW();
-- Configure image tag pattern for manifest validation
INSERT INTO settings (key, value, updated_at)
     VALUES ('app_image_patterns', '{"rack-gateway": ".*:{{GIT_COMMIT}}-amd64"}'::jsonb, NOW())
ON CONFLICT (key) DO UPDATE
     SET value = '{"rack-gateway": ".*:{{GIT_COMMIT}}-amd64"}'::jsonb,
         updated_at = NOW();
SQL
  )

  psql_exec "$sql"
}

trap 'reset_all_test_state || true' EXIT

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

  # Clear used TOTP codes to allow replay in tests
  psql_exec "DELETE FROM mfa_totp_attempts"

  echo -e "${YELLOW}Starting CLI login for ${user_email} on rack ${rack_name}...${NC}"
  local AUTH_FILE COOKIE_FILE HTML_FILE
  AUTH_FILE="$(mktemp)"
  COOKIE_FILE="$(mktemp)"
  HTML_FILE="$(mktemp)"

  echo "  - Running CLI login (no-open) and writing auth params to $AUTH_FILE ..."
  set -m
  ./bin/rack-gateway login "${rack_name}" "http://127.0.0.1:${GATEWAY_PORT}" --no-open --auth-file "$AUTH_FILE" >"$HTML_FILE" 2>&1 &
  local CLI_PID=$!
  echo "    CLI PID: $CLI_PID"
  for _i in $(seq 1 50); do
    [[ -s "$AUTH_FILE" ]] && break
    sleep 0.1
  done
  if [[ ! -s "$AUTH_FILE" ]]; then
    echo -e "${RED}Auth file not created after 5 seconds${NC}" >&2
    echo "CLI output:" >&2
    cat "$HTML_FILE" >&2
    kill $CLI_PID 2>/dev/null || true
    exit 1
  fi

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
    echo "    Sending MFA code for state: ${STATE}"
    local mfa_response
    mfa_response=$(curl -s -c "$COOKIE_FILE" -b "$COOKIE_FILE" \
      -H "Content-Type: application/json" \
      --data "{\"state\":\"${STATE}\",\"code\":\"${totp_code}\"}" \
      "http://127.0.0.1:${GATEWAY_PORT}/.gateway/api/auth/cli/mfa")
    echo "    MFA response: $mfa_response"
  fi

  echo "  - Waiting for CLI to complete..."
  set +e
  local timeout=50
  local elapsed=0
  while kill -0 $CLI_PID 2>/dev/null; do
    if [[ $elapsed -ge $timeout ]]; then
      echo -e "${RED}CLI login timed out after 5 seconds${NC}" >&2
      echo "CLI still running. Output so far:" >&2
      cat "$HTML_FILE" >&2
      kill $CLI_PID 2>/dev/null || true
      rm -f "$COOKIE_FILE" "$AUTH_FILE" "$HTML_FILE"
      set +m
      exit 1
    fi
    sleep 0.1
    elapsed=$((elapsed + 1))
  done
  wait $CLI_PID
  local wait_status=$?
  set -e
  set +m

  if [[ $wait_status -ne 0 ]]; then
    echo -e "${RED}CLI login failed with status $wait_status${NC}" >&2
    echo "CLI output:" >&2
    cat "$HTML_FILE" >&2
    rm -f "$COOKIE_FILE" "$AUTH_FILE" "$HTML_FILE"
    exit 1
  fi

  rm -f "$COOKIE_FILE" "$AUTH_FILE" "$HTML_FILE"
}

function verify_command_status_and_output() {
  command="$1" expected_status="$2" expected_output=("${@:3}")
  local shell_cmd="${WRAPPER_CMD:-./bin/rack-gateway} $command"

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

function verify_rgw_command() {
  verify_command_status_and_output "$1" "0" "${@:2}"
}

function verify_rgw_command_failure() {
  verify_command_status_and_output "$1" "1" "${@:2}"
}

function logout_cli() {
  echo -e "${YELLOW}Logging out...${NC}"
  verify_rgw_command "logout" "Logged out from e2e"
}


# Tests
# --------------------------------------------
echo "Running CLI tests..."

# Reset all state and set up approved commands for tests
reset_all_test_state

if [ -z "$SKIP_ADMIN_TESTS" ] || [ -z "$SKIP_API_TOKEN_TESTS" ]; then

  rm -f "${GATEWAY_CLI_CONFIG_DIR:-config/cli-e2e}/config.json"

  echo -e "${YELLOW}Enabling MFA enforcement...${NC}"
  psql_exec "UPDATE settings SET value = jsonb_set(value, '{require_all_users}', 'true'::jsonb, true), updated_at = NOW() WHERE key = 'mfa';"

  echo -e "${YELLOW}Restarting ${E2E_GATEWAY_SERVICE} to apply MFA setting...${NC}"
  docker compose restart "${E2E_GATEWAY_SERVICE}" >/dev/null
  WEB_PORT="${GATEWAY_PORT}" CHECK_VITE_PROXY=false ./scripts/wait-for-services.sh

  for user_email in "admin@example.com" "deployer@example.com" "viewer@example.com"; do
    reset_user_mfa "$user_email"
  done

  echo -e "${YELLOW}Verifying CLI login fails until MFA enrollment...${NC}"
  AUTH_FILE="$(mktemp)"
  OUTPUT_FILE="$(mktemp)"
  COOKIE_FILE="$(mktemp)"
  set -m
  ./bin/rack-gateway login "e2e" "http://127.0.0.1:${GATEWAY_PORT}" --no-open --auth-file "$AUTH_FILE" >"$OUTPUT_FILE" 2>&1 &
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
  verify_rgw_command "rack" "Current rack: e2e" "Logged in as admin@example.com"
  verify_rgw_command "rack info" "mock-rack" "mock-rack.example.com"
  verify_rgw_command "apps" "rack-gateway" "RAPI123456"
  verify_rgw_command "apps info" \
    "Name        rack-gateway" "Status      running"
  verify_rgw_command "ps" "p-web-1" "p-worker-1"

  verify_rgw_command "run web 'echo hello'" \
    'Connected to mock exec for app=rack-gateway pid=proc-123456' \
    '$ echo hello' \
    'hello' \
    'Exit code: 0' \
    'Session closed.'

  verify_rgw_command "exec p-worker-1 'echo hello'" \
    'Connected to mock exec for app=rack-gateway pid=p-worker-1' \
    '$ echo hello' \
    'hello' \
    'Exit code: 0' \
    'Session closed.'

  # List environment for a known app
  verify_rgw_command "env" \
    "DATABASE_URL=********************" \
    "NODE_ENV=production" \
    "PORT=3000"

  # Fetch secret with --unmask flag
  verify_rgw_command "env get DATABASE_URL --unmask" \
    "postgres://user:pass@localhost/db"

  verify_rgw_command "env set FOO=bar" \
    "Setting FOO..." "Release:"

  verify_rgw_command "restart" \
    "Restarting web... OK" \
    "Restarting worker... OK"

  # Test full build + release flow
  verify_rgw_command "deploy" \
    "Packaging source..." "Uploading source..." "Starting build..." \
    "Building app..." \
    "Step 1/1: mock build step" \
    "Build complete" \
    "Promoting RNEW" \
    "OK"

  # Check logs via websockets (this stream is long-lived; kill after 3s)
  WRAPPER_CMD="timeout 3s ./bin/rack-gateway" verify_command_status_and_output "logs" \
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
  API_TOKEN_JSON=$(./bin/rack-gateway api-token create \
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

  # Log out admin before switching to API token
  logout_cli

  export RACK_GATEWAY_API_TOKEN="$API_TOKEN"
  export RACK_GATEWAY_URL="http://127.0.0.1:${GATEWAY_PORT}"
  export RACK_GATEWAY_RACK="Test"

  echo -e "${YELLOW}Simulating CircleCI deploy workflow with API token permissions...${NC}"

  # Show rack info via API token
  verify_rgw_command "rack" \
    "Current rack: Test" \
    "Gateway URL: http://127.0.0.1:9447"

  verify_rgw_command "rack info" \
    "Name" \
    "Status"

  # Show processes via API token
  verify_rgw_command \
    "ps --app rack-gateway" \
    "p-web-1"

  # No commands allowed
  verify_rgw_command_failure \
    "run web --app rack-gateway 'delete everything'" \
    "Error: You don't have permission to run processes"

  # Not even approved commands
  verify_rgw_command_failure \
    "run web --app rack-gateway 'echo hello'" \
    "Error: You don't have permission to run processes"


  # Request approval as API token (BEFORE building - git commit-based flow)
  echo -e "${YELLOW}API token requesting deploy approval for git commit...${NC}"
  GIT_COMMIT_HASH="abc123def456"  # Mock git commit hash for E2E test
  GIT_BRANCH="main"
  PIPELINE_URL="https://circleci.com/gh/example/repo/123"

  set +e
  approval_output=$(./bin/rack-gateway deploy-approval request \
    --git-commit "$GIT_COMMIT_HASH" \
    --branch "$GIT_BRANCH" \
    --pipeline-url "$PIPELINE_URL" \
    --ci-provider "circleci" \
    --message "Pipeline deployment ${E2E_TS}" 2>&1)
  approval_status=$?
  set -e
  if [[ $approval_status -ne 0 ]]; then
    echo -e "${RED}deploy-approval request failed:${NC}\n$approval_output" >&2
    exit 1
  fi
  echo "$approval_output"
  REQUEST_ID=$(echo "$approval_output" | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -n1)
  if [[ -z "$REQUEST_ID" ]]; then
    echo -e "${RED}Failed to parse request ID (UUID) from approval output${NC}" >&2
    exit 1
  fi

  echo -e "${GREEN}Created approval request: $REQUEST_ID for commit $GIT_COMMIT_HASH${NC}"

  # Unset API token temporarily to log in as admin for approval
  unset RACK_GATEWAY_API_TOKEN
  unset RACK_GATEWAY_URL
  unset RACK_GATEWAY_RACK

  # Log in as admin to approve the request
  login_cli_as "admin@example.com" "e2e"

  APPROVE_CODE=$(generate_totp_code "${MFA_TOTP_SECRETS[admin@example.com]}")
  verify_rgw_command \
    "deploy-approval approve $REQUEST_ID --notes 'Approved for E2E test' --mfa-code $APPROVE_CODE" \
    "Deploy approval request" "approved"

  # Log out admin and switch back to API token
  logout_cli

  export RACK_GATEWAY_API_TOKEN="$API_TOKEN"
  export RACK_GATEWAY_URL="http://127.0.0.1:${GATEWAY_PORT}"
  export RACK_GATEWAY_RACK="Test"

  # Test manifest validation with invalid image tags (should fail)
  echo -e "${BLUE}Testing manifest validation with invalid image tags...${NC}"
  TESTDATA_DIR="$(dirname "$0")/cli-e2e-testdata"

  set +e
  invalid_deploy_output=$(./bin/rack-gateway deploy . --app rack-gateway --manifest "$TESTDATA_DIR/convox.invalid-image-tag.yml" --description "invalid manifest test" 2>&1)
  invalid_deploy_status=$?
  set -e

  echo "Invalid deploy output: $invalid_deploy_output" >&2

  if [[ $invalid_deploy_status -eq 0 ]]; then
    echo -e "${RED}Expected invalid manifest deploy to fail, but it succeeded${NC}" >&2
    exit 1
  fi
  if ! echo "$invalid_deploy_output" | grep -q "manifest validation failed"; then
    echo -e "${RED}Expected 'manifest validation failed' error message${NC}" >&2
    exit 1
  fi
  echo -e "${GREEN}Invalid image tags correctly rejected${NC}"

  # Test manifest with no image tags (uses build:) - should fail
  echo -e "${BLUE}Testing manifest validation with build instead of image...${NC}"
  set +e
  no_image_deploy_output=$(./bin/rack-gateway deploy . --app rack-gateway --manifest "$TESTDATA_DIR/convox.no-image-tag.yml" --description "no image test" 2>&1)
  no_image_deploy_status=$?
  set -e

  echo "No image deploy output: $no_image_deploy_output" >&2

  if [[ $no_image_deploy_status -eq 0 ]]; then
    echo -e "${RED}Expected no-image manifest deploy to fail, but it succeeded${NC}" >&2
    exit 1
  fi
  if ! echo "$no_image_deploy_output" | grep -q "must use a pre-built image"; then
    echo -e "${RED}Expected 'must use a pre-built image' error message${NC}" >&2
    exit 1
  fi
  echo -e "${GREEN}Build-based manifest correctly rejected${NC}"

  # Test with actual rack-gateway manifest with no image tags (uses build:) - should fail
  echo -e "${BLUE}Testing manifest validation with build instead of image...${NC}"
  set +e
  rack_gateway_deploy_output=$(./bin/rack-gateway deploy 2>&1)
  rack_gateway_deploy_output=$?
  set -e

  echo "No image deploy output: $no_image_deploy_output" >&2

  if [[ $rack_gateway_deploy_output -eq 0 ]]; then
    echo -e "${RED}Expected rack gateway deploy to fail, but it succeeded${NC}" >&2
    exit 1
  fi
  if ! echo "$rack_gateway_deploy_output" | grep -q "must use a pre-built image"; then
    echo -e "${RED}Expected 'must use a pre-built image' error message${NC}" >&2
    exit 1
  fi
  echo -e "${GREEN}Rack-gateway build-based manifest correctly rejected${NC}"

  # Test with valid manifest (should succeed)
  echo -e "${BLUE}Testing manifest validation with valid image tags...${NC}"
  set +e
  valid_deploy_output=$(./bin/rack-gateway deploy . --app rack-gateway --manifest "$TESTDATA_DIR/convox.valid-image-tag.yml" --description "valid manifest test" 2>&1)
  valid_deploy_status=$?
  set -e
  echo "Valid deploy output: $valid_deploy_output" >&2

  if [[ $valid_deploy_status -ne 0 ]]; then
    echo -e "${RED}Valid manifest deploy failed${NC}" >&2
    exit 1
  fi
  echo -e "${GREEN}Valid manifest deploy succeeded${NC}"

  # Now build with the approved commit
  echo -e "${BLUE}Running build after approval...${NC}"
  set +e
  build_output=$(./bin/rack-gateway build --app rack-gateway --description "cli-e2e build for commit $GIT_COMMIT_HASH" 2>&1)
  build_status=$?
  set -e
  if [[ $build_status -ne 0 ]]; then
    echo -e "${RED}build failed:${NC}\n$build_output" >&2
    exit 1
  fi
  echo "$build_output"

  # Extract release ID from "Release: RXXX" line
  RELEASE_ID=$(echo "$build_output" | grep "^Release:" | awk '{print $2}')

  if [[ -z "$RELEASE_ID" ]]; then
    echo -e "${RED}Failed to parse release id from build output${NC}" >&2
    exit 1
  fi

  # Verify release ID format (starts with R, followed by alphanumeric)
  if ! [[ "$RELEASE_ID" =~ ^R[A-Z0-9-]+$ ]]; then
    echo -e "${RED}Release ID has unexpected format: '$RELEASE_ID'${NC}" >&2
    exit 1
  fi

  echo -e "${GREEN}Build created release: $RELEASE_ID${NC}"


  # No unapproved commands allowed
  verify_rgw_command_failure \
    "run web --app rack-gateway 'delete everything'" \
    "Error: websocket: bad handshake"

  # But now an approve   command is allowed to be run for that release ID
  verify_rgw_command "run web --app rack-gateway --release $RELEASE_ID 'echo hello'" \
    'Connected to mock exec for app=rack-gateway pid=proc-123456' \
    '$ echo hello'

  # Run mock migration command on the new release
  verify_rgw_command \
    "run web --app rack-gateway --release $RELEASE_ID 'echo migrate'" \
    "migrate"

  # Promote the release
  verify_rgw_command \
    "releases promote $RELEASE_ID --app rack-gateway" \
    "OK"

  # Deploy approval request has been consumed. No more commands allowed
  verify_rgw_command_failure \
    "run web 'echo hello'" \
    "ERROR:"

  # Clean up the API token now that the pipeline simulation is complete
  unset RACK_GATEWAY_API_TOKEN
  unset RACK_GATEWAY_URL
  unset RACK_GATEWAY_RACK

  # Delete via admin login to validate token deletion flow
  login_cli_as "admin@example.com" "e2e"
  verify_rgw_command "api-token delete $API_TOKEN_PUBLIC_ID" "Deleted token $API_TOKEN_PUBLIC_ID"
  logout_cli
fi

if [ -z "$SKIP_DEPLOYER_TESTS" ]; then
  # Login as deployer
  # ---------------------------------------------
  login_cli_as "deployer@example.com" "e2e"

  # Can list processes
  verify_rgw_command "ps" "p-web-1" "p-worker-1"

  # List environment for a known app
  verify_rgw_command "env" \
    "DATABASE_URL=********************" "NODE_ENV=production" "PORT=3000"

  # Cannot fetch secret
  verify_rgw_command_failure "env get DATABASE_URL --unmask" \
    "Error: failed to fetch env: You don't have permission to view secrets."

  # (env set tests removed for deployer; protected env policy preservation)

  # Should not be able to delete apps
  verify_rgw_command_failure "apps delete rack-gateway" "ERROR: permission denied"

  logout_cli
fi

if [ -z "$SKIP_VIEWER_TESTS" ]; then
  # Login as viewer
  # ---------------------------------------------
  login_cli_as "viewer@example.com" "e2e"

  # Viewer can list processes
  verify_rgw_command "ps" "p-web-1" "p-worker-1"

  # Cannot fetch env
  verify_rgw_command_failure "env" \
    "ERROR: permission denied"

  # Cannot fetch secret
  verify_rgw_command_failure "env get DATABASE_URL --unmask" \
    "Error: failed to fetch env: You don't have permission to view environment variables."

  # Viewer should not be able to set env or delete apps
  verify_rgw_command_failure "env set NOTALLOWED=1" "ERROR: permission denied"
  verify_rgw_command_failure "apps delete rack-gateway" "ERROR: permission denied"
fi

echo -e "${GREEN}CLI E2E completed successfully.${NC}"
