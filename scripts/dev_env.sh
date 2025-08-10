#!/bin/bash

# Development environment setup script
# This script sets up environment variables for local development

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Setting up development environment...${NC}"

# Check if .env file exists
if [ ! -f .env ]; then
    if [ -f .env.example ]; then
        echo -e "${YELLOW}No .env file found. Creating from .env.example...${NC}"
        cp .env.example .env
        echo -e "${YELLOW}Please edit .env with your configuration values${NC}"
    else
        echo -e "${RED}No .env or .env.example file found!${NC}"
        exit 1
    fi
fi

# Load environment variables from .env
set -a
source .env
set +a

# Check if config.yml exists
if [ ! -f config/config.yml ]; then
    if [ -f config/config.yml.example ]; then
        echo -e "${YELLOW}No config.yml found. Creating from example...${NC}"
        cp config/config.yml.example config/config.yml
        echo -e "${YELLOW}Please edit config/config.yml with your users and domain${NC}"
    fi
fi

# Set default values if not provided
export PORT=${PORT:-8080}
export DEV_MODE=${DEV_MODE:-true}
export MOCK_CONVOX_PORT=${MOCK_CONVOX_PORT:-5443}
export CONVOX_GATEWAY_DB_PATH=${CONVOX_GATEWAY_DB_PATH:-./data/db.sqlite}

# For development, use mock server if RACK_HOST is not set
if [ -z "$RACK_HOST" ]; then
    export RACK_HOST="http://localhost:${MOCK_CONVOX_PORT}"
    export RACK_TOKEN="mock-rack-token-12345"
    export RACK_USERNAME="convox"
    echo -e "${GREEN}Using mock Convox server at ${RACK_HOST}${NC}"
fi

# Generate JWT key if not set (for development only)
if [ -z "$APP_JWT_KEY" ] && [ "$DEV_MODE" = "true" ]; then
    export APP_JWT_KEY="dev-jwt-secret-$(date +%s)"
    echo -e "${GREEN}Generated development JWT key${NC}"
fi

echo -e "${GREEN}Development environment ready!${NC}"
echo -e "Gateway will run on port: ${PORT}"
echo -e "Database path: ${CONVOX_GATEWAY_DB_PATH}"

# Execute the command passed to the script
exec "$@"