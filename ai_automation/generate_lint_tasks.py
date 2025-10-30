#!/usr/bin/env python3
import json
import math
import os
import pathlib
import subprocess
from collections import defaultdict

ROOT = pathlib.Path(__file__).resolve().parents[1]
TASK_DIR = ROOT / "ai_automation" / "tasks"
COMPLETED_DIR = TASK_DIR / "completed"
TMP_JSON = ROOT / "tmp" / "golangci_full.json"

os.makedirs(TASK_DIR, exist_ok=True)
os.makedirs(COMPLETED_DIR, exist_ok=True)
os.makedirs(TMP_JSON.parent, exist_ok=True)

print("Running golangci-lint...")
subprocess.run([
    "golangci-lint",
    "run",
    "./...",
    f"--output.json.path={TMP_JSON}",
], check=False)

if not TMP_JSON.exists():
    raise SystemExit("golangci-lint did not produce JSON output")

print("Generating tasks...")
with TMP_JSON.open() as f:
    data = json.load(f)

file_counts: dict[str, int] = defaultdict(int)
for issue in data.get("Issues", []):
    filename = issue.get("Pos", {}).get("Filename")
    if filename:
        file_counts[filename] += 1

entries = sorted(
    ((count, filename) for filename, count in file_counts.items()),
    key=lambda x: (-x[0], x[1]),
)

# start fresh
for path in TASK_DIR.glob("task-*.md"):
    path.unlink()

MIN_ISSUES = 10
batch: list[str] = []
issue_accum = 0
index = 1
tasks_summary: list[tuple[str, list[str], int]] = []

def flush():
    global index, batch, issue_accum
    if not batch:
        return
    task_id = f"task-{index:02d}"
    task_path = TASK_DIR / f"{task_id}.md"
    files = list(batch)
    lines: list[str] = []
    lines.append(f"# Task {index:02d}\n")
    lines.append("\n## Files\n\n")
    lines.extend(f"- {path}\n" for path in files)
    lines.append("\n## Instructions\n\n")
    lines.append("Run golangci-lint on the files listed above and fix every reported issue.\n\n")
    lines.append("**DO NOT run tests or any other commands. Only golangci-lint is permitted.**\n")
    lines.append("If you add helper files, lint them as well before finishing.\n\n")
    lines.append("## Acceptance criteria\n\n")
    for item in files:
        lines.append(f"- `{item}` passes `golangci-lint run {item}`.\n")
    task_path.write_text("".join(lines))
    tasks_summary.append((task_id, files, issue_accum))
    index += 1
    batch = []
    issue_accum = 0

for count, filename in entries:
    if count <= 0:
        continue
    batch.append(filename)
    issue_accum += count
    if issue_accum >= MIN_ISSUES:
        flush()

flush()

tasks_created = len(list(TASK_DIR.glob("task-*.md")))
print(f"Generated {tasks_created} tasks")
for task_id, files, total in tasks_summary:
    file_count = len(files)
    file_preview = ", ".join(files)
    print(f"  {task_id}: {total} issues across {file_count} file(s) [{file_preview}]")
