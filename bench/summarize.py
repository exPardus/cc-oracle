#!/usr/bin/env python3
"""Aggregate bench/results/<run>/runs.jsonl into summary.md. Stdlib only.

    python bench/summarize.py bench/results/20260902-190000
"""
from __future__ import annotations

import json
import statistics
import sys
from collections import defaultdict
from pathlib import Path


def load(out: Path) -> tuple[list[dict], dict]:
    rows = []
    with (out / "runs.jsonl").open(encoding="utf-8") as f:
        for line in f:
            if line.strip():
                rows.append(json.loads(line))
    meta = {}
    mp = out / "meta.json"
    if mp.exists():
        meta = json.loads(mp.read_text(encoding="utf-8"))
    return rows, meta


def fmt_money(x) -> str:
    return "-" if x is None else f"${x:.2f}"


def mean(xs):
    xs = [x for x in xs if x is not None]
    return statistics.mean(xs) if xs else None


def median(xs):
    xs = [x for x in xs if x is not None]
    return statistics.median(xs) if xs else None


def write_summary(out: Path) -> str:
    rows, meta = load(out)
    by_cfg: dict[str, list[dict]] = defaultdict(list)
    for r in rows:
        by_cfg[r["config"]].append(r)
    cfg_ids = sorted(by_cfg)
    labels = {c: by_cfg[c][0]["config_label"] for c in cfg_ids}

    lines = []
    lines.append(f"# Benchmark results: {out.name}\n")
    if meta:
        lines.append(
            f"Claude Code {meta.get('claude_version')}, Python {meta.get('python')}, {meta.get('platform')}. "
            f"Effort `{meta.get('effort')}`, max {meta.get('max_turns')} turns, cap ${meta.get('cap_usd')} per run, "
            f"{meta.get('reps')} rep(s) per cell. Started {meta.get('started')}.\n"
        )
    lines.append("## Per configuration\n")
    lines.append("| config | n | solved | pass rate | mean cost | median cost | cost per solve | mean turns | mean consults | mean nudges | tampered/timeouts |")
    lines.append("|---|---|---|---|---|---|---|---|---|---|---|")
    for c in cfg_ids:
        rs = by_cfg[c]
        n = len(rs)
        solved = sum(1 for r in rs if r["passed"])
        cost_total = sum(r["cost_usd"] or 0 for r in rs)
        cps = cost_total / solved if solved else None
        tampered = sum(1 for r in rs if r["tampered"])
        timeouts = sum(1 for r in rs if r["stop_reason"] == "timeout")
        lines.append(
            f"| {c} ({labels[c]}) | {n} | {solved} | {solved / n:.0%} | {fmt_money(mean([r['cost_usd'] for r in rs]))} | "
            f"{fmt_money(median([r['cost_usd'] for r in rs]))} | {fmt_money(cps)} | "
            f"{mean([r['num_turns'] for r in rs]) or 0:.1f} | {mean([r['oracle_consults'] for r in rs]) or 0:.1f} | "
            f"{mean([r['nudges'] for r in rs]) or 0:.1f} | {tampered}/{timeouts} |"
        )
    lines.append("")

    lines.append("## Per task\n")
    lines.append("Cell = result per rep, in order. `✓` passed, `✗` failed, `T` tests tampered, `⏱` timeout, `$` budget cap. Cost in parentheses.\n")
    lines.append("| task | " + " | ".join(f"{c} ({labels[c]})" for c in cfg_ids) + " |")
    lines.append("|---|" + "---|" * len(cfg_ids))
    tasks = sorted({r["task"] for r in rows})
    for t in tasks:
        cells = []
        for c in cfg_ids:
            rs = sorted((r for r in by_cfg[c] if r["task"] == t), key=lambda r: r["rep"])
            marks = []
            for r in rs:
                if r["tampered"]:
                    m = "T"
                elif r["stop_reason"] == "timeout":
                    m = "⏱"
                elif "budget" in str(r["stop_reason"]):
                    m = "$"
                else:
                    m = "✓" if r["passed"] else "✗"
                marks.append(f"{m} ({fmt_money(r['cost_usd'])})")
            cells.append(" ".join(marks) if marks else "-")
        lines.append(f"| `{t}` | " + " | ".join(cells) + " |")
    lines.append("")

    models = sorted({m for r in rows for m in (r.get("models") or [])})
    lines.append("## Notes\n")
    lines.append(f"- Models observed across all runs: {', '.join(f'`{m}`' for m in models) or 'none recorded'}.")
    lines.append("- Costs are the CLI's reported `total_cost_usd` (API list prices), not what a subscription charges.")
    lines.append("- A run counts as solved only if pytest exits 0 **and** nothing under `tests/` changed.")
    lines.append("- Tasks, runner, and raw `runs.jsonl` are in this repository; rerun with `python bench/run.py`.")
    text = "\n".join(lines) + "\n"
    (out / "summary.md").write_text(text, encoding="utf-8")
    return text


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    print(write_summary(Path(sys.argv[1])))
