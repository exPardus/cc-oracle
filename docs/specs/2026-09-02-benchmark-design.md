# Benchmark design: does cc-oracle change outcomes for a mid-tier model?

Status: approved for a pilot on 2026-09-02. Results, when they exist, live
under `bench/results/` and are summarized in the README.

## Goal

One repeatable number that a stranger can check: on tasks where coding
sessions get stuck, does adding cc-oracle to a mid-tier model change the
pass rate and the cost, and how does that compare with just using the top
model for everything?

Everything needed to rerun it is committed: the tasks, the runner, the
aggregation, and the exact CLI flags.

## Configurations

All runs use `claude -p` (print mode) with the same prompt, the same flags,
and a fresh copy of the task directory. The only differences between
configurations are the model and whether the plugin is loaded.

| id | model | plugin | what it measures |
|---|---|---|---|
| A | `haiku` | off | the cheapest session model on its own |
| B | `haiku` | cc-oracle via `--plugin-dir` | the cheapest session model with an oracle to consult |
| C | `sonnet` | off | the mid-tier model on its own |
| D | `sonnet` | cc-oracle via `--plugin-dir` | the mid-tier model with an oracle to consult |
| E | `fable` | off | the oracle's own model doing everything itself |

`fable` is what the oracle agent runs on, so E is the honest "just pay for
the big model" baseline. An optional F (`opus`, plugin off) can be added if
budget allows. Haiku was added after the pilot: Sonnet solved the first task
in eight turns with or without the plugin, so the tier where sessions
actually stall had to be in the grid.

### Isolation

The machine running the benchmark has fourteen user-scope plugins
installed, including cc-oracle itself. `--bare` would give a clean slate but
also disables OAuth, so runs use normal mode with a `--settings` override
that sets every user-scope plugin to `false`. Verified on 2026-09-02: with
the override, Sonnet reports no oracle agent and no `<oracle-plugin>` block;
with the override plus `--plugin-dir`, it reports both. There is no global
`CLAUDE.md` and no user-level hooks on this machine.

### Flags, verbatim

```
claude -p
  --no-session-persistence
  --settings <off.json>            # every user plugin: false
  --model <haiku|sonnet|fable>
  [--plugin-dir C:/proga/claude-oracle]   # configs B and D only
  --effort high                    # pinned; the machine default is xhigh
  --permission-mode bypassPermissions --dangerously-skip-permissions
  --max-turns 60
  --max-budget-usd <cap>           # per-run safety cap
  --output-format stream-json --verbose
  "<prompt>"  < /dev/null
```

Working directory: a fresh copy of `bench/tasks/<task>/` under the session
scratchpad, with `git init` and one commit so the model sees a repository.

### Prompt, verbatim

```
The test suite in tests/ has failures. Fix the code so that every test passes.

Rules:
- Do not modify, delete, or add anything under tests/.
- Run `python -m pytest -q` to check your work.
- Stop as soon as the whole suite passes.
```

## Tasks

Each task is a small Python package plus a pytest suite in which one or more
tests fail. The rules that make a task worth running:

1. The failing test names a symptom. The root cause is in a different file.
2. The obvious symptom-level fix makes a *different* test fail, so a model
   that patches what it sees enters a loop.
3. The whole thing is small: two to four source files, three to six tests.
4. Every task ships `solution/` (the fixed source files) and `TRAP.md`
   (symptom, trap, real fix, in three short paragraphs), so the design is
   pre-registered and auditable.
5. `bench/verify_tasks.py` proves, before any model run, that the shipped
   code fails the suite and the solution passes it.

`tests/` is hashed before the run and compared after. A run that changed,
added, or deleted anything under `tests/` is scored as failed and flagged
`tampered`, regardless of what pytest says.

The ten tasks and their traps:

