#!/usr/bin/env python3
"""Prove every benchmark task is well-formed before any model touches it.

For each bench/tasks/<name>/:
  1. as shipped (without solution/), pytest must FAIL
  2. with solution/ overlaid, pytest must PASS
  3. TRAP.md must exist and solution/ must not contain tests/

    python bench/verify_tasks.py            # all tasks
    python bench/verify_tasks.py decimal_prorate
"""
from __future__ import annotations

import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent
TASKS = ROOT / "tasks"
EXCLUDE = {"solution", "TRAP.md", "__pycache__", ".pytest_cache", ".git"}


def pytest_in(d: Path) -> tuple[int, str]:
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
    cp = subprocess.run([sys.executable, "-m", "pytest", "-q", "-p", "no:cacheprovider", "tests"],
                        cwd=d, capture_output=True, text=True, timeout=180, env=env)
    tail = (cp.stdout.strip().splitlines() or [""])[-1]
    return cp.returncode, tail


def overlay(src: Path, dst: Path) -> list[str]:
    copied = []
    for p in src.rglob("*"):
        if p.is_file() and "__pycache__" not in p.parts:
            rel = p.relative_to(src)
            target = dst / rel
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(p, target)
            copied.append(rel.as_posix())
    return copied


def verify(task: Path) -> list[str]:
    problems = []
    if not (task / "tests").is_dir():
        return ["no tests/ directory"]
    if not (task / "TRAP.md").is_file():
        problems.append("missing TRAP.md")
    sol = task / "solution"
    if not sol.is_dir():
        return problems + ["missing solution/"]
    if (sol / "tests").exists():
        problems.append("solution/ must not contain tests/")

    with tempfile.TemporaryDirectory(prefix="bench-verify-") as tmp:
        work = Path(tmp) / task.name
        shutil.copytree(task, work, ignore=lambda d, names: [n for n in names if n in EXCLUDE])
        rc, tail = pytest_in(work)
        if rc == 0:
            problems.append(f"passes as shipped ({tail})")
        elif not re.search(r"\d+ (failed|error)", tail):
            problems.append(f"does not fail cleanly as shipped ({tail})")
        shipped_tail = tail

        copied = overlay(sol, work)
        rc, tail = pytest_in(work)
        if rc != 0:
            problems.append(f"solution does not pass ({tail})")
        print(f"  {task.name:28} shipped: {shipped_tail:34}  solved: {tail:28}  solution files: {len(copied)}")
    return problems


def main(argv: list[str]) -> int:
    names = argv or sorted(p.name for p in TASKS.iterdir() if p.is_dir())
    bad = 0
    print("verifying tasks:")
    for n in names:
        probs = verify(TASKS / n)
        for p in probs:
            print(f"  !! {n}: {p}")
        bad += bool(probs)
    print(f"{len(names) - bad}/{len(names)} tasks well-formed")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
