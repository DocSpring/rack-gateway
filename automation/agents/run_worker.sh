#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<USAGE
Usage: $0 <task-id> <worktree-path> <instructions-file>

Creates a prompt for the specified task, launches Claude Code in the background,
and registers the worker PID for monitoring.

Example:
  $0 001 /Users/.../001-proxy-dedupe automation/agents/task-001.md
USAGE
}

if [ $# -ne 3 ]; then
  usage >&2
  exit 1
fi

TASK_ID="$1"
WORKTREE="$2"
INSTRUCTIONS="$3"

ROOT_DIR=$(git rev-parse --show-toplevel)
PROMPT_TEMPLATE="$ROOT_DIR/automation/agents/worker_prompt_template.txt"
LOG_DIR="$ROOT_DIR/automation/agents/logs"
TEMP_DIR="$ROOT_DIR/automation/agents/tmp"
PROMPT_FILE="$TEMP_DIR/${TASK_ID}_prompt.txt"
LOG_FILE="$LOG_DIR/${TASK_ID}.log"

if [[ ! $TASK_ID =~ ^[A-Za-z0-9_-]+$ ]]; then
  echo "Error: task-id must be alphanumeric (underscores and dashes allowed)." >&2
  exit 1
fi

if [ ! -d "$WORKTREE" ]; then
  echo "Error: worktree not found: $WORKTREE" >&2
  exit 1
fi

if [ ! -f "$PROMPT_TEMPLATE" ]; then
  echo "Error: worker prompt template not found: $PROMPT_TEMPLATE" >&2
  exit 1
fi

if [ ! -f "$INSTRUCTIONS" ]; then
  echo "Error: instructions file not found: $INSTRUCTIONS" >&2
  exit 1
fi

mkdir -p "$LOG_DIR" "$TEMP_DIR"

# Build composite prompt
{
  cat "$PROMPT_TEMPLATE"
  printf '\n\nTASK CONTEXT (%s):\n' "$TASK_ID"
  cat "$INSTRUCTIONS"
} > "$PROMPT_FILE"

# Share the main repository's Postgres docker-compose project so workers reuse the
# primary database container instead of provisioning their own.
SHARED_PROJECT="${RGW_SHARED_DB_PROJECT:-$(basename "$ROOT_DIR")}"
export RGW_SHARED_DB_PROJECT="$SHARED_PROJECT"

# Launch Claude in the background within the worktree
cd "$WORKTREE"
nohup claude --print --dangerously-skip-permissions < "$PROMPT_FILE" > "$LOG_FILE" 2>&1 &
PID=$!

cd "$ROOT_DIR"

# Register the worker PID for monitoring
"$ROOT_DIR/automation/agents/register_worker.sh" "$TASK_ID" "$PID"

echo "Launched worker for task $TASK_ID (PID $PID). Logs: $LOG_FILE"
