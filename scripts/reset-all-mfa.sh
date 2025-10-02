#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

psql_exec() {
  local sql="$1"
  local output
  set +e
  output=$(docker exec -i rack-gateway-postgres-1 psql -U postgres -d gateway -At -F $'\t' -c "$sql" 2>&1)
  local status=$?
  set -e
  if [[ $status -ne 0 ]]; then
    echo "$output" >&2
    exit $status
  fi
}

reset_all_mfa_state() {
  if ! docker ps --format '{{.Names}}' | grep -q '^rack-gateway-postgres-1$'; then
    return 0
  fi

  echo "Resetting all MFA state..."

  local sql
  sql=$(cat <<'SQL'
UPDATE user_sessions SET trusted_device_id = NULL, mfa_verified_at = NULL, recent_step_up_at = NULL;
DELETE FROM trusted_devices;
DELETE FROM mfa_backup_codes;
DELETE FROM mfa_methods;
UPDATE users SET mfa_enrolled = FALSE, mfa_enforced_at = NULL;
SQL
  )

  psql_exec "$sql"
}

reset_all_mfa_state

# Disable MFA enforcement
psql_exec "UPDATE settings SET value = jsonb_set(value, '{require_all_users}', 'false'::jsonb, true), updated_at = NOW() WHERE key = 'mfa';"