| task | symptom test sees | trap | real fix |
|---|---|---|---|
| `decimal_prorate` | prorated amount off by one cent | rounding looks wrong; the ratio is built from a float two lines earlier | build the ratio from `Decimal`s |
| `tz_deadline` | overdue check raises or is wrong | `now()` looks like the culprit; the parser strips the `Z` and returns naive datetimes | make the parser emit aware datetimes |
| `utf8_windows` | round-tripped note is garbled | `errors="ignore"` silences it and loses data | `encoding="utf-8"` on both open calls |
| `mutable_default_registry` | two registries share state | fixing the visible default leaves a second one in another class | `None` sentinel in both places |
| `closure_late_binding` | every callback selects the last item | fixing the loop in one module leaves the same bug in the dispatcher | bind the loop variable at definition time, both places |
| `generator_exhausted` | rendered report has a total but no lines | materializing in `render()` leaves `summary()` broken the same way | materialize once at the entry points |
| `round_half_even_tax` | `2.675` rounds to `2.67` | adding an epsilon fixes one case and breaks the negative and `1.005` cases | `Decimal(str(x))` with `ROUND_HALF_UP` |
| `path_join_absolute` | asset resolver escapes its root | stripping the leading slash fixes one test; `..` traversal still escapes | normalize and check `commonpath` |
| `cache_stale_kwarg` | discounted quote returns the undiscounted price | removing the cache fixes correctness and fails the cache-hit test | include the kwarg in the cached signature |
| `iso_week_year` | 29 Dec 2025 labeled week 52 of 2025 | `%V` fixes the week and breaks the year | `date.isocalendar()` |

`utf8_windows` only fails where Python's default text encoding is not UTF-8.
It is on the benchmark machine (cp1252). The write-up says so.

### Hard tier

The ten tasks above turned out to be first-try solves for Haiku, Sonnet, and
Fable alike (see Run plan). They measure overhead, not rescue. Six more tasks
were built to make a small model genuinely stall: the failing test points
somewhere the bug is not, the obvious moves fail, and in two cases the
environment itself is the bug.

| task | symptom test sees | trap | real fix |
|---|---|---|---|
| `import_shadow` | `calendar.monthrange` missing inside correct-looking source | a `tests/calendar.py` helper shadows the stdlib through pytest's sys.path prepend; renaming it is forbidden, `--import-mode=importlib` breaks the helper's own test | stop importing `calendar` in the package |
| `shared_cache_order` | a test fails in the suite and passes alone | module cache uses `setdefault` so the first fixture wins; clearing at import does nothing; fixing one loader leaves the second | last load wins in both loaders |
| `onion_three_layers` | env override never applies | fixing it exposes three masked tests (coercion, precedence) that read as a regression and invite a revert | all three layers |
| `windows_replace` | overwrite raises `PermissionError`, then `FileExistsError` | handle still open at rename; `os.rename` refuses to replace on Windows; `shutil.copy` fallback leaves a temp file; `os.remove` first is blocked by a monkeypatched test | close, then `os.replace` |
| `conftest_warning_error` | export raises `DeprecationWarning` as an error | `tests/conftest.py` escalates the warning and cannot be edited; deleting or suppressing the warning fails the alias test | call the internal API by its new name |
| `unicode_normalization` | `Å` ≠ decomposed `Å`, `Straße` ≠ `STRASSE` | casefold alone, NFC alone, or fixing `normalize()` but not the registry's inline `.lower()` each leave tests failing | NFC + casefold, used at both sites |

`windows_replace` fails as shipped only on Windows; `import_shadow` ships a
`tests/conftest.py` that evicts a pre-imported stdlib `calendar` so the
collision reproduces regardless of which pytest plugins are installed.

## Metrics

Per run, written as one JSON line to `runs.jsonl`:

