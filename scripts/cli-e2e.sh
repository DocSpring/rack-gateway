#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# Resolve ports from mise.toml or environment
MiseFile="mise.toml"
toml_get() {
  local key="$1" file="$2"
  if [[ -f "$file" ]]; then
    local line
    line="$(grep -E "^\s*${key}\s*=\s*" "$file" | head -n1)"
    if [[ -n "$line" ]]; then
      echo "$line" | sed -E 's/^[^=]+= *"?([^"#]+).*$/\1/' | xargs
      return 0
    fi
  fi
  return 1
}

GW_PORT="${GATEWAY_PORT:-}"
[[ -z "$GW_PORT" ]] && GW_PORT="$(toml_get GATEWAY_PORT "$MiseFile" || echo 8447)"

echo "Building CLI..."
make -s cli

echo "Logging in non-interactively via mock OAuth..."
./bin/convox-gateway login e2e "http://127.0.0.1:${GW_PORT}" --non-interactive --email admin@company.com

echo "Verifying CLI commands..."
./bin/convox-gateway rack
./bin/convox-gateway convox rack
./bin/convox-gateway convox apps || true

echo "CLI E2E completed successfully."

