#!/usr/bin/env bash
set -euo pipefail

echo "==> Convox Gateway Dev Setup"

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

install_task
install_pnpm

echo "==> Done"
echo "Run: task dev   # start the dev stack"
echo "     task test  # run all tests (web + go + e2e)"

