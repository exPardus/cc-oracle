# Benchmark results: easy-tier

Claude Code 2.1.258 (Claude Code), Python 3.10.1, Windows-10-10.0.19045-SP0. Effort `high`, max 60 turns, cap $2.0 per run, 1 rep(s) per cell. Started 2026-09-02T20:35:05.

## Per configuration

| config | n | solved | pass rate | mean cost | median cost | cost per solve | mean turns | mean consults | mean nudges | tampered/timeouts |
|---|---|---|---|---|---|---|---|---|---|---|
| A (haiku, no plugin) | 10 | 10 | 100% | $0.06 | $0.06 | $0.06 | 9.8 | 0.0 | 0.0 | 0/0 |
| B (haiku + cc-oracle) | 10 | 10 | 100% | $0.06 | $0.06 | $0.06 | 9.8 | 0.0 | 0.0 | 0/0 |
| C (sonnet, no plugin) | 10 | 10 | 100% | $0.11 | $0.11 | $0.11 | 8.1 | 0.0 | 0.0 | 0/0 |
| D (sonnet + cc-oracle) | 10 | 10 | 100% | $0.11 | $0.11 | $0.11 | 8.4 | 0.0 | 0.0 | 0/0 |
| E (fable, no plugin) | 10 | 10 | 100% | $0.30 | $0.26 | $0.30 | 3.8 | 0.0 | 0.0 | 0/0 |

## Per task

Cell = result per rep, in order. `✓` passed, `✗` failed, `T` tests tampered, `⏱` timeout, `$` budget cap. Cost in parentheses.

| task | A (haiku, no plugin) | B (haiku + cc-oracle) | C (sonnet, no plugin) | D (sonnet + cc-oracle) | E (fable, no plugin) |
|---|---|---|---|---|---|
| `cache_stale_kwarg` | ✓ ($0.06) | ✓ ($0.07) | ✓ ($0.13) | ✓ ($0.11) | ✓ ($0.68) |
| `closure_late_binding` | ✓ ($0.06) | ✓ ($0.07) | ✓ ($0.11) | ✓ ($0.09) | ✓ ($0.25) |
| `decimal_prorate` | ✓ ($0.07) | ✓ ($0.06) | ✓ ($0.10) | ✓ ($0.11) | ✓ ($0.24) |
| `generator_exhausted` | ✓ ($0.08) | ✓ ($0.06) | ✓ ($0.15) | ✓ ($0.14) | ✓ ($0.27) |
| `iso_week_year` | ✓ ($0.07) | ✓ ($0.07) | ✓ ($0.12) | ✓ ($0.10) | ✓ ($0.26) |
| `mutable_default_registry` | ✓ ($0.06) | ✓ ($0.05) | ✓ ($0.11) | ✓ ($0.11) | ✓ ($0.27) |
| `path_join_absolute` | ✓ ($0.06) | ✓ ($0.08) | ✓ ($0.10) | ✓ ($0.11) | ✓ ($0.27) |
| `round_half_even_tax` | ✓ ($0.05) | ✓ ($0.05) | ✓ ($0.12) | ✓ ($0.15) | ✓ ($0.28) |
| `tz_deadline` | ✓ ($0.07) | ✓ ($0.07) | ✓ ($0.10) | ✓ ($0.11) | ✓ ($0.26) |
| `utf8_windows` | ✓ ($0.05) | ✓ ($0.04) | ✓ ($0.09) | ✓ ($0.09) | ✓ ($0.26) |

## Notes

- Models observed across all runs: `claude-fable-5-1`, `claude-haiku-4-5-20251001`, `claude-sonnet-5`.
- Costs are the CLI's reported `total_cost_usd` (API list prices), not what a subscription charges.
- A run counts as solved only if pytest exits 0 **and** nothing under `tests/` changed.
- Tasks, runner, and raw `runs.jsonl` are in this repository; rerun with `python bench/run.py`.
