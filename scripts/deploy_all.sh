#!/bin/bash
set -e

# Deploy to all racks in sequence: staging -> eu -> us
# Usage: ./scripts/deploy_all.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RACKS=("staging" "eu" "us")

echo "=== Deploying rack-gateway to all racks ==="
echo "Order: ${RACKS[*]}"
echo ""

for rack in "${RACKS[@]}"; do
  echo "========================================"
  echo "Deploying to: $rack"
  echo "========================================"
  echo ""

  if ! "$SCRIPT_DIR/deploy.sh" "$rack"; then
    echo ""
    echo "Error: Deployment to $rack failed"
    exit 1
  fi

  echo ""
  echo "✓ $rack deployment complete"
  echo ""
done

echo "========================================"
echo "✓ All deployments complete!"
echo "  Racks: ${RACKS[*]}"
echo "========================================"
