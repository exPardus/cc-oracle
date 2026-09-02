# Benchmark

Does cc-oracle change the pass rate and cost of a mid-tier model on tasks
where coding sessions get stuck, and how does that compare with using the
top model for everything? Design, rationale, and the validity caveats are in
[`docs/specs/2026-09-02-benchmark-design.md`](../docs/specs/2026-09-02-benchmark-design.md).
Results live under `results/`.

## Configurations

| id | model | plugin |
|---|---|---|
| A | `haiku` | off |
| B | `haiku` | cc-oracle via `--plugin-dir` |
| C | `sonnet` | off |
| D | `sonnet` | cc-oracle via `--plugin-dir` |
| E | `fable` | off |
| F | `opus` | off (optional) |

Every run is `claude -p` in a fresh copy of the task with all user plugins
disabled through a `--settings` override, no MCP servers, no skills, effort
pinned, a turn cap, and a per-run dollar cap. The runner records the exact
command in `runs.jsonl`.

## Tiers

`tasks/` holds two tiers. The easy tier (ten tasks: `decimal_prorate`,
`tz_deadline`, `utf8_windows`, `mutable_default_registry`,
`closure_late_binding`, `generator_exhausted`, `round_half_even_tax`,
`path_join_absolute`, `cache_stale_kwarg`, `iso_week_year`) is small,
non-local bugs with a decoy fix; every model solved them first try, so they
measure the plugin's overhead. The hard tier (`import_shadow`,
`shared_cache_order`, `onion_three_layers`, `windows_replace`,
`conftest_warning_error`, `unicode_normalization`) is built to stall a small
model. Each task's `TRAP.md` states the symptom, the trap, and the real fix.

## Run it

```sh
python bench/verify_tasks.py                      # every task fails as shipped, passes with solution/
python bench/run.py --tasks decimal_prorate --configs A,B --cap 2        # one task
python bench/run.py --tasks all --configs A,B,C,D,E --reps 3 --cap 3 --parallel 2
python bench/summarize.py bench/results/<run>     # rebuild summary.md from runs.jsonl
```

Requirements: Claude Code on `PATH` and logged in, Python 3.9+ with pytest.
Working copies go under `--scratch` (default `%TEMP%/cc-oracle-bench`) and
are kept for inspection; each holds `_stream.jsonl` (the full session) and
`_stderr.txt`.

## Read it

`summary.md` has two tables. Per configuration: n, solved, pass rate, mean
and median cost, cost per solved task, mean turns, mean oracle consults, mean
Stop-hook nudges. Per task: one cell per rep with pass/fail and cost.

A run counts as solved only if pytest exits 0 **and** nothing under `tests/`
changed. Runs that hit the turn cap, the dollar cap, or the wall-clock
timeout are scored as failed and labeled.

## Add a task

Copy the layout of `tasks/decimal_prorate/`: a small package, a `tests/`
directory, `solution/` with only the changed source files, and a `TRAP.md`
with three paragraphs (Symptom, Trap, Real fix). The rules that make a task
worth running are in the spec. Run `python bench/verify_tasks.py <name>`
until it reports the task well-formed.
