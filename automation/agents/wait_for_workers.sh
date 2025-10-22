#!/usr/bin/env bash
set -euo pipefail

TIMEOUT_MINUTES=20
POLL_INTERVAL=5
PID_DIR="$(git rev-parse --show-toplevel)/automation/agents/pids"

end_time=$((SECONDS + TIMEOUT_MINUTES * 60))

load_tasks() {
  task_ids=()
  pids=()
  if [ ! -d "$PID_DIR" ]; then
    return
  fi
  shopt -s nullglob
  for file in "$PID_DIR"/*.pid; do
    task=$(basename "$file" .pid)
    pid=$(tr -d '\n' < "$file")
    if [ -z "$pid" ]; then
      rm -f "$file"
      continue
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "Task $task: process $pid is no longer running; removing stale PID file." >&2
      rm -f "$file" "$file.info" 2>/dev/null || true
      continue
    fi
    task_ids+=("$task")
    pids+=("$pid")
  done
  shopt -u nullglob
}

load_tasks
if [ ${#pids[@]} -eq 0 ]; then
  echo "No registered Claude Code workers found in $PID_DIR." >&2
  exit 1
fi

while [ ${#pids[@]} -gt 0 ]; do
  for idx in "${!pids[@]}"; do
    pid=${pids[$idx]}
    task=${task_ids[$idx]}
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "Task $task has completed (PID $pid)."
      rm -f "$PID_DIR/$task.pid" "$PID_DIR/$task.pid.info" 2>/dev/null || true
      exit 0
    fi
  done

  if [ $SECONDS -ge $end_time ]; then
    echo "Timeout after ${TIMEOUT_MINUTES} minutes." >&2
    exit 124
  fi

  echo "Waiting for ${#pids[@]} Claude Code worker(s)..."
  for idx in "${!pids[@]}"; do
    printf '  • Task %s — PID %s\n' "${task_ids[$idx]}" "${pids[$idx]}"
  done
  remaining=$((end_time - SECONDS))
  echo "  Time remaining: $((remaining / 60))m $((remaining % 60))s"

  sleep "$POLL_INTERVAL"
  load_tasks
done

echo "All Claude Code processes finished." >&2
exit 0
