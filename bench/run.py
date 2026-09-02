#!/usr/bin/env python3
"""cc-oracle benchmark runner.

Runs each task under each configuration in a fresh working copy, drives
Claude Code in print mode, then scores the result with pytest. Stdlib only.
Design and rationale: docs/specs/2026-09-02-benchmark-design.md.

    python bench/run.py --tasks decimal_prorate --configs A,B --reps 1 --cap 2
    python bench/run.py --tasks all --configs A,B,C,D,E --reps 1 --cap 3 --parallel 2
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

ROOT = Path(__file__).resolve().parent  # bench/
REPO = ROOT.parent  # plugin root, also the --plugin-dir
TASKS = ROOT / "tasks"

PROMPT = """The test suite in tests/ has failures. Fix the code so that every test passes.

Rules:
- Do not modify, delete, or add anything under tests/.
- Run `python -m pytest -q` to check your work.
- Stop as soon as the whole suite passes."""

CONFIGS = {
    "A": {"model": "haiku", "plugin": False, "label": "haiku, no plugin"},
    "B": {"model": "haiku", "plugin": True, "label": "haiku + cc-oracle"},
    "C": {"model": "sonnet", "plugin": False, "label": "sonnet, no plugin"},
    "D": {"model": "sonnet", "plugin": True, "label": "sonnet + cc-oracle"},
    "E": {"model": "fable", "plugin": False, "label": "fable, no plugin"},
    "F": {"model": "opus", "plugin": False, "label": "opus, no plugin"},
}

# Never staged into the working copy: the model must not see the answer.
EXCLUDE = {"solution", "TRAP.md", "__pycache__", ".pytest_cache", ".git"}

NUDGE_MARK = "without consulting the oracle"


# ----------------------------------------------------------------- helpers


def now_stamp() -> str:
    return dt.datetime.now().strftime("%Y%m%d-%H%M%S")


def hash_tree(root: Path) -> str:
    """Content hash of every file under root, order-independent."""
    h = hashlib.sha256()
    if not root.exists():
        return "missing"
    for p in sorted(root.rglob("*")):
        if p.is_file() and "__pycache__" not in p.parts:
            h.update(p.relative_to(root).as_posix().encode())
            h.update(b"\0")
            h.update(p.read_bytes())
            h.update(b"\0")
    return h.hexdigest()


def claude_command() -> list[str]:
    """Resolve the claude executable in a way subprocess can launch on every OS."""
    exe = shutil.which("claude")
    if not exe:
        sys.exit("claude not found on PATH")
    if os.name == "nt" and exe.lower().endswith((".cmd", ".bat")):
        return ["cmd.exe", "/d", "/c", exe]
    return [exe]


def write_isolation_files(out: Path) -> tuple[Path, Path]:
    """off.json disables every user-scope plugin; empty_mcp.json disables MCP.

    Verified 2026-09-02: with both, the init event lists no MCP servers and
    only the --plugin-dir plugin (if any). See the spec.
    """
    settings = Path.home() / ".claude" / "settings.json"
    enabled: dict = {}
    if settings.exists():
        try:
            enabled = json.loads(settings.read_text(encoding="utf-8")).get("enabledPlugins", {}) or {}
        except Exception:
            enabled = {}
    off = out / "off.json"
    off.write_text(json.dumps({"enabledPlugins": {k: False for k in enabled}}, indent=2), encoding="utf-8")
    mcp = out / "empty_mcp.json"
    mcp.write_text('{"mcpServers": {}}\n', encoding="utf-8")
    return off, mcp


def stage(task_dir: Path, workdir: Path) -> None:
    """Fresh working copy without the solution, initialised as a git repo."""
    if workdir.exists():
        shutil.rmtree(workdir)
    shutil.copytree(task_dir, workdir, ignore=lambda d, names: [n for n in names if n in EXCLUDE])
    git = ["git", "-c", "user.name=bench", "-c", "user.email=bench@example.invalid", "-c", "core.autocrlf=false"]
    subprocess.run(git + ["init", "-q"], cwd=workdir, check=True)
    subprocess.run(git + ["add", "-A"], cwd=workdir, check=True)
    subprocess.run(git + ["commit", "-q", "-m", "task as shipped"], cwd=workdir, check=True)


def build_cmd(cfg: dict, off: Path, mcp: Path, plugin_dir: Path, cap: float, max_turns: int, effort: str) -> list[str]:
    cmd = claude_command() + [
        "-p",
        "--no-session-persistence",
        "--settings", str(off),
        "--strict-mcp-config", "--mcp-config", str(mcp),
        "--disable-slash-commands",
        "--model", cfg["model"],
        "--effort", effort,
        "--permission-mode", "bypassPermissions", "--dangerously-skip-permissions",
        "--max-turns", str(max_turns),
        "--max-budget-usd", str(cap),
        "--output-format", "stream-json", "--verbose",
    ]
    if cfg["plugin"]:
        cmd += ["--plugin-dir", str(plugin_dir)]
    return cmd


def kill_tree(proc: subprocess.Popen) -> None:
    if os.name == "nt":
        subprocess.run(["taskkill", "/F", "/T", "/PID", str(proc.pid)], capture_output=True)
    else:
        proc.kill()


def parse_stream(path: Path) -> dict:
    """Pull metrics out of the stream-json transcript. Tolerates junk lines."""
    info = {
        "result": None, "init": None,
        "oracle_consults": 0, "test_runs": 0, "nudges": 0,
        "tool_calls": 0, "edits": 0,
    }
    if not path.exists():
        return info
    with path.open(encoding="utf-8", errors="replace") as f:
        for line in f:
            line = line.strip()
            if not line.startswith("{"):
                continue
            try:
                ev = json.loads(line)
            except Exception:
                continue
            if NUDGE_MARK in line and ev.get("type") != "system":
                # The Stop hook's block reason is fed back to the model as a
                # message; the doctrine text does not contain this phrase.
                info["nudges"] += 1
            t = ev.get("type")
            if t == "system" and ev.get("subtype") == "init":
                info["init"] = {k: ev.get(k) for k in ("model", "permissionMode", "plugins", "mcp_servers")}
            elif t == "result":
                info["result"] = ev
            elif t == "assistant":
                for block in (ev.get("message") or {}).get("content") or []:
                    if block.get("type") != "tool_use":
                        continue
                    info["tool_calls"] += 1
                    name = block.get("name") or ""
                    inp = block.get("input") or {}
                    if name in ("Agent", "Task") and "oracle" in str(inp.get("subagent_type", "")).lower():
                        info["oracle_consults"] += 1
                    elif name in ("Bash", "PowerShell") and "pytest" in str(inp.get("command", "")):
                        info["test_runs"] += 1
                    elif name in ("Edit", "Write", "MultiEdit", "NotebookEdit"):
                        info["edits"] += 1
    return info


def run_pytest(workdir: Path) -> dict:
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
    try:
        cp = subprocess.run(
            [sys.executable, "-m", "pytest", "-q", "-p", "no:cacheprovider", "tests"],
            cwd=workdir, capture_output=True, text=True, timeout=180, env=env,
        )
        tail = (cp.stdout.strip().splitlines() or [""])[-1]
        rc = cp.returncode
    except subprocess.TimeoutExpired:
        tail, rc = "pytest timed out", 124

    def count(word: str) -> int:
        m = re.search(r"(\d+) " + word, tail)
        return int(m.group(1)) if m else 0

    return {"rc": rc, "passed_n": count("passed"), "failed_n": count("failed") + count("error"), "tail": tail}


def git_changed_files(workdir: Path) -> list[str]:
    cp = subprocess.run(["git", "status", "--porcelain"], cwd=workdir, capture_output=True, text=True)
    return [l[3:] for l in cp.stdout.splitlines() if l.strip() and not l[3:].startswith("_")]


# --------------------------------------------------------------- one run


def run_one(task: str, cfg_id: str, rep: int, args, out: Path, off: Path, mcp: Path, lock: threading.Lock) -> dict:
    cfg = CONFIGS[cfg_id]
    task_dir = TASKS / task
    workdir = Path(args.scratch) / out.name / f"{task}-{cfg_id}-{rep}"
    stage(task_dir, workdir)
    tests_before = hash_tree(workdir / "tests")

    stream = workdir / "_stream.jsonl"
    stderr = workdir / "_stderr.txt"
    cmd = build_cmd(cfg, off, mcp, args.plugin_dir, args.cap, args.max_turns, args.effort)
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")

    t0 = time.time()
    stop_reason = "unknown"
    with stream.open("w", encoding="utf-8") as so, stderr.open("w", encoding="utf-8") as se:
        proc = subprocess.Popen(cmd, cwd=workdir, stdin=subprocess.PIPE, stdout=so, stderr=se, env=env, text=True, encoding="utf-8")
        try:
            proc.communicate(input=PROMPT, timeout=args.timeout)
        except subprocess.TimeoutExpired:
            kill_tree(proc)
            try:
                proc.communicate(timeout=30)
            except Exception:
                pass
            stop_reason = "timeout"
    wall_ms = int((time.time() - t0) * 1000)
    exit_code = proc.returncode
    stderr_head = stderr.read_text(encoding="utf-8", errors="replace")[:300].strip()

    info = parse_stream(stream)
    res = info["result"] or {}
    if stop_reason != "timeout":
        stop_reason = res.get("subtype") or ("no_result" if not res else "unknown")
    err_text = str(res.get("result") or "") + " " + stderr.read_text(encoding="utf-8", errors="replace")[:500]
    if res.get("is_error") and ("limit" in err_text.lower() or str(res.get("api_error_status")) == "429"):
        stop_reason = "rate_limited"

    # Score. The run edits nothing under tests/ by rule; any change is a fail.
    tests_after = hash_tree(workdir / "tests")
    tampered = tests_after != tests_before
    py = run_pytest(workdir)
    passed = (py["rc"] == 0) and not tampered

    usage = res.get("usage") or {}
    row = {
        "task": task, "config": cfg_id, "config_label": cfg["label"], "rep": rep,
        "passed": passed, "tampered": tampered,
        "pytest_passed": py["passed_n"], "pytest_failed": py["failed_n"], "pytest_tail": py["tail"],
        "stop_reason": stop_reason, "is_error": bool(res.get("is_error")), "exit_code": exit_code, "stderr_head": stderr_head,
        "cost_usd": res.get("total_cost_usd"), "num_turns": res.get("num_turns"),
        "duration_ms": res.get("duration_ms"), "wall_ms": wall_ms,
        "input_tokens": usage.get("input_tokens"), "output_tokens": usage.get("output_tokens"),
        "cache_read_tokens": usage.get("cache_read_input_tokens"),
        "cache_create_tokens": usage.get("cache_creation_input_tokens"),
        "models": sorted((res.get("modelUsage") or {}).keys()),
        "oracle_consults": info["oracle_consults"], "nudges": info["nudges"],
        "test_runs": info["test_runs"], "edits": info["edits"], "tool_calls": info["tool_calls"],
        "init": info["init"],
        "changed_files": git_changed_files(workdir),
        "workdir": str(workdir),
        "cmd": cmd,
        "started": dt.datetime.fromtimestamp(t0).isoformat(timespec="seconds"),
    }
    with lock:
        with (out / "runs.jsonl").open("a", encoding="utf-8") as f:
            f.write(json.dumps(row, ensure_ascii=True) + "\n")
        mark = "PASS" if passed else ("TAMPERED" if tampered else "FAIL")
        print(f"[{mark:8}] {task:26} {cfg_id} rep{rep}  ${row['cost_usd'] or 0:.2f}  turns={row['num_turns']}  "
              f"consults={row['oracle_consults']} nudges={row['nudges']}  stop={stop_reason}  {py['tail'][:60]}", flush=True)
        if stop_reason in ("no_result", "timeout") or exit_code:
            print(f"           exit={exit_code} stderr: {stderr_head[:160]!r}", flush=True)
    return row


# ------------------------------------------------------------------ main


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--tasks", default="all", help="comma-separated task names, or 'all'")
    ap.add_argument("--configs", default="A,B,C,D,E", help="comma-separated config ids from " + ",".join(CONFIGS))
    ap.add_argument("--reps", type=int, default=1)
    ap.add_argument("--cap", type=float, default=2.0, help="--max-budget-usd per run")
    ap.add_argument("--max-turns", type=int, default=60)
    ap.add_argument("--effort", default="high")
    ap.add_argument("--timeout", type=int, default=900, help="wall-clock seconds per run")
    ap.add_argument("--parallel", type=int, default=1)
    ap.add_argument("--plugin-dir", type=Path, default=REPO)
    ap.add_argument("--scratch", default=os.environ.get("BENCH_SCRATCH") or str(Path(os.environ.get("TEMP", "/tmp")) / "cc-oracle-bench"))
    ap.add_argument("--out", type=Path, default=None, help="results dir (default bench/results/<timestamp>)")
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--resume", action="store_true", help="skip cells already in <out>/runs.jsonl (rate-limited rows are redone)")
    args = ap.parse_args()

    tasks = sorted(p.name for p in TASKS.iterdir() if (p / "tests").is_dir()) if args.tasks == "all" else args.tasks.split(",")
    for t in tasks:
        if not (TASKS / t / "tests").is_dir():
            sys.exit(f"unknown task: {t}")
    cfg_ids = args.configs.split(",")
    for c in cfg_ids:
        if c not in CONFIGS:
            sys.exit(f"unknown config: {c}")

    out = (args.out or (ROOT / "results" / now_stamp())).resolve()
    out.mkdir(parents=True, exist_ok=True)
    args.plugin_dir = args.plugin_dir.resolve()
    args.scratch = str(Path(args.scratch).resolve())
    off, mcp = write_isolation_files(out)

    ver = subprocess.run(claude_command() + ["--version"], capture_output=True, text=True).stdout.strip()
    meta = {
        "started": dt.datetime.now().isoformat(timespec="seconds"),
        "claude_version": ver, "python": sys.version.split()[0], "platform": platform.platform(),
        "tasks": tasks, "configs": {c: CONFIGS[c] for c in cfg_ids}, "reps": args.reps,
        "cap_usd": args.cap, "max_turns": args.max_turns, "effort": args.effort, "timeout_s": args.timeout,
        "prompt": PROMPT, "plugin_dir": str(args.plugin_dir),
    }
    (out / "meta.json").write_text(json.dumps(meta, indent=2), encoding="utf-8")

    plan = [(t, c, r) for r in range(1, args.reps + 1) for t in tasks for c in cfg_ids]
    if args.resume and (out / "runs.jsonl").exists():
        done = set()
        kept = []
        for line in (out / "runs.jsonl").read_text(encoding="utf-8").splitlines():
            if not line.strip():
                continue
            row = json.loads(line)
            if row.get("stop_reason") == "rate_limited":
                continue  # redo it
            done.add((row["task"], row["config"], row["rep"]))
            kept.append(line)
        (out / "runs.jsonl").write_text("\n".join(kept) + ("\n" if kept else ""), encoding="utf-8")
        plan = [cell for cell in plan if cell not in done]
        print(f"resume: {len(done)} cells kept, {len(plan)} to run")
    print(f"{len(plan)} runs -> {out}  (claude {ver}, effort={args.effort}, cap=${args.cap}/run)")
    if args.dry_run:
        for t, c, r in plan:
            print("  ", t, c, r)
        print("cmd:", " ".join(build_cmd(CONFIGS[cfg_ids[0]], off, mcp, args.plugin_dir, args.cap, args.max_turns, args.effort)))
        return 0

    lock = threading.Lock()
    halted = threading.Event()

    def guarded(t, c, r):
        if halted.is_set():
            return None
        row = run_one(t, c, r, args, out, off, mcp, lock)
        if row["stop_reason"] == "rate_limited":
            halted.set()
        return row

    with ThreadPoolExecutor(max_workers=max(1, args.parallel)) as ex:
        futures = [ex.submit(guarded, t, c, r) for t, c, r in plan]
        for fu in futures:
            try:
                fu.result()
            except Exception as e:  # keep going; the row is simply missing
                with lock:
                    print(f"[ERROR   ] {e!r}", flush=True)
    if halted.is_set():
        print(f"HALTED on a rate limit. When it resets, rerun the same command with --resume --out {out}", flush=True)

    try:
        sys.path.insert(0, str(ROOT))
        import summarize  # type: ignore
        summarize.write_summary(out)
        print(f"summary -> {out / 'summary.md'}")
    except Exception as e:
        print(f"(summary skipped: {e!r})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
