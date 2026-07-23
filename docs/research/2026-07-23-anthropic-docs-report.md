# Research report: Claude Code plugin development (official docs)

> Produced 2026-07-23 by a docs-research subagent from official Claude Code documentation (code.claude.com). Reference for implementing the oracle plugin. Items marked UNCONFIRMED reflect documentation gaps.

## 1. Plugin manifest schema

Location: `.claude-plugin/plugin.json`. Source: https://code.claude.com/docs/en/plugins-reference.md

Required: `name` (kebab-case), `description`. Optional relevant fields: `version` (semver; if omitted, git commit SHA used), `author.name`, `author.email`, `homepage`, `repository`, `license` (SPDX), `keywords` (array), `hooks` (path/inline), `agents` (custom paths).

## 2. Plugin directory structure

`.claude-plugin/` contains ONLY `plugin.json` (and `marketplace.json` for marketplace repos). `agents/`, `hooks/`, `skills/`, `commands/` live at plugin ROOT, not inside `.claude-plugin/`. All manifest paths relative, starting `./`.

## 3. Environment variables

- `${CLAUDE_PLUGIN_ROOT}` — absolute path to plugin install dir. Available in hook commands, MCP/LSP configs, skill/agent content.
- `${CLAUDE_PLUGIN_DATA}` — `~/.claude/plugins/data/{id}/`, persists across updates. Use for state.
- `${CLAUDE_PROJECT_DIR}` — project root.

Windows: prefer exec form (`"args": [...]`) or wrap variables in double quotes in shell form.

## 4. Subagent definition frontmatter

File: `agents/<name>.md`. Fields:

| Field | Notes |
|---|---|
| `name` | required, unique |
| `description` | required; DRIVES proactive delegation — "Use when...", "Use PROACTIVELY...". 1,536 char cap (combined with `when_to_use`) |
| `model` | `sonnet`, `opus`, `haiku`, `fable`, full ID, or `inherit` (default). UNCONFIRMED: exhaustive alias list |
| `tools` | space/comma-separated string or YAML list; inherits session tools if omitted |
| `disallowedTools` | removes from inherited pool |
| `effort` | low/medium/high/xhigh/max |
| `maxTurns`, `background`, `isolation`, `color`, `permissionMode`, `skills`, `memory` | optional; plugin agents cannot use `hooks` |

Model resolution order: `CLAUDE_CODE_SUBAGENT_MODEL` env → per-invocation `model` param → definition's `model` field → main session model. **Unavailable models: checked against `availableModels` allowlist; falls back to inherited model** (exact error messaging UNCONFIRMED).

## 5. Description writing for proactive delegation

Lead with key use case; include trigger phrases ("Use when...", "Use PROACTIVELY...", "Use immediately after..."); state when to auto-invoke; be specific. Generic descriptions ("A tool for X") trigger poorly.

## 6. Hook contracts

Common stdin fields (all events): `session_id`, `prompt_id`, `transcript_path`, `cwd`, `permission_mode`, `hook_event_name`.

### Stop hook
- Extra stdin field: `stop_hook_active` (true = already blocked this turn — exit 0 to avoid loops).
- Output: exit 0 + stdout JSON `{"decision": "block", "reason": "..."}` to block; exit 0 with no output = allow. Exit 2: stderr fed to Claude as feedback. Other exit codes: non-blocking error.
- Built-in cap: Claude Code blocks max 8 times per turn, then overrides the hook.

### SessionStart hook
- stdin `source`: `startup` | `resume` | `clear` | `compact` | `fork` (also the matcher values).
- Context injection output contract:

```json
{
  "hookSpecificOutput": {
    "hookEventName": "SessionStart",
    "additionalContext": "Text injected into Claude's context"
  }
}
```

### Transcript JSONL
One JSON object per line. Assistant messages: `"role": "assistant"` with `content` array of `{"type": "text", "text": ...}` and `{"type": "tool_use", "id", "name", "input"}` blocks. Task dispatches: `"name": "Task"`, `input.subagent_type`.

## 7. hooks.json syntax

```json
{
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "python3 \"${CLAUDE_PLUGIN_ROOT}/hooks/x.py\""}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "..."}]}
    ]
  }
}
```

Exec form alternative (Windows-safe, no shell): `{"type": "command", "args": ["python", "${CLAUDE_PLUGIN_ROOT}/hooks/x.py", "stop"]}`. Matchers are unanchored regex (tool names for Pre/PostToolUse; source values for SessionStart). Omitted matcher = all.

## 8. Marketplace & installation

`marketplace.json` (in `.claude-plugin/`): `name`, `owner{name,email}`, `plugins[{name, source: "./", description, ...}]`.

User install flow for GitHub-hosted repo `owner/repo`:

```
/plugin marketplace add owner/repo
/plugin install <plugin-name>@<marketplace-name>
```

CLI: `claude plugin install <plugin>@<marketplace>`. Update: `/plugin update`, `/plugin marketplace update`. Version resolution: plugin.json `version` → marketplace entry `version` → git SHA. If `version` set, MUST bump for users to receive updates.

## 9. Subagent system prompt structure (docs pattern)

Role statement → `## Your Role` bullets → `## Process` numbered steps → `## Output Format` explicit structure → "Be concise and actionable." Clear direct instructions; XML tags and examples per general Anthropic prompt guidance.

## 10. Platform portability

- Windows shell-form hooks: Git Bash can mangle backslashes — use forward slashes, quote `${CLAUDE_PLUGIN_ROOT}`, or use exec form `args`.
- `python3` on Windows Git Bash often absent; `python` on modern Ubuntu often absent. Fallback chaining or exec form choice needed.

## 11. Unconfirmed

- Exhaustive model alias list beyond haiku/sonnet/opus/fable.
- Exact error surface when a model alias is unavailable (documented as fallback to inherited model).
- `--plugin-dir` CLI flag for loading a plugin from a local dir without marketplace install: NOT confirmed.
- plugin.json inline `hooks` vs `hooks/hooks.json` precedence when both exist.

## Sources

- https://code.claude.com/docs/en/plugins.md
- https://code.claude.com/docs/en/plugins-reference.md
- https://code.claude.com/docs/en/sub-agents.md
- https://code.claude.com/docs/en/hooks-guide.md
- https://code.claude.com/docs/en/hooks.md
- https://code.claude.com/docs/en/skills.md
- https://code.claude.com/docs/en/plugin-marketplaces.md
