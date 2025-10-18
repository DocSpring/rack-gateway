#!/bin/bash
# Check for biome-ignore comments - zero tolerance policy

set -e

exitcode=0

# Search for biome-ignore in TypeScript/TSX files
if grep -r "biome-ignore" web/src --include="*.ts" --include="*.tsx" 2>/dev/null; then
  echo "❌ Found biome-ignore comments - these are not allowed"
  echo "   Fix the code instead of suppressing the linter"
  exitcode=1
fi

if [ $exitcode -eq 0 ]; then
  echo "✅ No biome-ignore comments found"
fi

exit $exitcode
