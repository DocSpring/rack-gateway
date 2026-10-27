#!/usr/bin/env bash
# Builds the CLI release archive for the platform it runs on: dist/rack-gateway-<os>-<arch>.tar.gz, holding a
# single `rack-gateway` binary, and its .sha256 checksum file. The Release workflow runs it on each platform's
# runner, so it can run the binary it built.
#
# macOS builds link libfido2 and OpenSSL statically from Homebrew (brew install libfido2 openssl@3), so they
# support security keys. Linux builds use the nofido tag: go-libfido2 links libfido2 dynamically there.
#
# Usage: scripts/build-cli-release.sh <version> <repository>
set -euo pipefail

VERSION="${1:?usage: build-cli-release.sh <version> <repository>}"
REPOSITORY="${2:?usage: build-cli-release.sh <version> <repository>}"

OS="$(go env GOOS)"
ARCH="$(go env GOARCH)"
NAME="rack-gateway-${OS}-${ARCH}"

case "$OS" in
  darwin)
    export CGO_ENABLED=1 CGO_CFLAGS=-Wno-deprecated-declarations
    TAGS=""
    SECURITY_KEYS="supported"
    ;;
  linux)
    export CGO_ENABLED=0
    TAGS="nofido"
    SECURITY_KEYS="not supported"
    ;;
  *)
    echo "Error: releases have no build for $OS" >&2
    exit 1
    ;;
esac

BUILD_TIME="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
LDFLAGS="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}"
LDFLAGS+=" -X github.com/DocSpring/rack-gateway/internal/cli/update.Repository=${REPOSITORY}"

rm -rf dist/build
mkdir -p dist/build
go build -tags "$TAGS" -buildvcs=false -trimpath -ldflags "$LDFLAGS" -o dist/build/rack-gateway ./cmd/rack-gateway/

# rack-gateway update runs build-info on the binary it downloads, so check it here too.
info="$(dist/build/rack-gateway build-info)"
echo "$info"
grep -qxF "version: ${VERSION}" <<<"$info"
grep -qxF "security keys: ${SECURITY_KEYS}" <<<"$info"

# The macOS binary must only link libraries every Mac has, not Homebrew's.
if [ "$OS" = "darwin" ]; then
  otool -L dist/build/rack-gateway
  if otool -L dist/build/rack-gateway | tail -n +2 | grep -vE '^[[:space:]]+(/usr/lib/|/System/Library/)'; then
    echo "Error: the binary links libraries outside macOS" >&2
    exit 1
  fi
fi

# COPYFILE_DISABLE keeps macOS tar from adding AppleDouble (._*) files.
COPYFILE_DISABLE=1 tar -C dist/build -czf "dist/${NAME}.tar.gz" rack-gateway
if command -v sha256sum >/dev/null; then
  (cd dist && sha256sum "${NAME}.tar.gz" > "${NAME}.tar.gz.sha256")
else
  (cd dist && shasum -a 256 "${NAME}.tar.gz" > "${NAME}.tar.gz.sha256")
fi
cat "dist/${NAME}.tar.gz.sha256"
