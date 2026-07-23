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

- **Model:** `fable` alias in frontmatter. Aliases only, never hardcoded model IDs — aliases resolve per provider (Anthropic API, Bedrock, Vertex). Fallback: the standing instruction tells the caller that if dispatch fails with a model-unavailable error, retry the Agent call with `model: opus` as a parameter override.
- **Tools:** Read, Grep, Glob, Bash (read-only use). No Edit, no Write.
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
- Fallback rule: oracle dispatch fails with model-unavailable → retry with `model: opus` override.
- Applies to all tiers: strong models may consult for a fresh-context second opinion.

### 3. Stop-hook safety net — `hooks/stop_oracle_nudge.py`

Python stdlib script wired as a Stop hook.

- **Input:** Stop-hook JSON on stdin (includes `transcript_path`, `session_id`, `stop_hook_active`).
- **Detection:** parse the transcript JSONL; take the final assistant message text; match against a conservative, built-in marker list (e.g. "I'm not sure", "I am not sure", "I'm stuck", "can't figure out", "not certain why", "I'm confused"). Conservative by design: a false positive (annoying block) is worse than a miss, because the instruction path is primary — the hook only catches forgetting.
- **Assistant-text-only scanning:** only the final *assistant* message is ever scanned. User messages, tool results, and hook-injected context are never matched — a user typing "I'm not sure what I want here" must not trigger the hook. The transcript walk must filter strictly by assistant role and by text content blocks (not tool_use blocks).
- **Asking-the-user suppression:** "I'm not sure" often prefaces a question *to the user* ("I'm not sure which option you prefer — A or B?"), which is legitimate turn-ending behavior, not flailing. The hook must NOT block in that case. Rule: if the sentence containing the marker ends in a question mark, or the message's final sentence is a question, treat the turn as a user-question and exit 0. Marker-in-question always wins over marker-matched.
- **Suppression:** if an oracle Task/Agent dispatch already occurred this turn, exit 0 silently.
- **Action on match:** emit `{"decision": "block", "reason": "You stated uncertainty this turn. Consult the oracle agent with a full brief (goal / problem / tried / context / question) before finishing."}`.
- **Loop guards:**
  - Respect `stop_hook_active` — never block a stop that resulted from a prior block.
  - Max one block per turn; cooldown state kept in a small state file under the plugin data directory, keyed by session id.
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

- Oracle model unavailable → caller retries with `opus` override (instruction-driven, no code).
- Thin brief → oracle requests missing fields (prompt-driven).
- Hook parse failures → silent exit 0.
- Hook loop risk → `stop_hook_active` check + once-per-turn state guard.

## Testing

- Unit tests (pytest) for the Stop hook: marker matching (positive/negative cases), suppression when oracle was consulted, `stop_hook_active` respected, malformed transcript → exit 0, once-per-turn guard.
- Manual integration: haiku session with a seeded stuck-task; verify dispatch, brief completeness, and hook nudge when the consult is skipped.

## Trade-offs decided

- **Stop-hook block over PostToolUse/UserPromptSubmit nudge:** fires once at a natural boundary, cannot spam mid-turn; UserPromptSubmit cannot see the model's own uncertainty.
- **Advisor-only oracle:** keeps the main context authoritative, avoids two-writers conflicts, matches "brief aid" intent.
- **Aliases over model IDs:** portability across providers and future model families.
- **Codebase-only reading for the oracle:** past-consult logs and caller-transcript access were considered and rejected (user decision) — the brief is the interface.
