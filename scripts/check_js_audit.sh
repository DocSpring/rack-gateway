#!/usr/bin/env bash
set -euo pipefail

# Fails on any high or critical advisory in the JavaScript packages: the web UI, the docs site and the mock OAuth
# server. bun audit reads bun.lock, so nothing needs installing first. CI runs it on every push, and the Security
# Scan workflow nightly, so a newly disclosed advisory shows up even when nobody pushes.

cd "$(dirname "$0")/.."

# braces <= 3.0.3 (GHSA-vfj7-8cjw-p6xm, deeply nested patterns exhaust the stack) has no patched release. Only
# Ultracite's CLI pulls it in, through fast-glob, and this repo only extends Ultracite's Biome preset: it never runs
# that CLI. The advisory is ignored only while every dependency of the package that leads to braces is ultracite.
braces_only_via_ultracite() {
  local name direct
  name="$(jq -r .name package.json)"
  # bun why prints each path up to the package itself; the line above the package's own line is its dependency
  direct="$(bun why braces 2>/dev/null \
    | grep -B1 -E "─ (dev |peer |optional )?${name} \(requires" \
    | grep -v -E "─ (dev |peer |optional )?${name} \(requires|^--$" || true)"
  [ -n "${direct}" ] && ! grep -v -q 'ultracite@' <<<"${direct}"
}

status=0
for dir in web docs mock-oauth; do
  echo "Auditing ${dir} dependencies..."
  ignore=()
  if (cd "${dir}" && braces_only_via_ultracite); then
    echo "Ignoring GHSA-vfj7-8cjw-p6xm: braces is only used by Ultracite's CLI (see scripts/check_js_audit.sh)"
    ignore+=(--ignore=GHSA-vfj7-8cjw-p6xm)
  fi
  if ! (cd "${dir}" && bun audit --audit-level=high ${ignore[@]+"${ignore[@]}"}); then
    status=1
  fi
done
exit "${status}"
