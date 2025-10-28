#!/bin/bash

# Development environment setup script
# This script sets up the development environment using mise for environment variables

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Setting up development environment...${NC}"

# Check if mise is installed
if ! command -v mise &> /dev/null; then
    echo -e "${RED}mise is not installed! Please install mise: https://mise.jdx.dev/getting-started.html${NC}"
    exit 1
fi

# Check if mise.local.toml exists
if [ ! -f mise.local.toml ]; then
    if [ -f mise.local.toml.example ]; then
        echo -e "${YELLOW}No mise.local.toml found. Creating from example...${NC}"
        cp mise.local.toml.example mise.local.toml
        echo -e "${YELLOW}Please edit mise.local.toml with your configuration values${NC}"
    else
        echo -e "${YELLOW}No mise.local.toml found. You can create one from mise.local.toml.example${NC}"
    fi
fi

# Load environment variables using mise
eval "$(mise env --shell bash)"

# Set default values if not provided
export PORT=${PORT:-8080}
export DEV_MODE=${DEV_MODE:-true}
export MOCK_CONVOX_PORT=${MOCK_CONVOX_PORT:-5443}

# For development, use mock server if RACK_HOST is not set
if [ -z "$RACK_HOST" ]; then
    export RACK_HOST="http://localhost:${MOCK_CONVOX_PORT}"
    export RACK_TOKEN="mock-rack-token-12345"
    export RACK_USERNAME="convox"
    echo -e "${GREEN}Using mock Convox server at ${RACK_HOST}${NC}"
fi

# Generate secret key if not set (for development only)
if [ -z "$APP_SECRET_KEY" ] && [ "$DEV_MODE" = "true" ]; then
    generated_key="dev-secret-$(date +%s)"
    export APP_SECRET_KEY="$generated_key"
    echo -e "${GREEN}Generated development secret key${NC}"
fi

echo -e "${GREEN}Development environment ready!${NC}"
echo -e "Gateway will run on port: ${PORT}"

# Execute the command passed to the script
exec "$@"
