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
  ["admin@example.com"]="K745D33R6A3NCWP5C3NYDQMBQF5ZFFHU"
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
INSERT INTO settings (app_name, key, value, updated_at)
     VALUES (NULL, 'mfa', jsonb_build_object('require_all_users', false), NOW())
ON CONFLICT (app_name, key) DO UPDATE
     SET value = jsonb_set(settings.value, '{require_all_users}', 'false'::jsonb, true),
         updated_at = NOW();
-- Allow specific commands for E2E tests (deploy approvals)
INSERT INTO settings (app_name, key, value, updated_at)
     VALUES (NULL, 'approved_commands', '{"commands": ["echo rake db:migrate"]}'::jsonb, NOW())
ON CONFLICT (app_name, key) DO UPDATE
     SET value = '{"commands": ["echo rake db:migrate"]}'::jsonb,
         updated_at = NOW();
-- Configure image tag pattern for manifest validation
INSERT INTO settings (app_name, key, value, updated_at)
     VALUES ('rack-gateway', 'service_image_patterns', '{"gateway": ".*:{{GIT_COMMIT}}-amd64"}'::jsonb, NOW())
ON CONFLICT (app_name, key) DO UPDATE
     SET value = '{"gateway": ".*:{{GIT_COMMIT}}-amd64"}'::jsonb,
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

