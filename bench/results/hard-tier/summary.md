# Benchmark results: hard-tier

Claude Code 2.1.258 (Claude Code), Python 3.10.1, Windows-10-10.0.19045-SP0. Effort `high`, max 60 turns, cap $3.0 per run, 1 rep(s) per cell. Started 2026-09-02T20:48:59.

## Per configuration

| config | n | solved | pass rate | mean cost | median cost | cost per solve | mean turns | mean consults | mean nudges | tampered/timeouts |
|---|---|---|---|---|---|---|---|---|---|---|
| A (haiku, no plugin) | 6 | 6 | 100% | $0.07 | $0.07 | $0.07 | 10.2 | 0.0 | 0.0 | 0/0 |
| B (haiku + cc-oracle) | 6 | 6 | 100% | $0.08 | $0.07 | $0.08 | 11.0 | 0.0 | 0.0 | 0/0 |
| C (sonnet, no plugin) | 6 | 6 | 100% | $0.11 | $0.10 | $0.11 | 8.7 | 0.0 | 0.0 | 0/0 |
| D (sonnet + cc-oracle) | 6 | 6 | 100% | $0.11 | $0.11 | $0.11 | 8.7 | 0.0 | 0.0 | 0/0 |
| E (fable, no plugin) | 6 | 6 | 100% | $0.27 | $0.27 | $0.27 | 4.0 | 0.0 | 0.0 | 0/0 |

## Per task

Cell = result per rep, in order. `✓` passed, `✗` failed, `T` tests tampered, `⏱` timeout, `$` budget cap. Cost in parentheses.

| task | A (haiku, no plugin) | B (haiku + cc-oracle) | C (sonnet, no plugin) | D (sonnet + cc-oracle) | E (fable, no plugin) |
|---|---|---|---|---|---|
| `conftest_warning_error` | ✓ ($0.05) | ✓ ($0.05) | ✓ ($0.09) | ✓ ($0.12) | ✓ ($0.25) |
| `import_shadow` | ✓ ($0.11) | ✓ ($0.13) | ✓ ($0.16) | ✓ ($0.13) | ✓ ($0.29) |
| `onion_three_layers` | ✓ ($0.07) | ✓ ($0.05) | ✓ ($0.10) | ✓ ($0.10) | ✓ ($0.25) |
| `shared_cache_order` | ✓ ($0.07) | ✓ ($0.09) | ✓ ($0.11) | ✓ ($0.12) | ✓ ($0.30) |
| `unicode_normalization` | ✓ ($0.06) | ✓ ($0.07) | ✓ ($0.12) | ✓ ($0.10) | ✓ ($0.27) |
| `windows_replace` | ✓ ($0.07) | ✓ ($0.07) | ✓ ($0.10) | ✓ ($0.10) | ✓ ($0.27) |

## Notes

- Models observed across all runs: `claude-fable-5-1`, `claude-haiku-4-5-20251001`, `claude-sonnet-5`.
- Costs are the CLI's reported `total_cost_usd` (API list prices), not what a subscription charges.
- A run counts as solved only if pytest exits 0 **and** nothing under `tests/` changed.
- Tasks, runner, and raw `runs.jsonl` are in this repository; rerun with `python bench/run.py`.
