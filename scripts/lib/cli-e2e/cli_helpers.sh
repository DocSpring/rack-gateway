# shellcheck shell=bash

login_cli_as() {
    local user_email="$1"
    local rack_name="${2:-e2e}"
    local secret="${MFA_TOTP_SECRETS[$user_email]:-}"

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
            "http://127.0.0.1:${GATEWAY_PORT}/api/v1/auth/cli/mfa")
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

verify_command_status_and_output() {
    local command="$1"
    local expected_status="$2"
    shift 2
    local expected_output=("$@")
    local shell_cmd="${WRAPPER_CMD:-./bin/rack-gateway} $command"

    echo -e "${BLUE}Running: $shell_cmd...${NC}"
    set +e
    local output
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

verify_rgw_command() {
    verify_command_status_and_output "$1" "0" "${@:2}"
}

verify_rgw_command_failure() {
    verify_command_status_and_output "$1" "1" "${@:2}"
}

logout_cli() {
    echo -e "${YELLOW}Logging out...${NC}"
    verify_rgw_command "logout" "Logged out from e2e"
}
