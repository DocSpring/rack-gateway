#!/usr/bin/env bash
# Backward-compatible wrapper to the shared coverage script
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
exec "$ROOT_DIR/scripts/coverage_report.sh" "$@"
