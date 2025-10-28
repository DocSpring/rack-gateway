#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<USAGE
Usage: $0 <task-id> [pid]

Registers the PID of a running Claude Code worker for later monitoring. If PID
is omitted, the script attempts to infer the most recent "claude code" process.

Examples:
  $0 001 12345
  $0 002
USAGE
}

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  usage >&2
  exit 1
fi

TASK_ID="$1"
PID="${2:-}"

if [[ ! $TASK_ID =~ ^[A-Za-z0-9_-]+$ ]]; then
  echo "Error: task-id must be alphanumeric (underscores and dashes allowed)." >&2
  exit 1
fi

if [ -z "$PID" ]; then
  PID=$(pgrep -n -f "claude code" || true)
  if [ -z "$PID" ]; then
    echo "Error: unable to infer Claude Code PID. Please supply it explicitly." >&2
    exit 1
  fi
fi

if ! kill -0 "$PID" 2>/dev/null; then
  echo "Error: PID $PID is not running." >&2
  exit 1
fi

PID_DIR="$(git rev-parse --show-toplevel)/ai_automation/agents/pids"
mkdir -p "$PID_DIR"
PID_FILE="$PID_DIR/${TASK_ID}.pid"

printf '%s\n' "$PID" > "$PID_FILE"

COMMAND=$(ps -o command= -p "$PID" 2>/dev/null || true)
if [ -n "$COMMAND" ]; then
  printf '%s\n' "$COMMAND" > "$PID_FILE.info"
fi

echo "Registered task $TASK_ID with PID $PID"
