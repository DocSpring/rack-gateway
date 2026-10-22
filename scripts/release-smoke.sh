#!/usr/bin/env bash
# Smoke-tests the release gateway image, built without the testbypass tag, with a production configuration. The E2E
# suite can't cover this: it runs a testbypass build with DEV_MODE and E2E_TEST_MODE on. Checks that:
#   - the binary reports its test bypasses compiled out
#   - the runtime user can read the CA bundle convox.yml gives as PGSSLROOTCERT
#   - it refuses to start (or run a command) while a test bypass is set, naming each one, or with the published
#     development column encryption key
#   - the server refuses the published development APP_SECRET_KEY and secrets shorter than 32 bytes
#   - it migrates, starts, marks its database production and serves the health check
#   - production behaviour is on: HSTS and a strict CSP, no dev-mode localhost origins, Secure cookies, OAuth against
#     Google over https, and no E2E flag in the web UI
#
# Uses the gateway-api-release service in docker-compose.yml, whose image must already exist (task
# docker:release:smoke builds it; CI loads it). Needs network access to accounts.google.com for OIDC discovery.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICE="gateway-api-release"
DATABASE="gateway_release"
BASE_URL="http://127.0.0.1:${RELEASE_GATEWAY_PORT:-9460}"
MAX_WAIT_SECONDS="${MAX_WAIT_SECONDS:-60}"
# Fresh secrets: release builds refuse the development values published in this repository, and secrets shorter
# than 32 bytes.
RELEASE_COLUMN_ENCRYPTION_KEY="$(openssl rand -base64 32)"
RELEASE_APP_SECRET_KEY="$(openssl rand -base64 32)"
RELEASE_AUDIT_HMAC_SECRET="$(openssl rand -base64 32)"
export RELEASE_COLUMN_ENCRYPTION_KEY RELEASE_APP_SECRET_KEY RELEASE_AUDIT_HMAC_SECRET
DEV_COLUMN_KEY="cmFjay1nYXRld2F5LWRldi1vbmx5LWNvbHVtbi1rZXk="
DEV_APP_SECRET_KEY="dev-secret-key-change-in-production"

compose() {
  docker compose --profile release "$@"
}

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# shellcheck disable=SC2329 # invoked by the EXIT trap
cleanup() {
  local status=$?
  if [ "$status" -ne 0 ]; then
    compose logs --no-color "$SERVICE" >&2 || true
  fi
  compose stop "$SERVICE" >/dev/null 2>&1 || true
  compose rm -f -v "$SERVICE" >/dev/null 2>&1 || true
}
trap cleanup EXIT

expect() {
  local description="$1" haystack="$2" needle="$3"
  grep -qF -- "$needle" <<<"$haystack" || fail "${description}: expected '${needle}' in:"$'\n'"${haystack}"
}

refute() {
  local description="$1" haystack="$2" needle="$3"
  if grep -qF -- "$needle" <<<"$haystack"; then
    fail "${description}: unexpected '${needle}' in:"$'\n'"${haystack}"
  fi
}

# Runs the release image with the given arguments; the output goes to stdout either way. The timeout stops a
# server that should have refused to start from hanging the job.
run_gateway() {
  timeout 120 docker compose --profile release run --rm --no-deps -T "$@" 2>&1
}

echo "== Build info"
info="$(run_gateway "$SERVICE" ./rack-gateway-api build-info)" || fail "build-info failed:"$'\n'"${info}"
echo "$info"
expect "build-info" "$info" "test bypasses: compiled out"

# The smoke database runs without TLS, so the server never reads the bundle here; a rack's verify-full does.
echo "== Reads the database CA bundle"
mapfile -t ca_bundles < <(sed -n 's/^[[:space:]]*- PGSSLROOTCERT=//p' "$SCRIPT_DIR/../convox.yml" | sort -u)
[ "${#ca_bundles[@]}" -gt 0 ] || fail "convox.yml gives no PGSSLROOTCERT"
for bundle in "${ca_bundles[@]}"; do
  output="$(run_gateway --entrypoint /bin/sh "$SERVICE" -c "grep -q 'BEGIN CERTIFICATE' '${bundle}'")" ||
    fail "the runtime user can't read ${bundle}:"$'\n'"${output}"
  echo "Read ${bundle}"
done

