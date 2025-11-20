# shellcheck shell=bash

run_admin_tests() {
    if [[ -n "$SKIP_ADMIN_TESTS" ]]; then
        return
    fi

    login_cli_as "admin@example.com" "e2e"

    verify_rgw_command "rack" "Current rack: e2e" "Logged in as admin@example.com"
    verify_rgw_command "rack info" "mock-rack" "mock-rack.example.com"
    verify_rgw_command "apps" "rack-gateway" "RAPI123456"
    verify_rgw_command "apps info" "Name        rack-gateway" "Status      running"
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

    verify_rgw_command "env" \
        "DATABASE_URL=********************" \
        "NODE_ENV=production" \
        "PORT=3000"

    verify_rgw_command "env get DATABASE_URL --unmask" \
        "postgres://user:pass@localhost/db"

    verify_rgw_command "env set FOO=bar" \
        "Setting FOO..." "Release:"

    verify_rgw_command "restart" \
        "Restarting web... OK" \
        "Restarting worker... OK"

    local testdata_dir="scripts/cli-e2e-testdata"

    verify_rgw_command_failure "deploy --manifest $testdata_dir/convox.with-build.yml" \
        "Error: manifest validation failed: service gateway must use a pre-built image"

    verify_rgw_command "deploy --app other-app" \
        "Packaging source..." "Uploading source..." "Starting build..." \
        "Building app..." \
        "Step 1/1: mock build step" \
        "Build complete" \
        "Promoting RNEW" \
        "OK"

    WRAPPER_CMD="timeout 3s ./bin/rack-gateway" verify_command_status_and_output "logs" \
        "124" \
        "Promoting release" \
        "Release promoted successfully."

    if [[ -n "$SKIP_API_TOKEN_TESTS" ]]; then
        logout_cli
    fi
}

