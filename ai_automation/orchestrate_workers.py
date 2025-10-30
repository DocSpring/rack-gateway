#!/usr/bin/env python3
import os
import subprocess
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List

ROOT = Path(__file__).resolve().parents[1]
TASK_DIR = ROOT / "ai_automation" / "tasks"
COMPLETED_DIR = TASK_DIR / "completed"
PID_DIR = ROOT / "ai_automation" / "agents" / "pids"
LOG_DIR = ROOT / "ai_automation" / "logs"
ORCH_LOG = LOG_DIR / "orchestrator.log"
RUNNER = ROOT / "ai_automation" / "run_worker.sh"
MAX_WORKERS = int(os.environ.get("MAX_WORKERS", "7"))
REFRESH_INTERVAL = 1
start_times: Dict[str, float] = {}
active_tasks: Dict[str, Worker] = {}


def log_event(message: str) -> None:
    ORCH_LOG.parent.mkdir(parents=True, exist_ok=True)
    timestamp = time.strftime("%Y-%m-%d %H:%M:%S", time.localtime())
    with ORCH_LOG.open("a") as log_file:
        log_file.write(f"[{timestamp}] {message}\n")

@dataclass
class Worker:
    task_id: str
    pid: int
    start_time: float


def tidy_pid_files() -> Dict[str, Worker]:
    workers: Dict[str, Worker] = {}
    PID_DIR.mkdir(parents=True, exist_ok=True)
    for pid_file in PID_DIR.glob("*.pid"):
        task_id = pid_file.stem
        try:
            pid = int(pid_file.read_text().strip())
        except ValueError:
            pid_file.unlink(missing_ok=True)
            (pid_file.with_suffix(".pid.info")).unlink(missing_ok=True)
            continue
        if pid <= 0:
            pid_file.unlink(missing_ok=True)
            continue
        start_file = PID_DIR / f"{task_id}.start"
        if start_file.exists():
            try:
                start_times[task_id] = float(start_file.read_text().strip())
            except ValueError:
                start_times[task_id] = time.time()
        else:
            start_times.setdefault(task_id, time.time())
        if not check_pid(pid):
            pid_file.unlink(missing_ok=True)
            (pid_file.with_suffix(".pid.info")).unlink(missing_ok=True)
            start_file.unlink(missing_ok=True)
            mark_task_completed(task_id)
            log_event(f"Removed stale worker for {task_id}")
            continue
        worker = Worker(task_id=task_id, pid=pid, start_time=start_times[task_id])
        workers[task_id] = worker
        active_tasks[task_id] = worker
    return workers


def check_pid(pid: int) -> bool:
    try:
        os.kill(pid, 0)
    except OSError:
        return False
    return True


def mark_task_completed(task_id: str) -> None:
    task_file = TASK_DIR / f"{task_id}.md"
    if task_file.exists():
        COMPLETED_DIR.mkdir(exist_ok=True, parents=True)
        task_file.rename(COMPLETED_DIR / task_file.name)
    (PID_DIR / f"{task_id}.start").unlink(missing_ok=True)
    log_event(f"Marked {task_id} as completed")
    active_tasks.pop(task_id, None)


def pending_tasks() -> List[Path]:
    return sorted(TASK_DIR.glob("task-*.md"))


def start_worker(task_file: Path) -> Worker:
    task_id = task_file.stem
    LOG_DIR.mkdir(exist_ok=True)
    proc = subprocess.Popen(
        [str(RUNNER), task_id, str(ROOT), str(task_file)],
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    output, _ = proc.communicate()
    if output:
        log_event(output.decode().strip())
    if proc.returncode != 0:
        raise RuntimeError(f"Failed to start worker for {task_id}")
    pid_file = PID_DIR / f"{task_id}.pid"
    pid = int(pid_file.read_text().strip())
    start_file = PID_DIR / f"{task_id}.start"
    if start_file.exists():
        try:
            start_times[task_id] = float(start_file.read_text().strip())
        except ValueError:
            start_times[task_id] = time.time()
    else:
        start_times[task_id] = time.time()
    worker = Worker(task_id=task_id, pid=pid, start_time=start_times[task_id])
    log_event(f"Started worker {worker.task_id} (PID {worker.pid})")
    active_tasks[worker.task_id] = worker
    return worker


def load_running_workers(existing: Dict[str, Worker]) -> Dict[str, Worker]:
    workers: Dict[str, Worker] = {}
    for pid_file in PID_DIR.glob("*.pid"):
        task = pid_file.stem
        pid = int(pid_file.read_text().strip())
        existing_start = existing.get(task, Worker(task, pid, start_times.get(task, time.time()))).start_time
        start = start_times.get(task, existing_start)
        if check_pid(pid):
            start_times[task] = start
            worker = Worker(task, pid, start)
            workers[task] = worker
            active_tasks[task] = worker
        else:
            pid_file.unlink(missing_ok=True)
            (pid_file.with_suffix(".pid.info")).unlink(missing_ok=True)
            (PID_DIR / f"{task}.start").unlink(missing_ok=True)
            mark_task_completed(task)
            start_times.pop(task, None)
            log_event(f"Detected completion of {task}")
    return workers


def print_status(workers: Dict[str, Worker], total_tasks: int):
    completed = len(list(COMPLETED_DIR.glob("task-*.md")))
    running = len(workers)
    pending = total_tasks - completed - running
    os.system("clear")
    print(f"Workers running: {running}/{MAX_WORKERS} | Completed: {completed} | Pending: {pending} | Total: {total_tasks}")
    print("\nTask ID       PID        Elapsed")
    print("--------------------------------")
    now = time.time()
    for worker in sorted(workers.values(), key=lambda w: w.task_id):
        elapsed_seconds = max(0, now - worker.start_time)
        minutes, seconds = divmod(int(elapsed_seconds), 60)
        print(f"{worker.task_id:<12} {worker.pid:<10} {minutes:02d}:{seconds:02d}")


def orchestrate():
    existing = tidy_pid_files()
    workers = load_running_workers(existing)
    all_tasks = pending_tasks()
    total_tasks = len(all_tasks) + len(list(COMPLETED_DIR.glob("task-*.md"))) + len(workers)

    while True:
        workers = load_running_workers(workers)
        all_tasks = pending_tasks()
        while len(workers) < MAX_WORKERS and all_tasks:
            task_file = all_tasks.pop(0)
            task_id = task_file.stem
            if task_id in workers:
                continue
            worker = start_worker(task_file)
            workers[worker.task_id] = worker
        print_status(workers, total_tasks)
        if not workers and not all_tasks:
            print("All tasks completed.")
            break
        time.sleep(REFRESH_INTERVAL)

if __name__ == "__main__":
    orchestrate()
