#!/usr/bin/env bash
set -euo pipefail

# Check for biome-ignore comments in web/ directory
# Zero tolerance policy - NO biome-ignore comments allowed
# Exception: Generated files in web/src/lib/generated/

echo "Checking for biome-ignore comments..."

# Search for biome-ignore comments in TypeScript/JavaScript files
# Exclude generated files (they have legitimate biome-ignore comments)
ignores=$(grep -r "biome-ignore" web/src web/e2e \
  --include="*.ts" --include="*.tsx" --include="*.js" --include="*.jsx" \
  --exclude-dir="generated" \
  || true)

if [ -n "$ignores" ]; then
  echo "❌ Found biome-ignore comments (zero tolerance):"
  echo "$ignores"
  exit 1
fi

echo "✅ No biome-ignore comments found (excluding generated files)"
exit 0
