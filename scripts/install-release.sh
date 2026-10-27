#!/bin/sh
# Installs the rack-gateway CLI from a GitHub release:
#
#   curl -fsSL https://github.com/DocSpring/rack-gateway/releases/latest/download/install.sh | sh
#
# Releases have builds for macOS on Apple Silicon and Linux (x86_64 and arm64). The script checks the archive's
# SHA-256 checksum, and its build provenance attestation with `gh attestation verify` when the GitHub CLI is
# installed, before it installs anything.
#
# Environment:
#   RACK_GATEWAY_VERSION      The release to install, such as v0.1.29 (default: the latest release)
#   RACK_GATEWAY_INSTALL_DIR  Where to install (default: /usr/local/bin when you can write to it, or ~/.local/bin)
#   RACK_GATEWAY_REPOSITORY   The GitHub repository to install from (default: DocSpring/rack-gateway)
set -eu

REPOSITORY="${RACK_GATEWAY_REPOSITORY:-DocSpring/rack-gateway}"
SOURCE_DOCS="https://github.com/${REPOSITORY}#readme"

fail() {
  echo "Error: $*" >&2
  exit 1
}

platform() {
  os="$(uname -s)"
  arch="$(uname -m)"
  case "$os" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) fail "releases have no build for $os: build the CLI from source ($SOURCE_DOCS)" ;;
  esac
  case "$arch" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) fail "releases have no build for $arch: build the CLI from source ($SOURCE_DOCS)" ;;
  esac
  # A shell running under Rosetta reports x86_64 on an Apple Silicon Mac.
  if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
    arch=arm64
  fi
  if [ "$os" = darwin ] && [ "$arch" = amd64 ]; then
    fail "releases have no build for Intel Macs: build the CLI from source ($SOURCE_DOCS)"
  fi
  echo "$os-$arch"
}

download() {
  if command -v curl >/dev/null 2>&1; then
    curl --proto '=https' --tlsv1.2 -fsSL -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget --https-only -q -O "$2" "$1"
  else
    fail "curl or wget is needed to download the CLI"
  fi
}

verify_checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$1" && sha256sum -c "$2.sha256" >/dev/null) || fail "checksum mismatch for $2"
  else
    (cd "$1" && shasum -a 256 -c "$2.sha256" >/dev/null) || fail "checksum mismatch for $2"
  fi
}

verify_attestation() {
  if ! command -v gh >/dev/null 2>&1; then
    echo "Checked the SHA-256 checksum. Install the GitHub CLI (gh) to also check the build provenance attestation."
    return
  fi
  gh attestation verify "$1/$2" --bundle "$1/$2.sigstore.json" --repo "$REPOSITORY" \
    --signer-workflow "$REPOSITORY/.github/workflows/release.yml" >/dev/null ||
    fail "the build provenance attestation for $2 doesn't check out"
  echo "Checked the SHA-256 checksum and the build provenance attestation."
}

install_dir() {
  if [ -n "${RACK_GATEWAY_INSTALL_DIR:-}" ]; then
    echo "$RACK_GATEWAY_INSTALL_DIR"
  elif [ -w /usr/local/bin ]; then
    echo /usr/local/bin
  else
    echo "$HOME/.local/bin"
  fi
}

main() {
  asset="rack-gateway-$(platform).tar.gz"
  if [ -n "${RACK_GATEWAY_VERSION:-}" ]; then
    base="https://github.com/$REPOSITORY/releases/download/v${RACK_GATEWAY_VERSION#v}"
  else
    base="https://github.com/$REPOSITORY/releases/latest/download"
  fi

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  echo "Downloading $base/$asset"
  for file in "$asset" "$asset.sha256" "$asset.sigstore.json"; do
    download "$base/$file" "$tmp/$file"
  done
  verify_checksum "$tmp" "$asset"
  verify_attestation "$tmp" "$asset"
  tar -xzf "$tmp/$asset" -C "$tmp" rack-gateway

  dir="$(install_dir)"
  mkdir -p "$dir"
  [ -w "$dir" ] || fail "can't write to $dir: set RACK_GATEWAY_INSTALL_DIR, or pipe the script to sudo sh"
  # Rename over the old binary rather than writing into it, so a running rack-gateway keeps working.
  cp "$tmp/rack-gateway" "$dir/.rack-gateway.install.$$"
  chmod 0755 "$dir/.rack-gateway.install.$$"
  mv -f "$dir/.rack-gateway.install.$$" "$dir/rack-gateway"

  version="$("$dir/rack-gateway" build-info | sed -n 's/^version: //p')"
  echo "Installed rack-gateway $version to $dir/rack-gateway"
  case ":$PATH:" in
    *":$dir:"*) ;;
    *) echo "$dir isn't on your PATH: add it to run rack-gateway" ;;
  esac
}

main "$@"
