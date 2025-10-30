# shellcheck shell=bash

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

resolve_ports() {
    local mise_file="mise.toml"

    GATEWAY_PORT="${GATEWAY_PORT:-${E2E_GATEWAY_PORT:-}}"
    if [[ -z "$GATEWAY_PORT" ]]; then
        GATEWAY_PORT="$(toml_get E2E_GATEWAY_PORT "$mise_file" || toml_get TEST_GATEWAY_PORT "$mise_file" || toml_get GATEWAY_PORT "$mise_file" || echo 8447)"
    fi
}

configure_stage_skips() {
    local stages="ADMIN API_TOKEN DEPLOYER VIEWER"
    for stage in $stages; do
        local var="ONLY_${stage}_TESTS"
        local val=""
        if [[ -n ${!var+x} ]]; then
            val="${!var}"
        fi
        if [[ -n "$val" ]]; then
            for other in $stages; do
                if [[ "$other" != "$stage" ]]; then
                    printf -v "SKIP_${other}_TESTS" %s true
                fi
            done
            break
        fi
    done

    for stage in $stages; do
        local var="SKIP_${stage}_TESTS"
        if [[ -z ${!var+x} ]]; then
            printf -v "$var" %s ""
        fi
    done
}

enforce_mfa_and_validate_cli_blocking() {
    echo -e "${YELLOW}Enabling MFA enforcement...${NC}"
    psql_exec "INSERT INTO settings (app_name, key, value, updated_at) VALUES (NULL, 'mfa_require_all_users', 'true'::jsonb, NOW()) ON CONFLICT (COALESCE(app_name, ''), key) DO UPDATE SET value = 'true'::jsonb, updated_at = NOW();"

    echo -e "${YELLOW}Restarting ${E2E_GATEWAY_SERVICE} to apply MFA setting...${NC}"
    docker compose restart "${E2E_GATEWAY_SERVICE}" >/dev/null
    WEB_PORT="${GATEWAY_PORT}" CHECK_VITE_PROXY=false ./scripts/wait-for-services.sh

    for user_email in "admin@example.com" "deployer@example.com" "viewer@example.com"; do
        reset_user_mfa "$user_email"
    done

    echo -e "${YELLOW}Verifying CLI login fails until MFA enrollment...${NC}"
    local auth_file output_file cookie_file
    auth_file="$(mktemp)"
    output_file="$(mktemp)"
    cookie_file="$(mktemp)"
    set -m
    ./bin/rack-gateway login "e2e" "http://127.0.0.1:${GATEWAY_PORT}" --no-open --auth-file "$auth_file" >"$output_file" 2>&1 &
    local cli_pid=$!
    for _i in $(seq 1 50); do
        [[ -s "$auth_file" ]] && break
        sleep 0.1
    done

    local auth_url state
    auth_url=$(sed -n 's/^AUTH_URL=//p' "$auth_file")
    state=$(sed -n 's/^STATE=//p' "$auth_file")
    if [[ -z "$auth_url" || -z "$state" ]]; then
        echo -e "${RED}CLI login did not produce AUTH_URL/STATE${NC}" >&2
        kill $cli_pid || true
        exit 1
    fi

    curl -s -L -c "$cookie_file" -b "$cookie_file" "${auth_url}&selected_user=admin@example.com" -o /dev/null || true

    set +e
    wait $cli_pid
    local cli_status=$?
    set -e
    set +m
    local cli_output
    cli_output=$(cat "$output_file")
    rm -f "$auth_file" "$output_file" "$cookie_file"

    if [[ $cli_status -eq 0 ]]; then
        echo -e "${RED}CLI login succeeded unexpectedly when MFA enrollment is required.${NC}" >&2
        echo "$cli_output" >&2
        exit 1
    fi

    if ! echo "$cli_output" | grep -Fq "Error: login failed: You must set up multi-factor authentication before you can continue using the CLI."; then
        echo -e "${RED}CLI did not report MFA enrollment error as expected.${NC}" >&2
        echo "$cli_output" >&2
        exit 1
    fi

    psql_exec "DELETE FROM cli_login_states"
}

provision_mfa() {
    for user_email in "admin@example.com" "deployer@example.com" "viewer@example.com"; do
        setup_user_mfa "$user_email" "$(get_totp_secret "$user_email")"
    done
}

run_cli_e2e_suite() {
    echo "Building CLI..."
    task go:build:cli

    configure_stage_skips
    resolve_ports

    echo "Running CLI tests..."
    reset_all_test_state

    rm -f "${GATEWAY_CLI_CONFIG_DIR:-config/cli-e2e}/config.json"

    enforce_mfa_and_validate_cli_blocking
    provision_mfa

    run_admin_tests
    run_api_token_tests
    run_deployer_tests
    run_viewer_tests

    echo -e "${GREEN}CLI E2E completed successfully.${NC}"
}
