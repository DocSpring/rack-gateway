# shellcheck shell=bash

psql_query() {
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
    echo "$output"
}

psql_exec() {
    psql_query "$1" > /dev/null
}

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

assert_deploy_approval_fields() {
    local git_commit_hash="$1"
    local expected_object_url="${2:-}"
    local expected_build_id="${3:-}"
    local expected_release_id="${4:-}"

    local result
    result=$(psql_query "SELECT object_url, build_id, release_id FROM deploy_approval_requests WHERE git_commit_hash = '$git_commit_hash' LIMIT 1;")

    if [[ -z "$result" ]]; then
        echo -e "${RED}ASSERTION FAILED: No deploy approval found for commit $git_commit_hash${NC}" >&2
        return 1
    fi

    local actual_object_url actual_build_id actual_release_id
    IFS=$'\t' read -r actual_object_url actual_build_id actual_release_id <<< "$result"

    local failed=0

    # Check object_url: "NOT_EMPTY" means any non-empty value
    if [[ "$expected_object_url" == "NOT_EMPTY" ]]; then
        if [[ -z "$actual_object_url" ]]; then
            echo -e "${RED}ASSERTION FAILED: object_url should be set but is empty${NC}" >&2
            failed=1
        fi
    elif [[ "$actual_object_url" != "$expected_object_url" ]]; then
        echo -e "${RED}ASSERTION FAILED: object_url mismatch${NC}" >&2
        echo -e "  Expected: '$expected_object_url'" >&2
        echo -e "  Actual:   '$actual_object_url'" >&2
        failed=1
    fi

    # Check build_id: "NOT_EMPTY" means any non-empty value
    if [[ "$expected_build_id" == "NOT_EMPTY" ]]; then
        if [[ -z "$actual_build_id" ]]; then
            echo -e "${RED}ASSERTION FAILED: build_id should be set but is empty${NC}" >&2
            failed=1
        fi
    elif [[ "$actual_build_id" != "$expected_build_id" ]]; then
        echo -e "${RED}ASSERTION FAILED: build_id mismatch${NC}" >&2
        echo -e "  Expected: '$expected_build_id'" >&2
        echo -e "  Actual:   '$actual_build_id'" >&2
        failed=1
    fi

    # Check release_id: "NOT_EMPTY" means any non-empty value
    if [[ "$expected_release_id" == "NOT_EMPTY" ]]; then
        if [[ -z "$actual_release_id" ]]; then
            echo -e "${RED}ASSERTION FAILED: release_id should be set but is empty${NC}" >&2
            failed=1
        fi
    elif [[ "$actual_release_id" != "$expected_release_id" ]]; then
        echo -e "${RED}ASSERTION FAILED: release_id mismatch${NC}" >&2
        echo -e "  Expected: '$expected_release_id'" >&2
        echo -e "  Actual:   '$actual_release_id'" >&2
        failed=1
    fi

    if [[ $failed -eq 1 ]]; then
        return 1
    fi

    echo -e "${GREEN}✓ Deploy approval fields match: object_url='$actual_object_url' build_id='$actual_build_id' release_id='$actual_release_id'${NC}"
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
TRUNCATE TABLE settings RESTART IDENTITY CASCADE;
INSERT INTO settings (app_name, key, value, updated_at) VALUES
  (NULL, 'mfa_require_all_users', 'false'::jsonb, NOW()),
  (NULL, 'mfa_trusted_device_ttl_days', '30'::jsonb, NOW()),
  (NULL, 'mfa_step_up_window_minutes', '10'::jsonb, NOW()),
  (NULL, 'allow_destructive_actions', 'false'::jsonb, NOW()),
  (NULL, 'deploy_approvals_enabled', 'true'::jsonb, NOW()),
  (NULL, 'deploy_approval_window_minutes', '15'::jsonb, NOW()),
  ('rack-gateway', 'approved_deploy_commands', '{"commands": ["echo rake db:migrate"]}'::jsonb, NOW()),
  ('rack-gateway', 'service_image_patterns', '{"gateway": ".*:{{GIT_COMMIT}}-amd64"}'::jsonb, NOW())
ON CONFLICT (COALESCE(app_name, ''), key) DO UPDATE
  SET value = EXCLUDED.value,
      updated_at = NOW();
SQL
    )

    psql_exec "$sql"
}
