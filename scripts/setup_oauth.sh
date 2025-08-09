#!/bin/bash

# Script to help set up Google OAuth credentials for the Convox Gateway

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Convox Gateway OAuth Setup ===${NC}"
echo ""
echo "This script will help you set up Google OAuth for the Convox Gateway."
echo ""

echo -e "${GREEN}Step 1: Create a Google Cloud Project${NC}"
echo "1. Go to https://console.cloud.google.com/"
echo "2. Create a new project or select an existing one"
echo "3. Note your project ID"
echo ""
read -p "Press Enter when you have a Google Cloud project ready..."

echo ""
echo -e "${GREEN}Step 2: Enable OAuth 2.0${NC}"
echo "1. Go to APIs & Services > Credentials"
echo "2. Click '+ CREATE CREDENTIALS' > 'OAuth client ID'"
echo "3. If prompted, configure the OAuth consent screen:"
echo "   - User Type: Internal (for Google Workspace)"
echo "   - App name: Convox Gateway"
echo "   - User support email: Your email"
echo "   - Authorized domains: Your company domain (e.g., company.com)"
echo ""
read -p "Press Enter when consent screen is configured..."

echo ""
echo -e "${GREEN}Step 3: Create OAuth 2.0 Client ID${NC}"
echo "1. Application type: Web application"
echo "2. Name: Convox Gateway"
echo "3. Authorized JavaScript origins:"
echo "   - http://localhost:8080 (for development)"
echo "   - https://your-gateway-domain.com (for production)"
echo "4. Authorized redirect URIs:"
echo "   - http://localhost:8080/.gateway/login/callback (for development)"
echo "   - https://your-gateway-domain.com/.gateway/login/callback (for production)"
echo "5. Click 'CREATE'"
echo ""
read -p "Press Enter when OAuth client is created..."

echo ""
echo -e "${GREEN}Step 4: Configure Environment${NC}"
echo "Copy your OAuth credentials:"
echo ""

# Check if .env exists
if [ ! -f .env ]; then
    if [ -f .env.example ]; then
        cp .env.example .env
        echo -e "${YELLOW}Created .env from .env.example${NC}"
    else
        echo -e "${RED}No .env.example found!${NC}"
        exit 1
    fi
fi

# Read OAuth credentials
echo -e "${BLUE}Enter your OAuth credentials:${NC}"
read -p "Client ID: " client_id
read -p "Client Secret: " client_secret
read -p "Allowed domain (e.g., company.com): " domain

# Update .env file
if [[ "$OSTYPE" == "darwin"* ]]; then
    # macOS
    sed -i '' "s|GOOGLE_CLIENT_ID=.*|GOOGLE_CLIENT_ID=$client_id|" .env
    sed -i '' "s|GOOGLE_CLIENT_SECRET=.*|GOOGLE_CLIENT_SECRET=$client_secret|" .env
    sed -i '' "s|GOOGLE_ALLOWED_DOMAIN=.*|GOOGLE_ALLOWED_DOMAIN=$domain|" .env
else
    # Linux
    sed -i "s|GOOGLE_CLIENT_ID=.*|GOOGLE_CLIENT_ID=$client_id|" .env
    sed -i "s|GOOGLE_CLIENT_SECRET=.*|GOOGLE_CLIENT_SECRET=$client_secret|" .env
    sed -i "s|GOOGLE_ALLOWED_DOMAIN=.*|GOOGLE_ALLOWED_DOMAIN=$domain|" .env
fi

# Update config.yml domain if it exists
if [ -f config/config.yml ]; then
    if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' "s|^domain:.*|domain: $domain|" config/config.yml
    else
        sed -i "s|^domain:.*|domain: $domain|" config/config.yml
    fi
    echo -e "${GREEN}Updated domain in config/config.yml${NC}"
fi

echo ""
echo -e "${GREEN}✓ OAuth setup complete!${NC}"
echo ""
echo "Your credentials have been saved to .env"
echo ""
echo -e "${YELLOW}Next steps:${NC}"
echo "1. Edit config/config.yml to add users with @$domain emails"
echo "2. Run 'make dev' to start the development environment"
echo "3. Test login at http://localhost:8080/.gateway/login/start"
echo ""
echo -e "${BLUE}For production deployment:${NC}"
echo "- Set these environment variables on your server"
echo "- Update redirect URIs in Google Cloud Console"
echo "- Use HTTPS for production URLs"