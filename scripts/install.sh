#!/usr/bin/env bash
set -euo pipefail

# rack-gateway CLI installer
# - Builds ./cmd/rack-gateway and installs the binary as "rack-gateway"
# - Default install dir: /usr/local/bin (override with INSTALL_DIR or --dir)

usage() {
  cat <<'USAGE'
Usage: scripts/install.sh [--dir /custom/bin]

Options:
  --dir DIR   Install destination (default: /usr/local/bin)

Environment:
  INSTALL_DIR  Same as --dir (takes precedence if set)

This script builds the CLI from source and installs it as "rack-gateway".
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

# Check for libfido2 (required for WebAuthn/FIDO2 support)
if ! pkg-config --exists libfido2 2>/dev/null; then
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
  echo "⚠️  libfido2 not found - required for WebAuthn CLI authentication" >&2
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
  echo "" >&2
  echo "Run the setup task to install all development dependencies:" >&2
  echo "  task dev-setup" >&2
  echo "" >&2
  echo "Without libfido2, the build will fail with:" >&2
  echo "  fatal error: 'fido.h' file not found" >&2
  echo "" >&2
  echo "Alternative: Build without WebAuthn support using:" >&2
  echo "  task go:build:cli:nofido" >&2
  echo "" >&2
  exit 1
fi

echo "✓ libfido2 detected: $(pkg-config --modversion libfido2)"

echo "Installing rack-gateway CLI to: $INSTALL_DIR"

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
GOFLAGS="${GOFLAGS:-}"
# Note: CGO is required for WebAuthn/FIDO2 support (HID device access)
CGO_ENABLED=1 GOFLAGS="$GOFLAGS" go build -ldflags "$LDFLAGS" -o bin/rack-gateway ./cmd/rack-gateway

popd >/dev/null

# Ensure destination exists
mkdir -p "$INSTALL_DIR"

DEST="$INSTALL_DIR/rack-gateway"

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

copy_with_sudo "$REPO_ROOT/bin/rack-gateway" "$DEST"
chmod +x "$DEST" || true

echo "✅ Installed: $DEST"

# Offer shell completion hints
echo
echo "Add shell completions (optional):"
echo "  Bash:       source <(rack-gateway completion bash)"
echo "  Zsh:        source <(rack-gateway completion zsh)"
echo "  Fish:       rack-gateway completion fish | source"

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
CLI_DIR="${GATEWAY_CLI_CONFIG_DIR:-}"

[[ -z "$GATEWAY_PORT" ]]   && GATEWAY_PORT="$(toml_get GATEWAY_PORT "$MiseFile" || echo 8447)"
[[ -z "$CLI_DIR" ]]   && CLI_DIR="$(toml_get GATEWAY_CLI_CONFIG_DIR "$MiseFile" || echo "$HOME/.config/rack-gateway")"

echo
echo "Authenticate with the gateway:"
echo "  Production example:"
echo "    rack-gateway login staging https://gateway.example.com"
echo
echo "  Local dev example:"
echo "    rack-gateway login local http://localhost:${GATEWAY_PORT}"

echo
echo "After login (examples):"
echo "  rack-gateway rack                # Show current rack"
echo "  rack-gateway convox apps         # Run convox through the gateway"
echo "  rack-gateway switch <rack>       # Switch racks later"
echo
echo "Config location:"
echo "  ${CLI_DIR}   # override with GATEWAY_CLI_CONFIG_DIR"

echo
echo "Use CLI against dev gateway:"
echo "  rack-gateway login local http://localhost:${GATEWAY_PORT}"
echo "  rack-gateway convox rack"
echo "  rack-gateway convox apps"