echo "== Refuses to start with test bypasses"
bypasses=(
  -e DEV_MODE=true -e E2E_TEST_MODE=true -e COOKIE_SECURE=false -e AWS_ENDPOINT_URL_S3=http://minio:9000
  -e POSTMARK_API_BASE=http://mock-postmark -e GOOGLE_OAUTH_BASE_URL=http://mock-oauth:3345
)
for command in "" "./rack-gateway-api migrate"; do
  # shellcheck disable=SC2086 # the command is deliberately word-split; "" runs the image's default (the server)
  if output="$(run_gateway "${bypasses[@]}" "$SERVICE" $command)"; then
    fail "the release image ran '${command:-server}' with test bypasses set:"$'\n'"${output}"
  fi
  expect "refusal (${command:-server})" "$output" "doesn't support test bypasses"
  expect "refusal (${command:-server})" "$output" \
    "DEV_MODE, E2E_TEST_MODE, COOKIE_SECURE=false, AWS_ENDPOINT_URL_S3, POSTMARK_API_BASE, GOOGLE_OAUTH_BASE_URL"
done
echo "Refused, naming every bypass"

echo "== Refuses the development column encryption key"
if output="$(run_gateway -e COLUMN_ENCRYPTION_KEY="$DEV_COLUMN_KEY" "$SERVICE" ./rack-gateway-api migrate)"; then
  fail "the release image ran migrate with the development column key:"$'\n'"${output}"
fi
expect "development key refusal" "$output" "is the development key published in this repository"
echo "Refused"

echo "== Database"
compose up -d --wait postgres
compose exec -T postgres psql -U postgres -d postgres -v ON_ERROR_STOP=1 -q <"$SCRIPT_DIR/audit-roles.sql"
compose exec -T postgres psql -U postgres -d postgres -v ON_ERROR_STOP=1 -q \
  -c "DROP DATABASE IF EXISTS ${DATABASE} WITH (FORCE)" -c "CREATE DATABASE ${DATABASE}"
run_gateway "$SERVICE" ./rack-gateway-api migrate

echo "== Refuses weak secrets"
weak_secrets=(-e APP_SECRET_KEY="$DEV_APP_SECRET_KEY" -e AUDIT_HMAC_SECRET=too-short)
if output="$(run_gateway "${weak_secrets[@]}" "$SERVICE")"; then
  fail "the release image started with weak secrets:"$'\n'"${output}"
fi
expect "weak secret refusal" "$output" "APP_SECRET_KEY (the development value published in this repository)"
expect "weak secret refusal" "$output" "AUDIT_HMAC_SECRET (shorter than 32 bytes)"
echo "Refused"

echo "== Start"
compose up -d --no-build "$SERVICE"
for attempt in $(seq 1 "$MAX_WAIT_SECONDS"); do
  if curl --max-time 10 -fsS "${BASE_URL}/api/v1/health" >/dev/null 2>&1; then
    break
  fi
  if [ "$attempt" -eq "$MAX_WAIT_SECONDS" ]; then
    fail "the release gateway didn't become healthy within ${MAX_WAIT_SECONDS}s"
  fi
  sleep 1
done
environment="$(compose exec -T postgres psql -U postgres -d "$DATABASE" -tAc \
  "SELECT environment FROM rgw_internal_metadata")"
[ "$environment" = "production" ] || fail "database marked '${environment}', expected production"
echo "Healthy; database marked production"

echo "== Production behaviour"
headers="$(curl --max-time 10 -fsS -D - -o /dev/null "${BASE_URL}/api/v1/health")"
expect "HSTS" "$headers" "Strict-Transport-Security: max-age=63072000"
expect "CSP" "$headers" "Content-Security-Policy: default-src 'self'"
refute "CSP" "$headers" "unsafe-inline"
refute "CSP" "$headers" "localhost"

status="$(curl --max-time 10 -sS -o /dev/null -w '%{http_code}' -H "Origin: http://localhost:5223" "${BASE_URL}/api/v1/health")"
[ "$status" = "400" ] || fail "a localhost Origin got ${status}, expected 400 (dev mode allows it)"

login="$(curl --max-time 10 -sS -D - -o /dev/null "${BASE_URL}/api/v1/auth/web/login")"
expect "OAuth login" "$login" "Location: https://accounts.google.com/"
expect "OAuth login" "$login" "redirect_uri=https%3A%2F%2Fgateway.example.test%2F"
state_cookie="$(grep -i '^Set-Cookie: rgw_oauth_state=' <<<"$login" || true)"
expect "OAuth state cookie" "$state_cookie" "Secure"

page="$(curl --max-time 10 -fsS "${BASE_URL}/app/login")"
expect "web UI" "$page" "window.__nonce__"
refute "web UI" "$page" "__e2e_test_mode__"

echo "Release image smoke test passed"
