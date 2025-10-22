# Multi-Agent Duplication Remediation Workflow

This directory holds coordination assets for running multiple Claude Code agents in parallel while they tackle the duplication backlog.

The high-level workflow looks like this:

1. **Pick a task** from `automation/duplication_tasks.md` (or add a new entry if you discover an uncovered hotspot).
2. **Create an isolated worktree** for the task:
   ```bash
   scripts/new-agent-worktree.sh <task-id> "short description"
   ```
   This creates a new branch under `agents/<task-id>-<slug>` and a worktree in `/Users/ndbroadbent/code/rack-gateway-worktrees/<task-id>-<slug>`.
3. **Launch Claude Code** inside the worktree and feed it the relevant prompt template:
   ```bash
   claude code --prompt-file automation/agents/worker_prompt_template.txt
   ```
   Provide the specific task number and context when you start the agent.
4. **Register the worker PID** so the supervisor scripts can track completion:
   ```bash
   # in another terminal
   automation/agents/register_worker.sh 001
   # or provide the PID explicitly if multiple agents are running
   automation/agents/register_worker.sh 001 12345
   ```
4. The agent follows the project rules in `AGENTS.md`, fixes the duplication, runs the required `task` commands, and opens a PR-ready branch.
6. To monitor workers automatically, run `automation/agents/wait_for_workers.sh` from the main repo. It watches the registered PIDs and returns as soon as one task completes (or times out after 20 minutes).

### Shared Postgres Container

- `automation/agents/run_worker.sh` exports `RGW_SHARED_DB_PROJECT` so every worktree shares the primary `rack-gateway-postgres-1` container from the main checkout.
- This keeps the canonical database on port 55432 while still allowing each worktree to spin up its own gateway/mock services on unique port numbers.
- When you run worker commands manually (outside the helper script), set `RGW_SHARED_DB_PROJECT=rack-gateway` to avoid provisioning duplicate Postgres containers that collide on 55432.

7. Once the worker finishes, run a review cycle from the worktree before merging:
   - If the worker left changes unstaged, run CodeRabbit against the working tree. If they committed, review the commit directly. **Run CodeRabbit only once per branch.** If you need to make follow-up format-only fixes (e.g., gofmt), skip re-running CodeRabbit; rely on your own review instead. Formally:
     ```bash
     # unstaged diff
     coderabbit review --plain --type uncommitted

     # committed changes versus origin/main
     coderabbit review --plain --type committed --base-commit origin/main
     ```
     Address minor suggestions immediately; if coderabbit flags major rework, spin up another worker with a follow-up task entry.
   - Re-run the required `task` commands locally. Do not merge until the branch is fully green (at minimum: `task go:test`).
8. After merging, run the full pipeline (`task ci`) from the main checkout. Only when `task ci` is green should you mark the task finished, update `duplication_tasks.md`, and remove the worktree (`git worktree remove ...`). If CI fails, address the failure immediately before launching new workers. The wait script cleans up PID files automatically, but you can manually remove them if a worker is aborted.

There is also an orchestrator prompt template (`manager_prompt_template.txt`) that you can use with Claude Code when you want an AI to coordinate several workers at once. The orchestrator reads the TODO list, assigns worktree branches, and monitors progress.

Keep everything in source control:
- Update the TODO list as tasks are claimed/completed.
- Capture any cross-task constraints or shared utilities in commit messages or `automation/duplication_tasks.md`.
- Always run `task go:test` (and any other relevant tasks) before shipping a branch.

> **Tip:** If you need to reuse the same worktree for another task, remove it with `git worktree remove <path>` once the branch is merged.