run_api_token_tests() {
    if [[ -n "$SKIP_API_TOKEN_TESTS" ]]; then
        return
    fi

    # If admin tests were skipped, we need to log in now
    if [[ -n "$SKIP_ADMIN_TESTS" ]]; then
        login_cli_as "admin@example.com" "e2e"
    fi

    echo -e "${YELLOW}Creating CI/CD API token for pipeline simulation...${NC}"

    clear_mfa_replay_protection
    local mfa_code api_token_json API_TOKEN API_TOKEN_PUBLIC_ID
    mfa_code=$(generate_totp_code "$(get_totp_secret 'admin@example.com')")
    api_token_json=$(./bin/rack-gateway api-token create \
        --name "E2E CLI API Token ${E2E_TS}" \
        --role cicd \
        --mfa-code "$mfa_code" \
        --output json)

    API_TOKEN=$(jq -r '.token' <<<"$api_token_json")
    API_TOKEN_PUBLIC_ID=$(jq -r '.api_token.public_id' <<<"$api_token_json")

    if [[ -z "$API_TOKEN" || -z "$API_TOKEN_PUBLIC_ID" ]]; then
        echo -e "${RED}Failed to parse API token response${NC}" >&2
        echo -e "${RED}API Token JSON: $api_token_json${NC}"
        exit 1
    fi

    echo -e "${GREEN}API Token ID: $API_TOKEN_PUBLIC_ID${NC}, Token: $API_TOKEN${NC}"

    logout_cli

    export RACK_GATEWAY_API_TOKEN="$API_TOKEN"
    export RACK_GATEWAY_URL="http://127.0.0.1:${GATEWAY_PORT}"
    export RACK_GATEWAY_RACK="Test"

    echo -e "${YELLOW}Simulating CircleCI deploy workflow with API token permissions...${NC}"

    verify_rgw_command "rack" \
        "Current rack: Test" \
        "Gateway URL: http://127.0.0.1:9447"

    verify_rgw_command "rack info" "Name" "Status"

    verify_rgw_command "ps --app rack-gateway" "p-web-1"

    verify_rgw_command_failure \
        "run web --app rack-gateway 'delete everything'" \
        "Error: You don't have permission to run processes"

    verify_rgw_command_failure \
        "run web --app rack-gateway 'echo rake db:migrate'" \
        "Error: You don't have permission to run processes"

    echo -e "${YELLOW}API token requesting deploy approval for git commit...${NC}"
    local git_commit_hash git_branch ci_metadata REQUEST_ID
    git_commit_hash=$(git rev-parse HEAD)
    git_branch=$(git rev-parse --abbrev-ref HEAD)
    ci_metadata='{"workflow_id":"test-workflow-'${E2E_TS}'","pipeline_number":"'${E2E_TS}'"}'

    set +e
    local approval_output
    approval_output=$(./bin/rack-gateway deploy-approval request \
        --git-commit "$git_commit_hash" \
        --branch "$git_branch" \
        --ci-metadata "$ci_metadata" \
        --message "Pipeline deployment ${E2E_TS}" 2>&1)
    local approval_status=$?
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

    echo -e "${GREEN}Created approval request: $REQUEST_ID for commit $git_commit_hash${NC}"

    unset RACK_GATEWAY_API_TOKEN
    unset RACK_GATEWAY_URL
    unset RACK_GATEWAY_RACK

    login_cli_as "admin@example.com" "e2e"

    clear_mfa_replay_protection
    local approve_code
    approve_code=$(generate_totp_code "$(get_totp_secret 'admin@example.com')")
    verify_rgw_command \
        "deploy-approval approve $REQUEST_ID --notes 'Approved for E2E test' --mfa-code $approve_code" \
        "Deploy approval request" "approved"

    logout_cli

    export RACK_GATEWAY_API_TOKEN="$API_TOKEN"
    export RACK_GATEWAY_URL="http://127.0.0.1:${GATEWAY_PORT}"
    export RACK_GATEWAY_RACK="Test"

    local testdata_dir="scripts/cli-e2e-testdata"

    echo -e "${BLUE}Test 1/4: Invalid image tags (should fail)...${NC}"
    set +e
    local invalid_deploy_output
    invalid_deploy_output=$(./bin/rack-gateway deploy . --app rack-gateway --manifest "$testdata_dir/convox.invalid-image-tag.yml" --description "invalid manifest test" 2>&1)
    local invalid_deploy_status=$?
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

    echo -e "${BLUE}Test 2/5: Duplicate object upload (should fail)...${NC}"
    set +e
    local duplicate_upload_output
    duplicate_upload_output=$(./bin/rack-gateway deploy . --app rack-gateway --manifest "$testdata_dir/convox.no-image-tag.yml" --description "duplicate upload test" 2>&1)
    local duplicate_upload_status=$?
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

    echo -e "${BLUE}Asserting object_url was saved (required for duplicate detection)...${NC}"
    assert_deploy_approval_fields "$git_commit_hash" "NOT_EMPTY" "" ""

    echo -e "${YELLOW}Clearing object_url for next test...${NC}"
    psql_exec "UPDATE deploy_approval_requests SET object_url = NULL WHERE git_commit_hash = '$git_commit_hash';"

    echo -e "${BLUE}Test 3/5: Build-based manifest (should fail)...${NC}"
    set +e
    local no_image_deploy_output
    no_image_deploy_output=$(./bin/rack-gateway deploy . --app rack-gateway --manifest "$testdata_dir/convox.no-image-tag.yml" --description "no image test" 2>&1)
    local no_image_deploy_status=$?
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

    echo -e "${YELLOW}Clearing object_url for successful build...${NC}"
    psql_exec "UPDATE deploy_approval_requests SET object_url = NULL WHERE git_commit_hash = '$git_commit_hash';"

    echo -e "${BLUE}Test 4/6: First successful build with valid manifest (commit $git_commit_hash)...${NC}"
    cat > "$testdata_dir/convox.valid-image-tag.yml" <<EOF
services:
  gateway:
    image: docspringcom/rack-gateway:${git_commit_hash}-amd64
    port: 8080
EOF

    set +e
    local first_build_output
    first_build_output=$(./bin/rack-gateway build . --app rack-gateway --manifest "$testdata_dir/convox.valid-image-tag.yml" --description "first successful build for commit $git_commit_hash" 2>&1)
    local first_build_status=$?
    set -e
    echo "Output: $first_build_output" >&2
    if [[ $first_build_status -ne 0 ]]; then
        echo -e "${RED}First valid manifest build failed${NC}" >&2
        exit 1
    fi
    echo -e "${GREEN}✓ First valid manifest build succeeded${NC}"

    echo -e "${BLUE}Asserting object_url, build_id, and release_id were saved...${NC}"
    assert_deploy_approval_fields "$git_commit_hash" "NOT_EMPTY" "NOT_EMPTY" "NOT_EMPTY"

    echo -e "${BLUE}Test 5/6: Duplicate build creation (should fail)...${NC}"
    echo -e "${YELLOW}Clearing object_url to allow re-upload, keeping build_id...${NC}"
    psql_exec "UPDATE deploy_approval_requests SET object_url = NULL WHERE git_commit_hash = '$git_commit_hash';"

    set +e
    local duplicate_build_output
    duplicate_build_output=$(./bin/rack-gateway build . --app rack-gateway --manifest "$testdata_dir/convox.valid-image-tag.yml" --description "duplicate build test" 2>&1)
    local duplicate_build_status=$?
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

    echo -e "${BLUE}Asserting all fields are set (object uploaded before build failed)...${NC}"
    assert_deploy_approval_fields "$git_commit_hash" "NOT_EMPTY" "NOT_EMPTY" "NOT_EMPTY"

    echo -e "${BLUE}Test 6/6: Default convox.yml with build (should fail)...${NC}"
    echo -e "${YELLOW}Clearing all fields for manifest validation test...${NC}"
    psql_exec "UPDATE deploy_approval_requests SET object_url = NULL, build_id = NULL, release_id = NULL WHERE git_commit_hash = '$git_commit_hash';"

    set +e
    local root_deploy_output
    root_deploy_output=$(./bin/rack-gateway deploy --manifest "$testdata_dir/convox.with-build.yml" 2>&1)
    local root_deploy_status=$?
    set -e
    echo "Output: $root_deploy_output" >&2
    if [[ $root_deploy_status -eq 0 ]]; then
        echo -e "${RED}Expected deploy with build to fail, but it succeeded${NC}" >&2
        exit 1
    fi
    if ! echo "$root_deploy_output" | grep -q "must use a pre-built image"; then
        echo -e "${RED}Expected 'must use a pre-built image' error message${NC}" >&2
        exit 1
    fi
    echo -e "${GREEN}✓ Build-based manifest correctly rejected${NC}"

    echo -e "${YELLOW}Clearing all fields for final successful test...${NC}"
    psql_exec "UPDATE deploy_approval_requests SET object_url = NULL, build_id = NULL, release_id = NULL WHERE git_commit_hash = '$git_commit_hash';"

    set +e
    local final_build_output
    final_build_output=$(./bin/rack-gateway build . --app rack-gateway --manifest "$testdata_dir/convox.valid-image-tag.yml" --description "final cli-e2e build for commit $git_commit_hash" 2>&1)
    local final_build_status=$?
    set -e
    echo "Output: $final_build_output" >&2
    if [[ $final_build_status -ne 0 ]]; then
        echo -e "${RED}Final manifest build failed${NC}" >&2
        exit 1
    fi
    echo -e "${GREEN}✓ Final manifest build succeeded${NC}"

    local release_id
    release_id=$(echo "$final_build_output" | grep "^Release:" | awk '{print $2}')
    if [[ -z "$release_id" ]]; then
        echo -e "${RED}Failed to parse release id from build output${NC}" >&2
        exit 1
    fi
    if ! [[ "$release_id" =~ ^R[A-Z0-9-]+$ ]]; then
        echo -e "${RED}Release ID has unexpected format: '$release_id'${NC}" >&2
        exit 1
    fi
    echo -e "${GREEN}Build created release: $release_id${NC}"

    # Verify deployer can read release info for the approved release
    verify_rgw_command \
        "releases info $release_id --app rack-gateway" \
        "Id" \
        "$release_id" \
        "Build" "Created" "Description"


    verify_rgw_command_failure \
        "run web --app rack-gateway 'delete everything'" \
        "Error: You don't have permission to run processes"

    verify_rgw_command "run web --app rack-gateway --release $release_id 'echo rake db:migrate'" \
        'Connected to mock exec for app=rack-gateway pid=proc-123456' \
        '$ echo rake db:migrate'

    verify_rgw_command_failure \
        "releases promote ROTHER --app rack-gateway" \
        "Error: You don't have permission to promote releases"

    verify_rgw_command \
        "releases promote $release_id --app rack-gateway" \
        "OK"

    verify_rgw_command_failure "run web --release $release_id 'echo rake db:migrate'" \
        "Error: You don't have permission to run processes"

    verify_rgw_command_failure "run web 'echo rake db:migrate'" \
        "Error: You don't have permission to run processes"

    unset RACK_GATEWAY_API_TOKEN
    unset RACK_GATEWAY_URL
    unset RACK_GATEWAY_RACK

    login_cli_as "admin@example.com" "e2e"

    clear_mfa_replay_protection
    local delete_code
    delete_code=$(generate_totp_code "$(get_totp_secret 'admin@example.com')")
    verify_rgw_command "api-token delete $API_TOKEN_PUBLIC_ID --mfa-code $delete_code" "Deleted token $API_TOKEN_PUBLIC_ID"
    logout_cli
}

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
        "Error: failed to fetch env: You don't have permission to view environment variables"
    verify_rgw_command_failure "env get DATABASE_URL --unmask" \
        "Error: failed to fetch env: You don't have permission to view environment variables."

    clear_mfa_replay_protection
    local delete_code
    delete_code=$(generate_totp_code "$(get_totp_secret 'viewer@example.com')")
    verify_rgw_command_failure "env set NOTALLOWED=1" "Error: permission denied"
    verify_rgw_command_failure "apps delete rack-gateway --mfa-code $delete_code" "Error: permission denied"
}
