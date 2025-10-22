#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<USAGE
Usage: $0 <task-id> "short description"

Creates a new git worktree for an agent branch rooted at origin/main.

Environment variables:
  WORKTREE_ROOT  Override default worktree directory (default: /Users/ndbroadbent/code/rack-gateway-worktrees)
USAGE
}

if [ "$#" -lt 2 ]; then
  usage >&2
  exit 1
fi

TASK_ID="$1"
shift
DESCRIPTION="$*"

if [[ ! $TASK_ID =~ ^[A-Za-z0-9_-]+$ ]]; then
  echo "Error: task-id must be alphanumeric (underscores and dashes allowed)." >&2
  exit 1
fi

slugify() {
  local input="$1"
  echo "$input" \
    | tr '[:upper:]' '[:lower:]' \
    | sed -E 's/[^a-z0-9]+/-/g' \
    | sed -E 's/^-+|-+$//g'
}

SLUG=$(slugify "$DESCRIPTION")
if [ -z "$SLUG" ]; then
  SLUG="task"
fi

BRANCH="agents/${TASK_ID}-${SLUG}"
WORKTREE_ROOT=${WORKTREE_ROOT:-/Users/ndbroadbent/code/rack-gateway-worktrees}
WORKTREE_PATH="${WORKTREE_ROOT}/${TASK_ID}-${SLUG}"

ROOT_DIR=$(git rev-parse --show-toplevel)

if [ ! -d "$WORKTREE_ROOT" ]; then
  mkdir -p "$WORKTREE_ROOT"
fi

if [ -d "$WORKTREE_PATH" ]; then
  echo "Error: worktree path already exists: $WORKTREE_PATH" >&2
  exit 1
fi

# Ensure we are up-to-date before branching.
(
  cd "$ROOT_DIR"
  git fetch origin main
)

# Create the worktree from origin/main, forcing the branch to point there.
git worktree add -B "$BRANCH" "$WORKTREE_PATH" main

echo "Created worktree: $WORKTREE_PATH"
echo "Branch: $BRANCH"
echo "Next steps:"
echo "  cd $WORKTREE_PATH"
echo "  claude code --prompt-file automation/agents/worker_prompt_template.txt"
echo "  (include task $TASK_ID context when launching the agent)"
