# Oracle Plugin — Design Spec

**Date:** 2026-07-23
**Status:** Approved design, pre-implementation
**Repo:** `C:\proga\claude-oracle` (standalone Claude Code plugin)

## Problem

Weaker models (haiku, sonnet) — and sometimes strong ones — hit moments of genuine uncertainty: stuck, confused, low confidence. Flailing solo pollutes their own context, burns tokens, and produces worse code. Models are now reliably good at *stating* uncertainty. That signal should route to a brief consultation with the best available model instead of solo attempts.

## Goal

A standalone Claude Code plugin, usable in any session on any tier, that:

1. Ships an **oracle subagent** running the best available model.
2. Instructs the main model to **stop and consult** the oracle at the first sign of being unsure/stuck, with a **full problem brief**.
3. Backs the instruction with a **Stop-hook safety net** that catches turns ending in stated uncertainty without a consult.

Success criteria: a haiku/sonnet session, when stuck, dispatches the oracle with a complete brief, receives a diagnosis + plan, and implements it — instead of looping on failed attempts. Strong-model sessions can use the same path for a fresh-context second opinion.

## Non-goals (YAGNI)

- No direct API-key sidecar calls; everything goes through the native Agent tool.
- No MCP server.
- No oracle write access — advisor only, never fixer.
- No per-project configuration surface.
- No persistent consultation log; each consult is fresh.
- No caller-transcript access for the oracle; the brief is the interface.

## Components

### 1. Oracle subagent — `agents/oracle.md`

- **Model:** `fable` alias in frontmatter. Aliases only, never hardcoded model IDs — aliases resolve per provider (Anthropic API, Bedrock, Vertex). Per official docs, an alias unavailable on the caller's plan/provider silently falls back to the *inherited* (caller's) model — no error reaches the caller. The standing instruction therefore carries only a generic safety line: if the dispatch *errors* for any reason, retry the same Agent call once with `model: opus`. The silent-downgrade caveat is documented in the README.
- **Tools:** Read, Grep, Glob only. No Edit, no Write, and no Bash — a Bash grant cannot be technically restricted to read-only use, and the read-only guarantee is architectural, not prose. Code/history questions are answerable from files via Read/Grep/Glob.
- **Description** (drives proactive dispatch — agent descriptions are always visible to the main model): "Use PROACTIVELY when unsure, stuck, confused, going in circles, or wanting a second opinion — BEFORE attempting solo."
- **System prompt:**
  - Role: senior consultant with zero shared context. Investigate the codebase directly (Read/Grep/Glob) rather than trusting the brief blindly.
  - Output: diagnosis, concrete step-by-step plan, pitfalls to avoid. Brief — this is a consult, not a takeover.
  - Never edit files; never attempt the task itself.
  - **Brief contract enforcement:** if the incoming brief is missing goal, attempts-so-far, or the verbatim error, the first line of output requests the missing fields instead of guessing.

### 2. Standing instruction — minimal SessionStart context

A SessionStart hook injects a deliberately tiny doctrine (3–6 lines, zero project state — a globally-enabled plugin fires in every session, so payload stays minimal):

- Uncertainty is a signal, not a failure. When unsure/stuck: do not burn context flailing — consult the oracle agent first.
- **Full consultation brief is mandatory.** Required fields:
  - **Goal** — what the task ultimately wants
  - **Problem** — the exact blocker, errors quoted verbatim
  - **Tried** — attempts made and why each failed
  - **Context** — relevant files/paths, key constraints (versions, platform, project rules)
  - **Question** — the specific ask, not "help"
- Rationale stated inline: the oracle has zero shared context; a thin brief wastes the consult and forces a second round-trip. One complete brief beats two vague ones.
- Fallback rule: oracle dispatch errors for any reason → retry the same call once with `model: opus` override.
- Applies to all tiers: strong models may consult for a fresh-context second opinion.

### 3. Stop-hook safety net — `hooks/oracle_hook.py`

Python stdlib script serving BOTH hook events via subcommand (`stop` | `session-start`) — one file, one interpreter dependency.

