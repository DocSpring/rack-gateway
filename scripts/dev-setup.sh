#!/usr/bin/env bash
set -euo pipefail

echo "==> Rack Gateway Dev Setup"

have() { command -v "$1" >/dev/null 2>&1; }

install_task() {
  if have task; then
    echo "- task already installed: $(task --version | head -n1)"
    return
  fi
  echo "- Installing task (go-task)..."
  if have brew; then
    brew install go-task/tap/go-task
  else
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT
    (cd "$tmpdir" && curl -sL https://taskfile.dev/install.sh | sh -s -- -b /usr/local/bin) || {
      echo "Could not install task to /usr/local/bin; try running with sudo or install manually: https://taskfile.dev" >&2
      exit 1
    }
  fi
  echo "- task installed: $(task --version | head -n1)"
}

install_pnpm() {
  echo "- Ensuring pnpm via corepack..."
  if have corepack; then
    corepack enable || true
    corepack prepare pnpm@latest --activate || true
  else
    echo "  corepack not found; installing pnpm globally via npm"
    if have npm; then
      npm i -g pnpm
    else
      echo "npm is not available. Install Node.js 20+ first." >&2
    fi
  fi
  echo "- pnpm version: $(pnpm -v || echo 'not found')"
}

install_air() {
  if have air; then
    echo "- air already installed: $(air -v 2>&1 | head -n1)"
    return
  fi
  echo "- Installing air (live reload for Go)..."
  go install github.com/air-verse/air@latest
  echo "- air installed: $(air -v 2>&1 | head -n1)"
}

install_goimports() {
  if have goimports; then
    echo "- goimports already installed"
    return
  fi
  echo "- Installing goimports (Go import formatter)..."
  go install golang.org/x/tools/cmd/goimports@latest
  echo "- goimports installed"
}

install_libfido2() {
  echo "- Checking for libfido2..."

  # Check if libfido2 is already installed
  if pkg-config --exists libfido2 2>/dev/null; then
    echo "  libfido2 already installed: $(pkg-config --modversion libfido2)"
    return
  fi

  echo "- Installing libfido2 (required for WebAuthn CLI support)..."

  if have brew; then
    # macOS
    brew install libfido2
  elif have apt-get; then
    # Ubuntu/Debian
    echo "  Installing via apt (requires sudo)..."
    sudo apt-get update
    sudo apt-get install -y software-properties-common
    sudo apt-add-repository -y ppa:yubico/stable
    sudo apt-get update
    sudo apt-get install -y libfido2-dev
  else
    echo "  Could not automatically install libfido2."
    echo "  Please install manually:"
    echo "    macOS: brew install libfido2"
    echo "    Linux: sudo apt-add-repository ppa:yubico/stable && sudo apt install libfido2-dev"
    return
  fi

  if pkg-config --exists libfido2 2>/dev/null; then
    echo "  libfido2 installed: $(pkg-config --modversion libfido2)"
  else
    echo "  libfido2 installation may have failed"
  fi
}

install_task
install_pnpm
install_air
install_goimports
install_libfido2

echo "==> Done"
echo "Run: task dev   # start the dev stack"
echo "     task test  # run all tests (web + go + e2e)"

