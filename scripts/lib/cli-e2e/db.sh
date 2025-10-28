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
    }

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
