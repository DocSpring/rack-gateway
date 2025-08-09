#!/bin/bash

set -e

echo "Starting end-to-end test..."

source ./scripts/dev_env.sh

echo "Building proxy..."
go build -o bin/proxy cmd/proxy/main.go

echo "Building CLI..."
go build -o bin/docspring-convox-login cmd/docspring-convox-login/main.go

echo "Starting proxy server..."
./bin/proxy &
PROXY_PID=$!

sleep 2

echo "Testing health endpoint..."
curl -s http://localhost:8080/health | jq .

echo "Testing login start (should work without auth)..."
curl -s -X POST http://localhost:8080/v1/login/start | jq .

echo "Testing protected endpoint (should fail)..."
curl -s http://localhost:8080/v1/me || echo "Failed as expected"

echo "Stopping proxy server..."
kill $PROXY_PID

echo "End-to-end test completed!"