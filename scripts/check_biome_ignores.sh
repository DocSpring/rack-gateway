#!/usr/bin/env bash
set -euo pipefail

# Check for biome-ignore comments in web/ directory
# Zero tolerance policy - NO biome-ignore comments allowed
# Exception: Generated files in web/src/lib/generated/

# WARNING: THIS FILE MAY NOT BE EDITED WITHOUT PERMISSION FROM THE USER.
# You either update biome.json to disable the linting rule globally, or you fix the issue!
# lint/complexity/noExcessiveCognitiveComplexity MUST BE FIXED. NO EXCEPTIONS.

echo "Checking for biome-ignore comments..."

# Search for biome-ignore comments in TypeScript/JavaScript files
# Exclude generated files (they have legitimate biome-ignore comments)
ignores=$(grep -r "biome-ignore" web/src \
  --include="*.ts" --include="*.tsx" --include="*.js" --include="*.jsx" \
  --exclude-dir="generated" \
  || true)

if [ -n "$ignores" ]; then
  allowed_patterns=(
    "lint/suspicious/noConsole"
  )

  filtered=""
  while IFS= read -r line; do
    skip=false
    for pattern in "${allowed_patterns[@]}"; do
      if [[ "$line" == *"$pattern"* ]]; then
        skip=true
        break
      fi
    done
    if ! $skip; then
      filtered+="$line\n"
    fi
  done <<< "$ignores"

  ignores="$filtered"

  if [ -z "$ignores" ]; then
    echo "✅ No disallowed biome-ignore comments found (excluding allowed patterns)"
    exit 0
  fi

  echo "❌ Found biome-ignore comments (zero tolerance):"
  printf '%s' "$ignores"
  exit 1
fi

echo "✅ No biome-ignore comments found (excluding generated files)"
exit 0
