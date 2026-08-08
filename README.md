# cc-oracle

A weaker (or any) model consults a best-model, read-only oracle the moment it's unsure or stuck, instead of flailing solo — fewer wasted tokens, better code.

## How it works

- **Doctrine** — a `SessionStart` hook injects a small standing instruction (via `additionalContext`): the moment a session notices it's unsure, stuck, confused, or going in circles, it should stop and consult the `oracle` agent with a full brief before attempting solo.
- **Oracle agent** — `agents/oracle.md` runs on the `fable` model alias, with read-only tools (`Read`, `Grep`, `Glob`; no `Bash`, `Edit`, or `Write`). It enforces a full-brief contract, investigates the codebase itself, and returns a diagnosis and plan for the caller to implement.
- **Stop-hook safety net** — a conservative marker check on the final assistant message of each turn, catching cases where the model states uncertainty (e.g. `"I'm not sure"`, `"I'm stuck"`, `"can't figure out"`) without having consulted the oracle. It suppresses questions posed to the user, text inside code fences/blockquotes/inline code/double quotes, and anything already covered by an oracle consult this turn; it blocks at most once per turn and fails open (silently) on any parse error or unexpected input.

## Install

```
/plugin marketplace add exPardus/cc-oracle
/plugin install oracle@cc-oracle
```

CLI variant:

```
claude plugin install oracle@cc-oracle
```

Local-directory variant (for development or before publishing):

```
/plugin marketplace add ./cc-oracle
```

## Usage

Nothing to do manually — once installed, the doctrine and Stop-hook are active in every session. A typical consult looks like this: the main model dispatches the `oracle` agent with a brief covering Goal, Problem, Tried, Context, and Question; the oracle reads the relevant code, diagnoses the root cause, and returns a concrete plan; the main model implements it.

This applies at any model tier — a strong model can consult the oracle too, for a fresh-context second opinion.

## The brief contract

- **Goal** — what the task ultimately wants
- **Problem** — the exact blocker, errors quoted verbatim
- **Tried** — attempts made and why each failed
- **Context** — relevant files/paths, versions, platform, project rules
- **Question** — the specific ask, not `"help"`

## Model selection

The oracle runs on the `fable` model alias, resolved per provider (Anthropic API, Bedrock, Vertex) — never a hardcoded model ID. Per official docs, an alias unavailable on the caller's plan/provider falls back silently to the session's own (inherited) model; no error reaches the caller. Separately, if the oracle *dispatch itself* errors, the doctrine instructs one retry of the same call with `model: opus`.

## Configuration

Optional, file-based, fail-open. One file, plugin-local:

```
<CLAUDE_PLUGIN_DATA>/oracle-state/config.json
```

(falling back to `<OS temp dir>/oracle-state/config.json` when `CLAUDE_PLUGIN_DATA` is unset — the exact same base-dir resolution the hook uses for its per-turn state, so the location is environment-independent: no cwd, no HOME involved).

Schema — every key optional; **zero config reproduces v1 behavior exactly**:

| Key | Type | Default | Effect |
|---|---|---|---|
| `stop_hook` | bool | `true` | `false` disables the Stop-hook safety net entirely |
| `doctrine` | bool | `true` | `false` disables the SessionStart doctrine injection |
| `markers.add` | list of strings | `[]` | extra uncertainty markers (lowercased, whitespace-normalized before matching, same as built-ins) |
| `markers.remove` | list of strings | `[]` | built-in markers to drop (case-insensitive) |
| `state_dir` | string | unset | relocates the per-turn block-state files (config file location itself never moves) |

Worked example — quieter hook for a repo where "I'm confused" shows up in legitimate prose, plus one project-specific marker and state on a RAM disk:

```json
{
  "markers": {
    "add": ["going in circles"],
    "remove": ["i'm confused"]
  },
  "state_dir": "R:/oracle-state"
}
```

Environment kill-switch: `CC_ORACLE_DISABLE=1` (also `true`/`yes`) silences both hooks — useful in CI.

Failure posture: a malformed file or a wrong-typed key is ignored and defaults apply — configuration can only tune the plugin, never break a session. Note the asymmetry: config trouble leaves the doctrine *on* (defaults win); only an explicit, well-formed `false` turns anything off.

There is no log-verbosity knob: the hook has no logging today, and the config surface only exposes behavior the code actually has.

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

## Requirements & portability

- Claude Code with plugin support.
- **No runtime dependency.** The hook ships as a precompiled binary for every
  supported platform, so there is nothing to install — no Python, no toolchain.

Supported platforms, all committed under `hooks/bin/`:

| | x86-64 | arm64 |
|---|---|---|
| **Windows** | ✅ | ✅ |
| **macOS** | ✅ | ✅ |
| **Linux** | ✅ | ✅ |

`hooks/hooks.json` selects the right binary at run time by trying each in turn;
see [`docs/dispatch.md`](docs/dispatch.md) for why that is safe in both
`cmd.exe` and POSIX shells. If no binary can execute — an unforeseen platform, a
`noexec` mount — the original `hooks/oracle_hook.py` still runs as a fallback,
so the safety net never silently disappears. That path needs Python 3.9+ as
`python` or `python3`.

The implementation commits to a portability floor:

- **Standard library only** — no third-party modules, in either language.
- **Windows / macOS / Linux** — no platform-specific paths or shell assumptions.
- **Fail open, always** — any unexpected input, any error, any panic exits 0
  and stays silent. A hook must never wedge a session.
- **Encoding robustness** — transcripts are decoded with invalid bytes replaced
  (one bad byte never kills detection); emitted JSON is ASCII-escaped so it
  survives any console codepage.

## Development

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

Repo layout:

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
| `tests/test_config.py` | Configuration surface + portability floor |

Further reading under `docs/`:

- [`docs/dispatch.md`](docs/dispatch.md) — how the platform binary is selected, and why it is safe
- [`docs/specs/2026-08-08-go-port-contract.md`](docs/specs/2026-08-08-go-port-contract.md) — the Go port contract and known Python/Go divergences
- [`docs/specs/2026-07-23-oracle-plugin-design.md`](docs/specs/2026-07-23-oracle-plugin-design.md) — design spec
- [`docs/plans/2026-07-23-oracle-plugin.md`](docs/plans/2026-07-23-oracle-plugin.md) — implementation plan
- [`docs/research/2026-07-23-anthropic-docs-report.md`](docs/research/2026-07-23-anthropic-docs-report.md) — official-docs research report

## License

MIT — see [`LICENSE`](LICENSE).
