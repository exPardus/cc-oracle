# Development

Everything a contributor needs that a user does not. The user-facing story
lives in the [README](../README.md).

## Two implementations, one behavior

The Go implementation is the one that ships; `hooks/oracle_hook.py` is kept as
the fallback and as the executable specification the Go port is tested against.

```sh
go test ./...          # Go unit tests
python -m pytest -q    # Python unit tests + the differential suite
sh scripts/build.sh    # cross-compile all six binaries from any one machine
```

`tests/test_differential.py` is the important one: it drives **both**
implementations with byte-identical stdin, environment, and transcript files and
asserts byte-identical stdout, including over real multi-megabyte session
transcripts. The unit tests on each side prove each side self-consistent; only
the differential suite proves they agree. It skips automatically when no Go
binary is built.

The implementation commits to a portability floor:

- **Standard library only** — no third-party modules, in either language.
- **Windows / macOS / Linux** — no platform-specific paths or shell assumptions.
- **Fail open, always** — any unexpected input, any error, any panic exits 0
  and stays silent. A hook must never wedge a session.
- **Encoding robustness** — transcripts are decoded with invalid bytes replaced
  (one bad byte never kills detection); emitted JSON is ASCII-escaped so it
  survives any console codepage.

## Building and shipping the binaries

Claude Code installs a plugin by cloning its repository. There is no build step
and no postinstall hook, so the six binaries under `hooks/bin/` are committed
and are the shipped artifact. `scripts/build.sh` cross-compiles every target
with `CGO_ENABLED=0` from any machine with a Go toolchain, then stages the
results and sets the executable bit on the Unix targets in the index, since a
Unix user who clones a binary without that bit gets `Permission denied` and
falls silently through the dispatch chain. The script refuses to finish if any
target is built but untracked.

`.gitattributes` marks `hooks/bin/**` as binary so no line-ending translation
can ever corrupt them, and pins `*.sh` to LF so the build script runs from a
Windows checkout.

How `hooks/hooks.json` picks the right binary at run time, and why trying the
wrong platform's binary first is safe, is in [`dispatch.md`](dispatch.md).

## Performance

The Stop hook runs at the end of every turn, so its cost is paid constantly and
is pure latency between a turn finishing and the session continuing. That is why
the implementation is compiled.

Median of 15 runs, Windows 10, same inputs driven through both implementations:

| scenario | Python | Go | |
|---|---|---|---|
| `session-start` | 88.2 ms | 18.4 ms | 4.8x |
| `stop`, small transcript | 80.6 ms | 21.8 ms | 3.7x |
| `stop`, real 3.7 MB transcript | 117.7 ms | 33.2 ms | 3.5x |
| 7 concurrent `stop` hooks, 3.7 MB | 184.6 ms | 56.8 ms | 3.3x |

The Stop payload carries `last_assistant_message`, so the final assistant text
is read from it directly rather than reconstructed from the transcript. That
retired the flush-race backoff entirely on any Claude Code that sends the field:
text-less turns used to spend up to 400ms waiting for the transcript to catch up,
and now spend none. The transcript is still read — the consult check and the
failure streak need it — but nothing waits on it, and older harnesses that omit
the field still fall back to the original bounded re-read.

Two things bound the remaining time, and neither is this code. Spawning *any*
executable on this Windows box costs ~17 ms, and the measurement harness adds
~9 ms; the hook binary in a no-op mode is indistinguishable from a trivial one.
On Linux and macOS, where process creation is far cheaper, the floor is lower.

The large-transcript figure depends on `transcript.LoadTurn`, which returns only
the current turn without parsing the entries before it. The hook discards
everything older anyway, and the turn boundary sits near the end of the file, so
parsing the whole transcript was wasted work — 109 ms of it on a 3.7 MB file,
versus 4.9 ms to parse just the turn, at 69x fewer allocations. Since the
flush-race retry re-reads the transcript, that saving is paid back on each
retry too.

## How the behavioral trigger threshold was chosen

The failure-streak trigger fires when three consecutive non-probe tool calls
fail in one turn. The threshold was set by measuring 1,741 real turns: 3+
consecutive non-probe failures occur in 0.34% of them, while looser signals (any
3 failures, or the same tool repeated) fire on 3-11% and would mostly catch
ordinary iterative work. Read-only lookups (`Read`, `Glob`, `Grep`, `LS`) are
excluded because a run of those failing is a model hunting for a file rather
than stalling; a successful call resets the count.

## Repo layout

| Path | Purpose |
|---|---|
| `.claude-plugin/plugin.json` | Plugin manifest |
| `.claude-plugin/marketplace.json` | Marketplace listing (lets this repo double as a marketplace) |
| `agents/oracle.md` | The oracle subagent definition |
| `cmd/oracle-hook/` | CLI entry point |
| `internal/detect/` | Marker + idiom-family detection, quote/question suppression |
| `internal/transcript/` | JSONL parsing, turn boundary, oracle-consult detection |
| `internal/config/` | `config.json` surface and data-dir resolution |
| `internal/state/` | Per-turn block record: atomic write, allowlisted pruning |
| `internal/pyjson/` | JSON encoder matching CPython's `json.dumps` byte for byte |
| `internal/hook/` | The two entry points and the flush-race handling |
| `manifest.go` | Plugin identity, embedded from the manifests at build time |
| `hooks/bin/` | The six committed binaries |
| `hooks/oracle_hook.py` | Python fallback and executable specification |
| `hooks/hooks.json` | SessionStart + Stop hook wiring, with platform dispatch |
| `scripts/build.sh` | Cross-compiles every target |
| `tests/test_differential.py` | Go ↔ Python parity over shared inputs |
| `tests/test_detection.py` | Marker + question/quote-suppression logic |
| `tests/test_transcript.py` | Transcript parsing / turn analysis |
| `tests/test_stop_entry.py` | End-to-end stdin→stdout behavior of the hook entrypoints |
| `tests/test_cli_smoke.py` | CLI-level smoke coverage of the hook binary |
| `tests/test_config.py` | Configuration surface + portability floor |

## Further reading

- [`dispatch.md`](dispatch.md) — how the platform binary is selected, and why it is safe
- [`specs/2026-08-08-go-port-contract.md`](specs/2026-08-08-go-port-contract.md) — the Go port contract and known Python/Go divergences
- [`specs/2026-07-23-oracle-plugin-design.md`](specs/2026-07-23-oracle-plugin-design.md) — design spec
- [`plans/2026-07-23-oracle-plugin.md`](plans/2026-07-23-oracle-plugin.md) — implementation plan
- [`research/2026-07-23-anthropic-docs-report.md`](research/2026-07-23-anthropic-docs-report.md) — official-docs research report
