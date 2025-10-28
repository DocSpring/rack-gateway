#!/usr/bin/env bash
set -euo pipefail

# Heavy end-to-end using Convox Development Rack (Ubuntu-compatible)
# Prereqs: convox, docker, minikube >=1.29, terraform

RACK_NAME=${RACK_NAME:-local}
APP_NAME=${APP_NAME:-rack-gateway}
TIMEOUT_SECS=${TIMEOUT_SECS:-600}

need() { command -v "$1" >/dev/null 2>&1 || { echo "Missing required command: $1"; exit 1; }; }

echo "Checking prerequisites..."
need convox; need docker; need minikube; need terraform

if [ "${E2E_DEV_RACK:-0}" != "1" ]; then
  echo "E2E_DEV_RACK is not set to 1. Skipping heavy operations."
  echo "To run: E2E_DEV_RACK=1 $0"
  exit 0
fi

echo "Starting minikube (if not running)..."
minikube status >/dev/null 2>&1 || minikube start

echo "Installing/ensuring local rack: $RACK_NAME"
if ! convox racks | grep -q "^$RACK_NAME\b"; then
  convox rack install local --name "$RACK_NAME" --wait --yes
fi

echo "Switching to rack: $RACK_NAME"
convox switch "$RACK_NAME"
convox rack || true

echo "Deploying app: $APP_NAME"
convox apps | grep -q "^$APP_NAME\b" || convox apps create "$APP_NAME"

# Pass common env; adjust as needed
convox env set \
  APP_SECRET_KEY="${APP_SECRET_KEY:-dev-e2e-key}" \
  GOOGLE_CLIENT_ID="${GOOGLE_CLIENT_ID:-mock-client-id}" \
  GOOGLE_CLIENT_SECRET="${GOOGLE_CLIENT_SECRET:-mock-client-secret}" \
  -a "$APP_NAME"

convox deploy -a "$APP_NAME"

echo "Waiting for app to be running..."
SECS=0
until convox apps info -a "$APP_NAME" | grep -q "Status *running"; do
  sleep 5; SECS=$((SECS+5));
  if [ "$SECS" -ge "$TIMEOUT_SECS" ]; then
    echo "Timed out waiting for app to be running"; exit 1;
  fi
done

echo "Running basic smoke commands via Convox CLI..."
convox rack
convox system
convox ps -a "$APP_NAME"
convox apps -a "$APP_NAME" || true

echo "E2E dev rack test completed."
