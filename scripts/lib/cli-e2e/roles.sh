# shellcheck shell=bash
# CLI E2E stages for the deployer and viewer roles: what each can do, and the refusals it gets.

run_deployer_tests() {
    if [[ -n "$SKIP_DEPLOYER_TESTS" ]]; then
        return
    fi

    login_cli_as "deployer@example.com" "e2e"

    verify_rgw_command "ps" "p-web-1" "p-worker-1"
    verify_rgw_command "env" "DATABASE_URL=********************" "NODE_ENV=production" "PORT=3000"
    verify_rgw_command_failure "env get DATABASE_URL --unmask" \
        "Error: failed to fetch env: You don't have permission to view secrets."

    clear_mfa_replay_protection
    local delete_code
    delete_code=$(generate_totp_code "$(get_totp_secret 'deployer@example.com')")
    verify_rgw_command_failure "apps delete rack-gateway --mfa-code $delete_code" "Error: permission denied"

    logout_cli
}

run_viewer_tests() {
    if [[ -n "$SKIP_VIEWER_TESTS" ]]; then
        return
    fi

    login_cli_as "viewer@example.com" "e2e"

    verify_rgw_command "ps" "p-web-1" "p-worker-1"
    verify_rgw_command_failure "env" \
        "Error: failed to fetch env: insufficient permissions: requires convox:env:read"
    verify_rgw_command_failure "env get DATABASE_URL --unmask" \
        "Error: failed to fetch env: insufficient permissions: requires convox:env:read"

    # The gateway refuses a viewer's exec at the WebSocket handshake, and the CLI shows its reason.
    clear_mfa_replay_protection
    local exec_code
    exec_code=$(generate_totp_code "$(get_totp_secret 'viewer@example.com')")
    verify_rgw_command_failure "exec p-web-1 --mfa-code $exec_code 'echo hi'" \
        "Error: the gateway refused the connection (403 Forbidden): You don't have permission to run processes."

    clear_mfa_replay_protection
    local delete_code
    delete_code=$(generate_totp_code "$(get_totp_secret 'viewer@example.com')")
    verify_rgw_command_failure "env set NOTALLOWED=1" "Error: permission denied"
    verify_rgw_command_failure "apps delete rack-gateway --mfa-code $delete_code" "Error: permission denied"
}
