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

Optional, file-based, fail-open. The hook looks for `oracle.json` in two places and merges them per-key:

1. `~/.claude/oracle.json` — user level
2. `<project>/.claude/oracle.json` — project level (wins on conflict)

All keys are optional:

```json
{
  "stop_hook": true,
  "doctrine": true,
  "markers": {
    "add": ["going in circles"],
    "remove": ["i'm confused"]
  }
}
```

| Key | Type | Default | Effect |
|---|---|---|---|
| `stop_hook` | bool | `true` | `false` disables the Stop-hook safety net entirely |
| `doctrine` | bool | `true` | `false` disables the SessionStart doctrine injection |
| `markers.add` | list of strings | `[]` | extra uncertainty markers (lowercased, whitespace-normalized before matching) |
| `markers.remove` | list of strings | `[]` | built-in markers to drop (case-insensitive) |

Environment kill-switch: `CC_ORACLE_DISABLE=1` (also `true`/`yes`) silences both hooks — useful in CI.

Failure posture: a malformed file or a wrong-typed key is ignored and defaults apply — configuration can only tune the plugin, never break a session. Note the asymmetry: config trouble leaves the doctrine *on* (defaults win), while only an explicit, well-formed `false` turns anything off.

## Requirements & portability floor

- Claude Code with plugin support.
- Python **3.9+** reachable as `python` or `python3` (`hooks/hooks.json` tries `python` first, falling back to `python3`).

The hook script commits to a portability floor:

- **Stdlib only** — no third-party imports, ever.
- **Windows / macOS / Linux** — no platform-specific paths or shell assumptions.
- **Below-floor grace** — on a Python older than 3.9 the hook exits 0 silently instead of wedging the session.
- **Encoding robustness** — transcripts are read with `errors="replace"` (one bad byte never kills detection); emitted JSON is ASCII-escaped so it survives any console codepage.

## Development

```
python -m pytest -q
```

Repo layout:

| Path | Purpose |
|---|---|
| `.claude-plugin/plugin.json` | Plugin manifest |
| `.claude-plugin/marketplace.json` | Marketplace listing (lets this repo double as a marketplace) |
| `agents/oracle.md` | The oracle subagent definition |
| `hooks/oracle_hook.py` | Single stdlib script serving both `session-start` and `stop` subcommands |
| `hooks/hooks.json` | SessionStart + Stop hook wiring |
| `tests/test_detection.py` | Marker + question/quote-suppression logic |
| `tests/test_transcript.py` | Transcript parsing / turn analysis |
| `tests/test_stop_entry.py` | End-to-end stdin→stdout behavior of the hook entrypoints |
| `tests/test_config.py` | v1.1 configuration surface + portability floor |

Further reading under `docs/`:

- [`docs/specs/2026-07-23-oracle-plugin-design.md`](docs/specs/2026-07-23-oracle-plugin-design.md) — design spec
- [`docs/plans/2026-07-23-oracle-plugin.md`](docs/plans/2026-07-23-oracle-plugin.md) — implementation plan
- [`docs/research/2026-07-23-anthropic-docs-report.md`](docs/research/2026-07-23-anthropic-docs-report.md) — official-docs research report

## License

MIT — see [`LICENSE`](LICENSE).
