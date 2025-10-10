#!/bin/bash
set -e

# Check if deploy argument was provided
SHOULD_DEPLOY="${1:-}"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

API_TOKEN_FILE=".test-deploy-request-api-token"
if [ -f "$API_TOKEN_FILE" ]; then
  echo -e "${GREEN}✓ Using existing API token from $API_TOKEN_FILE: ${API_TOKEN:0:20}...${NC}"
  API_TOKEN=$(cat "$API_TOKEN_FILE")
fi

if [ -z "$API_TOKEN" ]; then
  echo -e "${BLUE}Creating test API token...${NC}"
  echo -e "${YELLOW}Note: You will be prompted for MFA authentication${NC}"
  echo ""

  # Create API token with cicd role - run interactively first, then extract token
  # Valid roles: viewer, ops, deployer, cicd, admin

  ./bin/rack-gateway api-token create \
    --name "test-deploy-$(date +%s)" \
    --role cicd \
    --output token > "$API_TOKEN_FILE"

  API_TOKEN=$(cat "$API_TOKEN_FILE")

  echo ""
  echo -e "${GREEN}✓ Created API token: ${API_TOKEN:0:20}...${NC}"
  echo ""
fi

if [ -z "$API_TOKEN" ]; then
  echo "API token missing!"
  exit 1
fi

# Default values for deploy request
MESSAGE="${MESSAGE:-Test deploy request from script}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo "abc1234567890123456789012345678901234567")}"
BRANCH="${BRANCH:-$(git branch --show-current 2>/dev/null || echo "main")}"
CI_PROVIDER="${CI_PROVIDER:-github}"
PIPELINE_URL="${PIPELINE_URL:-https://github.com/example/repo/actions/runs/123456}"

# Clean up any existing deploy requests for this commit
echo -e "${BLUE}Cleaning up existing deploy requests for commit ${GIT_COMMIT:0:7}...${NC}"
docker exec -i rack-gateway-postgres-1 psql -U postgres -d gateway_dev -c "DELETE FROM deploy_approval_requests WHERE git_commit_hash = '$GIT_COMMIT';" >/dev/null 2>&1 || true
echo ""

echo -e "${BLUE}Creating deploy approval request...${NC}"
echo -e "  Message: ${YELLOW}$MESSAGE${NC}"
echo -e "  Branch: ${YELLOW}$BRANCH${NC}"
echo -e "  Commit: ${YELLOW}${GIT_COMMIT:0:7}${NC}"
echo -e "  CI Provider: ${YELLOW}$CI_PROVIDER${NC}"
echo -e "  Pipeline URL: ${YELLOW}$PIPELINE_URL${NC}"
echo ""

# Create deploy approval request using the new API token (app auto-detected from directory)
set +e
DEPLOY_OUTPUT=$(./bin/rack-gateway deploy-approval request \
  --api-token "$API_TOKEN" \
  --message "$MESSAGE" \
  --git-commit "$GIT_COMMIT" \
  --branch "$BRANCH" \
  --ci-provider "$CI_PROVIDER" \
  --pipeline-url "$PIPELINE_URL" 2>&1)

REQUEST_STATUS=$?
set -e
echo "$DEPLOY_OUTPUT"

if [ $REQUEST_STATUS -ne 0 ]; then
  echo -e "${RED}Failed to create deploy approval request${NC}"
  exit 1
fi
echo ""

# Extract request public ID from output (format: "Deploy approval request <public_id> created (status: pending)")
REQUEST_ID=$(echo "$DEPLOY_OUTPUT" | grep -oE "Deploy approval request [a-f0-9-]+ created" | awk '{print $4}')

if [ -z "$REQUEST_ID" ]; then
  echo -e "${YELLOW}⚠ Could not extract request ID from output${NC}"
  exit 1
fi

echo -e "${GREEN}✓ Successfully created deploy approval request ${REQUEST_ID}${NC}"
echo ""

if [ "$SHOULD_DEPLOY" != "deploy" ]; then
  echo -e "${BLUE}Useful commands:${NC}"
  echo -e "  # Approve the request:"
  echo -e "  ${YELLOW}./bin/rack-gateway deploy-approval approve $REQUEST_ID${NC}"
  echo ""
  echo -e "  # Wait for decision using the API token:"
  echo -e "  ${YELLOW}./bin/rack-gateway deploy-approval request --api-token $API_TOKEN --app $APP --git-commit $GIT_COMMIT --message \"$MESSAGE\" --wait${NC}"
  echo ""
  echo -e "  # Run this script with deploy to simulate full flow:"
  echo -e "  ${YELLOW}./scripts/create-test-deploy-request.sh deploy${NC}"\
  exit 0
fi

echo -e "${BLUE}Approving the deploy request...${NC}"
echo -e "${YELLOW}Note: You will be prompted for MFA authentication${NC}"
echo ""

./bin/rack-gateway deploy-approval approve "$REQUEST_ID"

echo ""
echo -e "${GREEN}✓ Deploy request approved${NC}"
echo ""
echo -e "${BLUE}Running simulated deploy with the API token...${NC}"
echo ""

# Run deploy using the API token
export RACK_GATEWAY_API_TOKEN="$API_TOKEN"

echo "Building..."
set +e
build_output=$(./bin/rack-gateway build --description "test-deploy-request build")
build_status=$?
set -e
echo "Output: $build_output" >&2

if [[ $build_status -ne 0 ]]; then
  echo -e "${RED}Build failed${NC}" >&2
  exit 1
fi
echo -e "${GREEN}✓ Build succeeded${NC}"
# Extract release ID from "Release: RXXX" line
RELEASE_ID=$(echo "$build_output" | grep "^Release:" | awk '{print $2}')

echo "Release ID: $RELEASE_ID"
./bin/rack-gateway run gateway "rails db:migrate" --release "$RELEASE_ID"
./bin/rack-gateway releases promote "$RELEASE_ID"

unset RACK_GATEWAY_API_TOKEN

echo ""
echo -e "${GREEN}✓ Deploy completed${NC}"
