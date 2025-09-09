#!/usr/bin/env bash
set -euo pipefail

# convox-gateway CLI installer
# - Builds ./cmd/cli and installs the binary as "convox-gateway"
# - Default install dir: /usr/local/bin (override with INSTALL_DIR or --dir)

usage() {
  cat <<'USAGE'
Usage: scripts/install.sh [--dir /custom/bin]

Options:
  --dir DIR   Install destination (default: /usr/local/bin)

Environment:
  INSTALL_DIR  Same as --dir (takes precedence if set)

This script builds the CLI from source and installs it as "convox-gateway".
Examples:
  scripts/install.sh                      # installs to /usr/local/bin
  INSTALL_DIR=$HOME/.local/bin scripts/install.sh
  scripts/install.sh --dir "$HOME/.local/bin"
USAGE
}

INSTALL_DIR_DEFAULT="/usr/local/bin"
INSTALL_DIR="${INSTALL_DIR:-}"  # env override optional

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --dir) shift; INSTALL_DIR="${1:-}" ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 2 ;;
  esac
  shift || true
done

if [[ -z "${INSTALL_DIR}" ]]; then
  INSTALL_DIR="$INSTALL_DIR_DEFAULT"
fi

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: Go is not installed or not on PATH." >&2
  echo "Install Go: https://golang.org/doc/install" >&2
  exit 1
fi

echo "Installing convox-gateway CLI to: $INSTALL_DIR"

# Resolve repo root relative to this script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

mkdir -p "$REPO_ROOT/bin"

pushd "$REPO_ROOT" >/dev/null

echo "Downloading Go modules..."
go mod download

# Embed version and build time into the binary
VERSION="$(git describe --tags --always --dirty=-modified 2>/dev/null || echo dev)"
BUILDTIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILDTIME}"

echo "Building CLI (version: $VERSION)..."
GOFLAGS=${GOFLAGS:-}
CGO_ENABLED=0 go build $GOFLAGS -ldflags "$LDFLAGS" -o bin/convox-gateway ./cmd/cli

popd >/dev/null

# Ensure destination exists
mkdir -p "$INSTALL_DIR"

DEST="$INSTALL_DIR/convox-gateway"

copy_with_sudo() {
  local src="$1" dest="$2"
  if cp "$src" "$dest" 2>/dev/null; then
    return 0
  fi
  echo "Elevated permissions required to write to $INSTALL_DIR" >&2
  if command -v sudo >/dev/null 2>&1; then
    sudo cp "$src" "$dest"
  else
    echo "ERROR: Cannot write to $INSTALL_DIR and 'sudo' is not available." >&2
    echo "       Re-run with INSTALL_DIR set to a writable directory (e.g., \$HOME/.local/bin)." >&2
    exit 1
  fi
}

copy_with_sudo "$REPO_ROOT/bin/convox-gateway" "$DEST"
chmod +x "$DEST" || true

echo "✅ Installed: $DEST"

# Offer shell completion hints
echo
echo "Add shell completions (optional):"
echo "  Bash:       source <(convox-gateway completion bash)"
echo "  Zsh:        source <(convox-gateway completion zsh)"
echo "  Fish:       convox-gateway completion fish | source"

# Detect ports and CLI config dir from mise.toml (single source of truth)
MiseFile="$REPO_ROOT/mise.toml"
toml_get() {
  local key="$1" file="$2"
  if [[ -f "$file" ]]; then
    local line
    line="$(grep -E "^\s*${key}\s*=\s*" "$file" | head -n1)"
    if [[ -n "$line" ]]; then
      # Strip key, equals, optional quote, and any trailing comments; trim spaces
      echo "$line" | sed -E 's/^[^=]+= *"?([^"#]+).*$/\1/' | xargs
      return 0
    fi
  fi
  return 1
}

GATEWAY_PORT="${GATEWAY_PORT:-}"
WEB_P="${WEB_PORT:-}"
OAUTH_P="${MOCK_OAUTH_PORT:-}"
RACK_P="${MOCK_CONVOX_PORT:-}"
CLI_DIR="${CONVOX_GATEWAY_CLI_CONFIG_DIR:-}"

[[ -z "$GATEWAY_PORT" ]]   && GATEWAY_PORT="$(toml_get GATEWAY_PORT "$MiseFile" || echo 8447)"
[[ -z "$CLI_DIR" ]]   && CLI_DIR="$(toml_get CONVOX_GATEWAY_CLI_CONFIG_DIR "$MiseFile" || echo "$HOME/.config/convox-gateway")"

echo
echo "Authenticate with the gateway:"
echo "  Production example:"
echo "    convox-gateway login staging https://gateway.company.com"
echo
echo "  Local dev example:"
echo "    convox-gateway login local http://localhost:${GATEWAY_PORT}"

echo
echo "After login (examples):"
echo "  convox-gateway rack                # Show current rack"
echo "  convox-gateway convox apps         # Run convox through the gateway"
echo "  convox-gateway switch <rack>       # Switch racks later"
echo
echo "Config location:"
echo "  ${CLI_DIR}   # override with CONVOX_GATEWAY_CLI_CONFIG_DIR"

echo
echo "Use CLI against dev gateway:"
echo "  convox-gateway login local http://localhost:${GATEWAY_PORT}"
echo "  convox-gateway convox rack"
echo "  convox-gateway convox apps"
