#!/bin/bash

set -e

echo "🚀 Starting Convox Gateway Integration Test"
echo "============================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Cleanup function
cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    if [ ! -z "$PROXY_PID" ]; then
        kill $PROXY_PID 2>/dev/null || true
    fi
}

trap cleanup EXIT

# Source dev environment
echo -e "\n${GREEN}1. Setting up environment...${NC}"
source ./scripts/dev_env.sh

# Build binaries
echo -e "\n${GREEN}2. Building binaries...${NC}"
go build -o bin/convox-gateway-api cmd/api/main.go
go build -o bin/convox-gateway cmd/cli/main.go

# Start gateway API server
echo -e "\n${GREEN}3. Starting gateway API server...${NC}"
./bin/convox-gateway-api &
PROXY_PID=$!
sleep 3

# Test health endpoint
echo -e "\n${GREEN}4. Testing health endpoint...${NC}"
HEALTH_RESPONSE=$(curl -s http://localhost:8080/health)
echo "Health check response: $HEALTH_RESPONSE"

if [ "$HEALTH_RESPONSE" != '{"status":"healthy"}' ]; then
    echo -e "${RED}Health check failed!${NC}"
    exit 1
fi

# Test login start endpoint
echo -e "\n${GREEN}5. Testing OAuth login start...${NC}"
LOGIN_START=$(curl -s -X POST http://localhost:8080/v1/login/start)
echo "Login start response received"

if echo "$LOGIN_START" | grep -q "auth_url"; then
    echo -e "${GREEN}✓ OAuth login flow initiated successfully${NC}"
else
    echo -e "${RED}OAuth login start failed!${NC}"
    echo "$LOGIN_START"
    exit 1
fi

# Test unauthorized access
echo -e "\n${GREEN}6. Testing unauthorized access...${NC}"
UNAUTH_RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/v1/me)

if [ "$UNAUTH_RESPONSE" = "401" ]; then
    echo -e "${GREEN}✓ Unauthorized access correctly blocked (401)${NC}"
else
    echo -e "${RED}Expected 401, got $UNAUTH_RESPONSE${NC}"
    exit 1
fi

# Test admin endpoints without auth
echo -e "\n${GREEN}7. Testing admin endpoints protection...${NC}"
ADMIN_RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/v1/admin/users)

if [ "$ADMIN_RESPONSE" = "401" ]; then
    echo -e "${GREEN}✓ Admin endpoints correctly protected (401)${NC}"
else
    echo -e "${RED}Admin endpoint not protected! Got $ADMIN_RESPONSE${NC}"
    exit 1
fi

# Test static UI endpoint
echo -e "\n${GREEN}8. Testing UI endpoint...${NC}"
UI_RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/)

if [ "$UI_RESPONSE" = "200" ]; then
    echo -e "${GREEN}✓ UI endpoint accessible (200)${NC}"
else
    echo -e "${YELLOW}UI endpoint returned $UI_RESPONSE${NC}"
fi

echo -e "\n${GREEN}============================================="
echo -e "✅ All integration tests passed!${NC}"
echo -e "${GREEN}=============================================\n${NC}"

echo -e "${YELLOW}Note: Full OAuth flow and proxy forwarding require:${NC}"
echo "  - Valid Google OAuth credentials"
echo "  - Running Convox rack"
echo "  - Proper rack tokens"
echo ""
echo "To complete testing:"
echo "  1. Set up Google OAuth app"
echo "  2. Configure rack tokens"
echo "  3. Use CLI: ./bin/convox-gateway login <rack>"
echo "  4. Test gateway: ./bin/convox-gateway call <rack> GET /apps"