- **Input:** Stop-hook JSON on stdin (includes `transcript_path`, `session_id`, `stop_hook_active`).
- **Detection:** parse the transcript JSONL; take the final assistant message text; match against a conservative, built-in marker list (e.g. "I'm not sure", "I am not sure", "I'm stuck", "can't figure out", "not certain why", "I'm confused"). Conservative by design: a false positive (annoying block) is worse than a miss, because the instruction path is primary — the hook only catches forgetting.
- **Assistant-text-only scanning:** only the final *assistant* message is ever scanned. User messages, tool results, and hook-injected context are never matched — a user typing "I'm not sure what I want here" must not trigger the hook. The transcript walk must filter strictly by assistant role and by text content blocks (not tool_use blocks).
- **Quoted/fenced-text exemption:** markers inside code fences (```…```), inline backtick spans, and double-quoted strings are stripped before matching — an assistant *quoting* an error message or documenting the marker list is not *stating* uncertainty. (Single-quoted spans are NOT stripped: apostrophes in contractions like "I'm" would corrupt matching.)
- **Asking-the-user suppression:** "I'm not sure" often prefaces a question *to the user* ("I'm not sure which option you prefer — A or B?"), which is legitimate turn-ending behavior, not flailing. The hook must NOT block in that case. Rule: if the sentence containing the marker ends in a question mark, or the message's final sentence is a question, treat the turn as a user-question and exit 0. Marker-in-question always wins over marker-matched.
- **Suppression:** if an oracle Task/Agent dispatch already occurred this turn, exit 0 silently. The match rule is exact: `subagent_type == "oracle"` or ends with `":oracle"` (plugin-scoped form) — never a bare substring test, which an unrelated agent name like `my-oracledb-helper` would falsely satisfy.
- **Action on match:** emit `{"decision": "block", "reason": <nudge>}` where the nudge restates the full-brief fields (Goal / Problem with errors verbatim / Tried / Context / Question) and tells the model to dispatch the oracle then implement its plan.
- **Loop guards:**
  - Respect `stop_hook_active` — never block a stop that resulted from a prior block.
  - Max one block per turn, enforced per-turn: state file keyed by session id records the `prompt_id` already blocked (Stop-hook stdin carries `prompt_id`); a second stop in the same turn is waved through, a new turn is eligible again. No wall-clock cooldown — a time window would silently swallow a genuinely distinct stuck turn arriving shortly after the first.
  - State lives under `${CLAUDE_PLUGIN_DATA}` (documented plugin state dir), falling back to the OS temp dir only when unset.
  - Claude Code itself caps Stop blocks at 8 per turn — a platform backstop beneath ours.
- **Failure posture:** any parse error or unexpected shape → exit 0 (never wedge a session).

### 4. Plugin manifest

- Declares the oracle agent, the SessionStart hook (doctrine injection), and the Stop hook.
- No statusline, no slash commands in v1.

## Data flow

1. Main model (any tier) hits uncertainty → recognizes signal (doctrine + agent description).
2. Composes full brief (goal/problem/tried/context/question) → dispatches `oracle` agent.
3. Oracle (fable; opus on fallback) reads code read-only, returns diagnosis + plan + pitfalls, or requests missing brief fields.
4. Main model implements the plan in its own context.
5. If the model instead ends its turn stating uncertainty with no consult → Stop hook blocks once with a nudge → model consults → proceeds.

## Error handling

- Oracle model alias unavailable on plan/provider → platform silently falls back to the caller's inherited model (documented behavior; caveat surfaced in README). Dispatch *errors* → caller retries once with `opus` override (instruction-driven, no code).
- Thin brief → oracle requests missing fields (prompt-driven).
- Hook parse failures → silent exit 0.
- Hook loop risk → `stop_hook_active` check + per-`prompt_id` once-per-turn guard + platform 8-block cap.

## Testing

- Unit tests (pytest) for the Stop hook: marker matching (positive/negative cases, including markers inside code fences / inline code / double quotes as required negatives), suppression when oracle was consulted (exact-name rule incl. plugin-scoped, with substring-lookalike negative), `stop_hook_active` respected, malformed stdin and corrupted transcript → exit 0, per-`prompt_id` once-per-turn guard.
- Manual integration: haiku session with a seeded stuck-task; verify dispatch, brief completeness, and hook nudge when the consult is skipped.

## Trade-offs decided

- **Stop-hook block over PostToolUse/UserPromptSubmit nudge:** fires once at a natural boundary, cannot spam mid-turn; UserPromptSubmit cannot see the model's own uncertainty.
- **Advisor-only oracle:** keeps the main context authoritative, avoids two-writers conflicts, matches "brief aid" intent.
- **Aliases over model IDs:** portability across providers and future model families.
- **Codebase-only reading for the oracle:** past-consult logs and caller-transcript access were considered and rejected (user decision) — the brief is the interface.