clear_mfa_replay_protection() {
  psql_exec "DELETE FROM used_totp_steps; DELETE FROM mfa_totp_attempts;"
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

  # Clear MFA replay protection
  clear_mfa_replay_protection

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

if [ -z "$SKIP_ADMIN_TESTS" ]; then
  verify_rgw_command "rack" "Current rack: e2e" "Logged in as admin@example.com"
  verify_rgw_command "rack info" "mock-rack" "mock-rack.example.com"
  verify_rgw_command "apps" "rack-gateway" "RAPI123456"
  verify_rgw_command "apps info" \
    "Name        rack-gateway" "Status      running"
  verify_rgw_command "ps" "p-web-1" "p-worker-1"

  verify_rgw_command "run web 'echo rake db:migrate'" \
    'Connected to mock exec for app=rack-gateway pid=proc-123456' \
    '$ echo rake db:migrate' \
    'rake db:migrate' \
    'Exit code: 0' \
    'Session closed.'

  verify_rgw_command "exec p-worker-1 'echo rake db:migrate'" \
    'Connected to mock exec for app=rack-gateway pid=p-worker-1' \
    '$ echo rake db:migrate' \
    'rake db:migrate' \
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

  # Can't deploy to rack-gateway without specific image tag
  verify_rgw_command_failure "deploy" \
    "Error: manifest validation failed: service gateway must use a pre-built image"

  # Test full build + release flow
  verify_rgw_command "deploy --app other-app" \
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

  # Clear MFA replay protection
  clear_mfa_replay_protection
  MFA_CODE=$(generate_totp_code "${MFA_TOTP_SECRETS[admin@example.com]}")
  API_TOKEN_JSON=$(./bin/rack-gateway api-token create \
    --name "E2E CLI API Token ${E2E_TS}" \
    --role cicd \
    --mfa-code "$MFA_CODE" \
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
    "run web --app rack-gateway 'echo rake db:migrate'" \
    "Error: You don't have permission to run processes"


  # Request approval as API token (BEFORE building - git commit-based flow)
  echo -e "${YELLOW}API token requesting deploy approval for git commit...${NC}"
  GIT_COMMIT_HASH=$(git rev-parse HEAD)  # Use actual git commit hash
  GIT_BRANCH=$(git rev-parse --abbrev-ref HEAD)  # Use actual branch name
  CI_METADATA='{"workflow_id":"test-workflow-'${E2E_TS}'","pipeline_number":"'${E2E_TS}'"}'

  set +e
  approval_output=$(./bin/rack-gateway deploy-approval request \
    --git-commit "$GIT_COMMIT_HASH" \
    --branch "$GIT_BRANCH" \
    --ci-metadata "$CI_METADATA" \
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

  # Clear MFA replay protection for approve command
  clear_mfa_replay_protection

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
  echo -e "${BLUE}Test 1/4: Invalid image tags (should fail)...${NC}"
  TESTDATA_DIR="scripts/cli-e2e-testdata"

  set +e
  invalid_deploy_output=$(./bin/rack-gateway deploy . --app rack-gateway --manifest "$TESTDATA_DIR/convox.invalid-image-tag.yml" --description "invalid manifest test" 2>&1)
  invalid_deploy_status=$?
  set -e

  echo "Output: $invalid_deploy_output" >&2

  if [[ $invalid_deploy_status -eq 0 ]]; then
    echo -e "${RED}Expected invalid manifest deploy to fail, but it succeeded${NC}" >&2
    exit 1
  fi
  if ! echo "$invalid_deploy_output" | grep -q "manifest validation failed"; then
    echo -e "${RED}Expected 'manifest validation failed' error message${NC}" >&2
    exit 1
  fi
  echo -e "${GREEN}✓ Invalid image tags correctly rejected${NC}"

  # Test 2: Duplicate object upload (should fail with "archive already uploaded")
  echo -e "${BLUE}Test 2/5: Duplicate object upload (should fail)...${NC}"
  set +e
  duplicate_upload_output=$(./bin/rack-gateway deploy . --app rack-gateway --manifest "$TESTDATA_DIR/convox.no-image-tag.yml" --description "duplicate upload test" 2>&1)
  duplicate_upload_status=$?
  set -e

  echo "Output: $duplicate_upload_output" >&2

  if [[ $duplicate_upload_status -eq 0 ]]; then
    echo -e "${RED}Expected duplicate upload to fail, but it succeeded${NC}" >&2
    exit 1
  fi
  if ! echo "$duplicate_upload_output" | grep -q "an archive has already been uploaded for this deploy approval request"; then
    echo -e "${RED}Expected 'an archive has already been uploaded' error message${NC}" >&2
    exit 1
  fi
  echo -e "${GREEN}✓ Duplicate object upload correctly rejected${NC}"

  # Clear object_url to allow next test
  echo -e "${YELLOW}Clearing object_url for next test...${NC}"
  psql_exec "UPDATE deploy_approval_requests SET object_url = NULL WHERE git_commit_hash = '$GIT_COMMIT_HASH';"

  # Test 3: Build-based manifest (should fail with "must use pre-built image")
  echo -e "${BLUE}Test 3/5: Build-based manifest (should fail)...${NC}"
  set +e
  no_image_deploy_output=$(./bin/rack-gateway deploy . --app rack-gateway --manifest "$TESTDATA_DIR/convox.no-image-tag.yml" --description "no image test" 2>&1)
  no_image_deploy_status=$?
  set -e

  echo "Output: $no_image_deploy_output" >&2

  if [[ $no_image_deploy_status -eq 0 ]]; then
    echo -e "${RED}Expected no-image manifest deploy to fail, but it succeeded${NC}" >&2
    exit 1
  fi
  if ! echo "$no_image_deploy_output" | grep -q "must use a pre-built image"; then
    echo -e "${RED}Expected 'must use a pre-built image' error message${NC}" >&2
    exit 1
  fi
  echo -e "${GREEN}✓ Build-based manifest correctly rejected${NC}"

  # Clear object_url for successful build, keep build_id/release_id NULL
  echo -e "${YELLOW}Clearing object_url for successful build...${NC}"
  psql_exec "UPDATE deploy_approval_requests SET object_url = NULL WHERE git_commit_hash = '$GIT_COMMIT_HASH';"

  # Generate valid manifest with correct git commit for initial successful build
  echo -e "${BLUE}Test 4/6: First successful build with valid manifest (commit $GIT_COMMIT_HASH)...${NC}"
  cat > "$TESTDATA_DIR/convox.valid-image-tag.yml" <<EOF
services:
  gateway:
    image: docspringcom/rack-gateway:${GIT_COMMIT_HASH}-amd64
    port: 8080
EOF

  # Test with valid manifest - this will succeed and set build_id/release_id
  set +e
  first_build_output=$(./bin/rack-gateway build . --app rack-gateway --manifest "$TESTDATA_DIR/convox.valid-image-tag.yml" --description "first successful build for commit $GIT_COMMIT_HASH" 2>&1)
  first_build_status=$?
  set -e
  echo "Output: $first_build_output" >&2

  if [[ $first_build_status -ne 0 ]]; then
    echo -e "${RED}First valid manifest build failed${NC}" >&2
    exit 1
  fi
  echo -e "${GREEN}✓ First valid manifest build succeeded${NC}"

  # Test 5: Duplicate build creation (should fail with "build already created")
  echo -e "${BLUE}Test 5/6: Duplicate build creation (should fail)...${NC}"

  # Clear object_url so we can upload again, but keep build_id/release_id set
  echo -e "${YELLOW}Clearing object_url to allow re-upload, keeping build_id...${NC}"
  psql_exec "UPDATE deploy_approval_requests SET object_url = NULL WHERE git_commit_hash = '$GIT_COMMIT_HASH';"

  set +e
  duplicate_build_output=$(./bin/rack-gateway build . --app rack-gateway --manifest "$TESTDATA_DIR/convox.valid-image-tag.yml" --description "duplicate build test" 2>&1)
  duplicate_build_status=$?
  set -e

  echo "Output: $duplicate_build_output" >&2

  if [[ $duplicate_build_status -eq 0 ]]; then
    echo -e "${RED}Expected duplicate build to fail, but it succeeded${NC}" >&2
    exit 1
  fi
  if ! echo "$duplicate_build_output" | grep -q "a build has already been created for this deploy approval request"; then
    echo -e "${RED}Expected 'a build has already been created' error message${NC}" >&2
    exit 1
  fi
  echo -e "${GREEN}✓ Duplicate build creation correctly rejected${NC}"

  # Test 6: Root convox.yml with build (should fail with manifest error)
  echo -e "${BLUE}Test 6/6: Default convox.yml with build (should fail)...${NC}"

  # Clear everything for this test
  echo -e "${YELLOW}Clearing all fields for root manifest test...${NC}"
  psql_exec "UPDATE deploy_approval_requests SET object_url = NULL, build_id = NULL, release_id = NULL WHERE git_commit_hash = '$GIT_COMMIT_HASH';"

  set +e
  root_deploy_output=$(./bin/rack-gateway deploy 2>&1)
  root_deploy_status=$?
  set -e

  echo "Output: $root_deploy_output" >&2

  if [[ $root_deploy_status -eq 0 ]]; then
    echo -e "${RED}Expected root convox.yml deploy to fail, but it succeeded${NC}" >&2
    exit 1
  fi
  if ! echo "$root_deploy_output" | grep -q "must use a pre-built image"; then
    echo -e "${RED}Expected 'must use a pre-built image' error message${NC}" >&2
    exit 1
  fi
  echo -e "${GREEN}✓ Root convox.yml correctly rejected${NC}"

  # Clear all fields for final successful deployment
  echo -e "${YELLOW}Clearing all fields for final successful test...${NC}"
  psql_exec "UPDATE deploy_approval_requests SET object_url = NULL, build_id = NULL, release_id = NULL WHERE git_commit_hash = '$GIT_COMMIT_HASH';"

  # Final successful build and promote
  set +e
  final_build_output=$(./bin/rack-gateway build . --app rack-gateway --manifest "$TESTDATA_DIR/convox.valid-image-tag.yml" --description "final cli-e2e build for commit $GIT_COMMIT_HASH" 2>&1)
  final_build_status=$?
  set -e
  echo "Output: $final_build_output" >&2

  if [[ $final_build_status -ne 0 ]]; then
    echo -e "${RED}Final manifest build failed${NC}" >&2
    exit 1
  fi
  echo -e "${GREEN}✓ Final manifest build succeeded${NC}"

  # Use the final build output for the rest of the tests
  valid_build_output="$final_build_output"


  # Extract release ID from "Release: RXXX" line
  RELEASE_ID=$(echo "$valid_build_output" | grep "^Release:" | awk '{print $2}')

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
    "Error: You don't have permission to run processes"

  # But now an approved command is allowed to be run for that release ID
  verify_rgw_command "run web --app rack-gateway --release $RELEASE_ID 'echo rake db:migrate'" \
    'Connected to mock exec for app=rack-gateway pid=proc-123456' \
    '$ echo rake db:migrate'

  # Cannot promote a different release
  verify_rgw_command_failure \
    "releases promote ROTHER --app rack-gateway" \
    "Error: You don't have permission to promote releases"

  # Promote the release
  verify_rgw_command \
    "releases promote $RELEASE_ID --app rack-gateway" \
    "OK"

  # Deploy approval request has been completed. No more commands allowed for that release
  verify_rgw_command_failure "run web --release $RELEASE_ID 'echo rake db:migrate'" \
    "Error: You don't have permission to run processes"

  # ... or without a release ID
  verify_rgw_command_failure "run web 'echo rake db:migrate'" \
    "Error: You don't have permission to run processes"

  # Clean up the API token now that the pipeline simulation is complete
  unset RACK_GATEWAY_API_TOKEN
  unset RACK_GATEWAY_URL
  unset RACK_GATEWAY_RACK

  # Delete via admin login to validate token deletion flow
  login_cli_as "admin@example.com" "e2e"

  # Clear MFA replay protection for token deletion
  clear_mfa_replay_protection
  DELETE_CODE=$(generate_totp_code "${MFA_TOTP_SECRETS[admin@example.com]}")
  verify_rgw_command "api-token delete $API_TOKEN_PUBLIC_ID --mfa-code $DELETE_CODE" "Deleted token $API_TOKEN_PUBLIC_ID"
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
  clear_mfa_replay_protection
  DELETE_CODE=$(generate_totp_code "${MFA_TOTP_SECRETS[deployer@example.com]}")
  verify_rgw_command_failure "apps delete rack-gateway --mfa-code $DELETE_CODE" "Error: permission denied"

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
    "Error: failed to fetch env: You don't have permission to view environment variables"

  # Cannot fetch secret
  verify_rgw_command_failure "env get DATABASE_URL --unmask" \
    "Error: failed to fetch env: You don't have permission to view environment variables."

  # Viewer should not be able to set env or delete apps
  verify_rgw_command_failure "env set NOTALLOWED=1" "Error: permission denied"
  clear_mfa_replay_protection
  DELETE_CODE=$(generate_totp_code "${MFA_TOTP_SECRETS[viewer@example.com]}")
  verify_rgw_command_failure "apps delete rack-gateway --mfa-code $DELETE_CODE" "Error: permission denied"
fi

echo -e "${GREEN}CLI E2E completed successfully.${NC}"
