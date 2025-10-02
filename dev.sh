#!/bin/bash
# Development script - starts backend in Docker, frontend locally
# Usage: ./dev.sh

set -e

echo "🚀 Starting Rack Gateway development environment..."
echo ""
echo "This will start:"
echo "- Backend services in Docker (Gateway API, Mock OAuth, Mock Convox)"
echo "- Frontend locally with hot reload"
echo ""

# Check if foreman is installed
if ! command -v foreman &> /dev/null; then
    echo "❌ foreman not found. Installing..."
    gem install foreman
fi

# Run with foreman
foreman start -f Procfile.dev
