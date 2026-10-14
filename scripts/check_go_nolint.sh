#!/usr/bin/env bash
set -euo pipefail

# Go lint suppressions (see "Linting" in AGENTS.md):
# - The only //nolint allowed is //nolint:lll, for struct tag lines gofmt won't break.
# - A gosec exception names its rule and says why: "#nosec G402 -- reason". Never a bare #nosec.

echo "Checking Go lint suppressions..."

nolints=$(git grep -n -E '//[[:space:]]*nolint' -- '*.go' | grep -v -E '//nolint:lll$' || true)
nosecs=$(git grep -n -E '#nosec' -- '*.go' | grep -v -E '#nosec G[0-9]+ -- [^[:space:]]' || true)

if [ -n "$nolints" ] || [ -n "$nosecs" ]; then
  if [ -n "$nolints" ]; then
    echo "❌ //nolint is only allowed as //nolint:lll on struct tags. Fix the code instead:"
    echo "$nolints"
  fi
  if [ -n "$nosecs" ]; then
    echo "❌ A #nosec must name one gosec rule and give a reason (#nosec G402 -- reason):"
    echo "$nosecs"
  fi
  exit 1
fi

echo "✅ No disallowed Go lint suppressions"
