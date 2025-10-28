#!/bin/bash

# Simple script to test the mock OAuth flow

echo "🔐 Testing Mock Google OAuth Server"
echo "=================================="

OAUTH_URL="http://localhost:${MOCK_OAUTH_PORT:-3345}"

echo "✅ Testing health endpoint..."
curl -s "$OAUTH_URL"/health | jq .

echo -e "\n✅ Testing discovery endpoint..."
curl -s "$OAUTH_URL"/.well-known/openid_configuration | jq .

echo -e "\n✅ Testing server info..."
curl -s "$OAUTH_URL"/ | jq .

echo -e "\n🌐 Testing authorization flow..."
echo "Visit this URL to test the user selection flow:"
echo "$OAUTH_URL/oauth2/v2/auth?client_id=mock-client-id&redirect_uri=http://localhost:8447/callback&response_type=code&scope=openid%20email%20profile&state=test-state"

echo -e "\n🎉 Mock OAuth server is ready!"
echo "Use GOOGLE_OAUTH_BASE_URL=http://localhost:${MOCK_OAUTH_PORT:-3345} in your gateway configuration"
