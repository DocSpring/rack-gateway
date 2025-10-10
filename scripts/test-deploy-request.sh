#!/bin/bash
set -e

# Check if deploy argument was provided
SHOULD_DEPLOY="${1:-}"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}Creating test API token...${NC}"
echo -e "${YELLOW}Note: You may be prompted for MFA authentication${NC}"
echo ""

# Create API token with cicd role - run interactively first, then extract token
# Valid roles: viewer, ops, deployer, cicd, admin
TEMP_FILE=$(mktemp)
trap "rm -f $TEMP_FILE" EXIT

./bin/rack-gateway api-token create \
  --name "test-deploy-$(date +%s)" \
  --role cicd \
  --output token > "$TEMP_FILE"

API_TOKEN=$(cat "$TEMP_FILE")

if [ -z "$API_TOKEN" ]; then
  echo "Failed to create API token"
  exit 1
fi

echo ""
echo -e "${GREEN}✓ Created API token: ${API_TOKEN:0:20}...${NC}"
echo ""

# Default values for deploy request
MESSAGE="${MESSAGE:-Test deploy request from script}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo "abc1234567890123456789012345678901234567")}"
BRANCH="${BRANCH:-$(git branch --show-current 2>/dev/null || echo "main")}"
CI_PROVIDER="${CI_PROVIDER:-github}"
PIPELINE_URL="${PIPELINE_URL:-https://github.com/example/repo/actions/runs/123456}"

echo -e "${BLUE}Creating deploy approval request...${NC}"
echo -e "  Message: ${YELLOW}$MESSAGE${NC}"
echo -e "  Branch: ${YELLOW}$BRANCH${NC}"
echo -e "  Commit: ${YELLOW}${GIT_COMMIT:0:7}${NC}"
echo -e "  CI Provider: ${YELLOW}$CI_PROVIDER${NC}"
echo -e "  Pipeline URL: ${YELLOW}$PIPELINE_URL${NC}"
echo ""

# Create deploy approval request using the new API token (app auto-detected from directory)
DEPLOY_OUTPUT=$(./bin/rack-gateway deploy-approval request \
  --api-token "$API_TOKEN" \
  --message "$MESSAGE" \
  --git-commit "$GIT_COMMIT" \
  --branch "$BRANCH" \
  --ci-provider "$CI_PROVIDER" \
  --pipeline-url "$PIPELINE_URL" 2>&1)

echo "$DEPLOY_OUTPUT"
echo ""

# Extract request public ID from output (format: "Deploy approval request <public_id> created (status: pending)")
REQUEST_ID=$(echo "$DEPLOY_OUTPUT" | grep -oE "Deploy approval request [a-f0-9-]+ created" | awk '{print $4}')

if [ -n "$REQUEST_ID" ]; then
  echo -e "${GREEN}✓ Successfully created deploy approval request ${REQUEST_ID}${NC}"
  echo ""

  if [ "$SHOULD_DEPLOY" = "deploy" ]; then
    echo -e "${BLUE}Approving the deploy request...${NC}"
    echo -e "${YELLOW}Note: You may be prompted for MFA authentication${NC}"
    echo ""

    ./bin/rack-gateway deploy-approval approve "$REQUEST_ID"

    echo ""
    echo -e "${GREEN}✓ Deploy request approved${NC}"
    echo ""
    echo -e "${BLUE}Running simulated deploy with the API token...${NC}"
    echo ""

    # Run deploy using the API token
    RACK_GATEWAY_API_TOKEN="$API_TOKEN" ./bin/rack-gateway deploy

    echo ""
    echo -e "${GREEN}✓ Deploy completed${NC}"
  else
    echo -e "${BLUE}Useful commands:${NC}"
    echo -e "  # Approve the request:"
    echo -e "  ${YELLOW}./bin/rack-gateway deploy-approval approve $REQUEST_ID${NC}"
    echo ""
    echo -e "  # Wait for decision using the API token:"
    echo -e "  ${YELLOW}./bin/rack-gateway deploy-approval request --api-token $API_TOKEN --app $APP --git-commit $GIT_COMMIT --message \"$MESSAGE\" --wait${NC}"
    echo ""
    echo -e "  # Run this script with deploy to simulate full flow:"
    echo -e "  ${YELLOW}./scripts/create-test-deploy-request.sh deploy${NC}"
  fi
else
  echo -e "${YELLOW}⚠ Could not extract request ID from output${NC}"
fi