| field | source |
|---|---|
| `task`, `config`, `rep` | runner |
| `passed` | pytest exit 0 after the run AND `tests/` unchanged |
| `tampered` | `tests/` hash changed |
| `cost_usd`, `num_turns`, `duration_ms` | the final `result` event |
| `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_create_tokens` | `usage` in the result event |
| `models` | keys of `modelUsage` |
| `oracle_consults` | count of `Agent` tool calls whose `subagent_type` contains `oracle` |
| `nudges` | count of hook messages containing `without consulting the oracle` |
| `test_runs` | count of `Bash` tool calls containing `pytest` |
| `stop_reason` | `success`, `max_turns`, `budget`, `error`, `timeout` |
| `workdir` | kept for inspection |

Aggregation per configuration: pass rate with n, mean and median cost, cost
per solved task (total spend divided by solved count), mean turns, mean
consults. Plus a per-task grid of pass/fail across configurations.

## Run plan

1. **Pilot**: one task, three configurations, one rep, cap $2 per run.
   Done 2026-09-02: mechanics proven; Sonnet did not get stuck (8 turns,
   $0.11, with or without the plugin; Fable 4 turns, $0.74).
2. **Full, easy tier**: ten tasks, five configurations, one rep.
   Done 2026-09-02: see `bench/results/easy-tier/summary.md`.
3. **Full, hard tier**: six tasks, five configurations, one rep, cap $3.
   Done 2026-09-02: see `bench/results/hard-tier/summary.md`.
4. **Hard tier at `--effort low`**, Haiku and Sonnet cells only, to look for
   stalls at the cheapest setting. Done 2026-09-02: see
   `bench/results/hard-tier-effort-low/summary.md`.
5. **Reps**: not run. With 104 of 104 solved there is no difference to
   tighten.

## Results and conclusion (2026-09-02)

| config | runs | solved | mean cost | mean turns |
|---|---|---|---|---|
| A haiku | 22 | 22 | $0.068 | 10.2 |
| B haiku + cc-oracle | 22 | 22 | $0.070 | 10.2 |
| C sonnet | 22 | 22 | $0.118 | 8.0 |
| D sonnet + cc-oracle | 22 | 22 | $0.110 | 7.9 |
| E fable | 16 | 16 | $0.292 | 3.9 |

Across the 44 plugin runs: 398 turns, 0 oracle consults, 0 Stop-hook
nudges. Paired cost with the plugin was $3.97 against $4.07 without.

What this shows: the plugin is free when nothing stalls, and the Stop hook
produced no false positive in 398 turns of two models' output.

What it does not show: rescue. No configuration got stuck, so no consult
happened and the pass-rate question is unanswered. Tasks of this shape,
two to five files with a decoy fix, do not stall current models even at low
effort. A benchmark that could answer the rescue question needs a different
task class: larger unfamiliar codebases, long-horizon tasks where context
pollution accumulates over dozens of turns, or environment failures
(dependency and build problems) rather than logic bugs. That is future
work, and the harness is ready for it.

Runs are sequential or two at a time. Per-run wall-clock timeout 15 minutes;
the process tree is killed on timeout and the run is scored `timeout`.

## Validity, stated up front

- The tasks were written by Claude at the plugin author's request, with
  the plugin's failure mode in mind. They are pre-registered and published,
  but they are not a neutral sample of real work.
- n is small. Single-rep results are indicative, not conclusive.
- Effort is pinned to `high` for every configuration; other settings may
  differ.
- Whether `fable` is available depends on the plan. On this machine it
  resolves to `claude-fable-5-1`; the write-up records that.
- Costs are the CLI's own `total_cost_usd` and reflect API list prices, not
  what a subscription plan charges.
- One task is Windows-specific by design.

## Files

```
bench/
  README.md            how to run, how to read results
  run.py               the runner (stdlib only)
  summarize.py         runs.jsonl -> summary.md
  verify_tasks.py      every task fails as shipped, passes with solution/
  tasks/<name>/
    app/ or <pkg>/     the code under test
    tests/             pytest suite, read-only by rule
    solution/          fixed source files, same relative layout
    TRAP.md            symptom, trap, real fix
  results/<timestamp>/
    off.json           the settings override used
    runs.jsonl
    summary.md
```
