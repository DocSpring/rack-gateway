#!/bin/bash

# Health check script for development environment

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

URL=${URL:-http://localhost:8080}
MOCK_URL=${MOCK_URL:-http://localhost:5443}

echo "Checking services health..."
echo ""

# Check mock Convox server
echo -n "Mock Convox server ($MOCK_URL): "
if curl -s -f -o /dev/null "$MOCK_URL/health"; then
    echo -e "${GREEN}✓ Healthy${NC}"
else
    echo -e "${RED}✗ Not responding${NC}"
    exit 1
fi

# Check gateway API
echo -n "Gateway API ($URL): "
if curl -s -f -o /dev/null "$URL/.gateway/api/health"; then
    echo -e "${GREEN}✓ Healthy${NC}"
else
    echo -e "${RED}✗ Not responding${NC}"
    exit 1
fi

# Check gateway can reach mock server
echo -n "Gateway → Mock connection: "
if curl -s -f -o /dev/null -u "convox:mock-rack-token-12345" "$URL/system"; then
    echo -e "${GREEN}✓ Connected${NC}"
else
    echo -e "${YELLOW}⚠ Not connected (authentication may be required)${NC}"
fi

echo ""
echo -e "${GREEN}All services are healthy!${NC}"
echo ""
echo "You can now:"
echo "- Test the CLI: ./bin/convox-gateway login local $URL"
echo "- View logs: task dev:logs"
echo "- Stop services: task dev:down"
