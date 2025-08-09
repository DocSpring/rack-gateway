#!/bin/bash

export PORT=8080
export DEV_MODE=true
export GOOGLE_CLIENT_ID=${GOOGLE_CLIENT_ID:-"dev-client-id"}
export GOOGLE_CLIENT_SECRET=${GOOGLE_CLIENT_SECRET:-"dev-client-secret"}
export GOOGLE_ALLOWED_DOMAIN=${GOOGLE_ALLOWED_DOMAIN:-"docspring.com"}
export REDIRECT_URL=${REDIRECT_URL:-"http://localhost:8080/v1/login/callback"}
export ADMIN_USERS=${ADMIN_USERS:-"admin@docspring.com"}
export RACKS_CONFIG_PATH=${RACKS_CONFIG_PATH:-"config/racks.yaml"}
export USERS_CONFIG_PATH=${USERS_CONFIG_PATH:-"config/users.yaml"}
export ROLES_CONFIG_PATH=${ROLES_CONFIG_PATH:-"config/roles.yaml"}
export POLICIES_PATH=${POLICIES_PATH:-"config/policies.yaml"}

export RACK_TOKEN_STAGING=${RACK_TOKEN_STAGING:-"dev-token-staging"}
export RACK_TOKEN_US=${RACK_TOKEN_US:-"dev-token-us"}
export RACK_TOKEN_EU=${RACK_TOKEN_EU:-"dev-token-eu"}

echo "Development environment configured:"
echo "  PORT: $PORT"
echo "  DEV_MODE: $DEV_MODE"
echo "  GOOGLE_ALLOWED_DOMAIN: $GOOGLE_ALLOWED_DOMAIN"
echo "  ADMIN_USERS: $ADMIN_USERS"
echo ""