#!/usr/bin/env bash
set -euo pipefail

# Fails on any high or critical advisory in the JavaScript packages: the web UI, the docs site and the mock OAuth
# server. bun audit reads bun.lock, so nothing needs installing first. CI runs it on every push, and the Security
# Scan workflow nightly, so a newly disclosed advisory shows up even when nobody pushes.

cd "$(dirname "$0")/.."

status=0
for dir in web docs mock-oauth; do
  echo "Auditing ${dir} dependencies..."
  if ! (cd "${dir}" && bun audit --audit-level=high); then
    status=1
  fi
done
exit "${status}"
