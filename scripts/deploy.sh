#!/bin/bash
set -e

# Deploy script for rack-gateway
# Usage: ./scripts/deploy.sh [rack-name]
#
# If rack-name is not provided, uses the current rack from rack-gateway config

# Determine which rack to deploy to
RACK_FLAG=""
if [ -n "$1" ]; then
  RACK_NAME="$1"
  RACK_FLAG="--rack $RACK_NAME"
  echo "Using rack from argument: $RACK_NAME"
else
  # Get current rack from rack-gateway
  RACK_OUTPUT=$(rack-gateway rack 2>&1)
  RACK_NAME=$(echo "$RACK_OUTPUT" | grep "^Current rack:" | awk '{print $3}')

  if [ -z "$RACK_NAME" ]; then
    echo "Error: No rack specified and no current rack configured"
    echo "Usage: $0 [rack-name]"
    echo "   or: rack-gateway login <rack-name> <url> first"
    exit 1
  fi

  echo "Deploying to $RACK_NAME rack (Ctrl+C to cancel)"
  echo ""
  sleep 1
fi

APP_NAME="rack-gateway"

echo "Deploying to rack: $RACK_NAME"
echo "App: $APP_NAME"
echo ""

# Step 1: Build the new release
echo "==> Building new release..."
BUILD_LOG="/tmp/rack-gateway-build-$$.log"
# shellcheck disable=SC2086
rack-gateway build $RACK_FLAG --app "$APP_NAME" | tee "$BUILD_LOG"

if [ "${PIPESTATUS[0]}" -ne 0 ]; then
  echo ""
  echo "Error: Build failed"
  rm -f "$BUILD_LOG"
  exit 1
fi

# Extract release ID from build output
RELEASE_ID=$(
  sed -n 's/^Release:[[:space:]]*//p' "$BUILD_LOG" | tail -1 | tr -d '[:space:]'
)
if [ -z "$RELEASE_ID" ]; then
  RELEASE_ID=$(grep -oi 'release[[:space:]]\+[A-Z0-9]*' "$BUILD_LOG" | tail -1 | awk '{print $2}')
fi
rm -f "$BUILD_LOG"

if [ -z "$RELEASE_ID" ]; then
  echo ""
  echo "Error: Could not determine release ID from build output"
  exit 1
fi

echo ""
echo "Built release: $RELEASE_ID"
echo ""

# Step 2: Run migrations using the admin service
echo "==> Running database migrations..."
set +e
# shellcheck disable=SC2086
rack-gateway run admin ./rack-gateway-api migrate \
  $RACK_FLAG \
  --app "$APP_NAME" \
  --release "$RELEASE_ID"
MIGRATE_EXIT_CODE=$?
set -e

if [ $MIGRATE_EXIT_CODE -ne 0 ]; then
  echo ""
  echo "Error: Migrations failed with exit code $MIGRATE_EXIT_CODE"
  exit 1
fi

echo ""
echo "Migrations completed successfully"
echo ""

# Step 3: Promote the release
echo "==> Promoting release $RELEASE_ID..."
set +e
# shellcheck disable=SC2086
rack-gateway releases promote "$RELEASE_ID" $RACK_FLAG --app "$APP_NAME" --wait
PROMOTE_EXIT_CODE=$?
set -e

if [ $PROMOTE_EXIT_CODE -ne 0 ]; then
  echo ""
  echo "Error: Promotion failed with exit code $PROMOTE_EXIT_CODE"
  exit 1
fi

echo ""
echo "✓ Deployment complete!"
echo "  Rack: $RACK_NAME"
echo "  App: $APP_NAME"
echo "  Release: $RELEASE_ID"
