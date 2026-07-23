# Oracle Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A standalone Claude Code plugin (`oracle`) that lets any-tier models escalate to a best-model read-only consultant subagent when unsure/stuck, with a SessionStart doctrine and a conservative Stop-hook safety net.

**Architecture:** One Python stdlib hook script (`hooks/oracle_hook.py`) serves both hook events via a subcommand (`session-start` prints doctrine; `stop` scans the transcript and may emit a block decision). One agent definition (`agents/oracle.md`) with `model: fable` alias. Plugin manifest + hooks.json wire it together. Everything else is tests, README, and repo hygiene.

**Tech Stack:** Python 3 stdlib only (no deps), pytest for tests, Claude Code plugin format (`.claude-plugin/plugin.json`, `hooks/hooks.json`, `agents/*.md`).

## Global Constraints

- Spec of record: `docs/specs/2026-07-23-oracle-plugin-design.md`. Read it before starting any task.
- Python stdlib ONLY in `hooks/oracle_hook.py`. Must run on Python 3.8+.
- Model ALIASES only (`fable`, `opus`) — never hardcoded model IDs like `claude-fable-5`.
- Oracle agent is read-only: tools `Read, Grep, Glob, Bash` — NO Edit/Write.
- Hook failure posture: any unexpected input/parse error → exit 0 silently. Never wedge a session.
- Hook scans ASSISTANT text only — user messages, tool results, tool_use blocks never matched.
- Hook commands in hooks.json use `${CLAUDE_PLUGIN_ROOT}` and forward slashes.
- Official-docs research report lives at `docs/research/2026-07-23-anthropic-docs-report.md`. Tasks marked **[research-informed]** MUST read it first. Where the report contradicts this plan's schema details, THE REPORT WINS — adjust and note the change in the commit message.
- Commit after every task (message format shown per task). All commits end with:

  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`

## File Structure

```
claude-oracle/
├── .claude-plugin/
│   ├── plugin.json          # plugin manifest
│   └── marketplace.json     # lets the GitHub repo double as a marketplace
├── agents/
│   └── oracle.md            # the oracle subagent (research-informed prompt)
├── hooks/
│   ├── hooks.json           # SessionStart + Stop wiring
│   └── oracle_hook.py       # single stdlib script, both events
├── tests/
│   ├── test_detection.py    # marker + question-suppression logic
│   ├── test_transcript.py   # transcript parsing / turn analysis
│   └── test_stop_entry.py   # end-to-end stdin→stdout behavior of the stop entrypoint
├── docs/
│   ├── specs/2026-07-23-oracle-plugin-design.md   # exists
│   └── plans/2026-07-23-oracle-plugin.md          # this file
├── .gitignore
├── LICENSE                  # MIT
├── pytest.ini
└── README.md
```

---

### Task 1: Repo scaffold + plugin manifests

**Files:**
- Create: `.gitignore`, `LICENSE`, `pytest.ini`, `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json`

**Interfaces:**
- Produces: plugin name `oracle` (later tasks reference agent as `oracle` and hook paths via `${CLAUDE_PLUGIN_ROOT}`).

- [ ] **Step 1: Write `.gitignore`**

```gitignore
__pycache__/
*.pyc
.pytest_cache/
```

- [ ] **Step 2: Write `LICENSE`** — standard MIT license text, year 2026, holder `Techn0Ninja27`.

- [ ] **Step 3: Write `pytest.ini`**

```ini
[pytest]
testpaths = tests
```

- [ ] **Step 4: Write `.claude-plugin/plugin.json`**

```json
{
  "name": "oracle",
  "version": "0.1.0",
  "description": "When unsure or stuck, consult a best-model read-only oracle instead of flailing solo.",
  "author": {
    "name": "Techn0Ninja27"
  },
  "license": "MIT",
  "repository": "https://github.com/Techn0Ninja27/claude-oracle",
  "keywords": ["oracle", "escalation", "consult", "subagent", "uncertainty"]
}
```

- [ ] **Step 5: Write `.claude-plugin/marketplace.json`**

```json
{
  "name": "claude-oracle",
  "owner": {
    "name": "Techn0Ninja27"
  },
  "plugins": [
    {
      "name": "oracle",
      "source": "./",
      "description": "When unsure or stuck, consult a best-model read-only oracle instead of flailing solo."
    }
  ]
}
```

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore: scaffold plugin manifests, license, pytest config"
```

---

### Task 2: Uncertainty detection logic (TDD)

**Files:**
- Create: `hooks/oracle_hook.py` (detection functions only)
- Test: `tests/test_detection.py`

**Interfaces:**
- Produces: `MARKERS: tuple[str, ...]`, `marker_hit(text: str) -> bool`, `is_question_turn(text: str) -> bool`, `should_nudge(text: str) -> bool`. Task 4 calls `should_nudge`.

- [ ] **Step 1: Write the failing tests** — `tests/test_detection.py`:

```python
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "hooks"))

from oracle_hook import marker_hit, is_question_turn, should_nudge


# --- marker_hit ---

def test_marker_hit_im_not_sure():
    assert marker_hit("I'm not sure why this test fails.")

def test_marker_hit_case_insensitive():
    assert marker_hit("I AM STUCK on this segfault.")

def test_marker_hit_cant_figure_out():
    assert marker_hit("I can't figure out where the config is loaded.")

def test_marker_hit_negative_plain_text():
    assert not marker_hit("All tests pass. Done.")

def test_marker_hit_negative_near_miss():
    # "sure" alone, "not sure" inside other words must not fire
    assert not marker_hit("Make sure the tests pass. This is not surprising.")


# --- is_question_turn (asking-the-user suppression) ---

def test_question_final_sentence():
    assert is_question_turn("I'm not sure which option fits. Do you prefer A or B?")

def test_question_marker_sentence_itself():
    assert is_question_turn("I'm not sure which one you want — A or B?  Meanwhile I'll wait.")

def test_not_question_plain_statement():
    assert not is_question_turn("I'm stuck. The build fails with a linker error.")


# --- should_nudge (composition) ---

def test_nudge_on_stuck_statement():
    assert should_nudge("I'm stuck. The mock never gets called and I can't figure out why.")

def test_no_nudge_when_asking_user():
    assert not should_nudge("I'm not sure what scope you want here — full rewrite or patch?")

def test_no_nudge_without_marker():
    assert not should_nudge("Refactored the parser; all 34 tests pass.")

def test_no_nudge_empty_text():
    assert not should_nudge("")
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `python -m pytest tests/test_detection.py -v`
Expected: FAIL / import error ("No module named 'oracle_hook'").

- [ ] **Step 3: Write minimal implementation** — create `hooks/oracle_hook.py`:

```python
#!/usr/bin/env python3
"""claude-oracle hooks: SessionStart doctrine injection + Stop-hook safety net.

Stdlib only. Failure posture: any unexpected input -> exit 0, never wedge a session.
"""
import re

# Conservative by design: a false positive (annoying block) is worse than a miss.
# The instruction path is primary; this hook only catches forgetting.
MARKERS = (
    "i'm not sure",
    "i am not sure",
    "i'm unsure",
    "i am unsure",
    "i'm stuck",
    "i am stuck",
    "can't figure out",
    "cannot figure out",
    "i'm confused",
    "i am confused",
    "not certain why",
    "unsure how to proceed",
)

_SENTENCE_SPLIT = re.compile(r"(?<=[.!?])\s+")


def _sentences(text):
    return [s for s in _SENTENCE_SPLIT.split(text.strip()) if s]


def marker_hit(text):
    low = text.lower()
    return any(m in low for m in MARKERS)


def is_question_turn(text):
    """True when the turn is (or ends with) a question to the user.

    Marker-in-question always wins over marker-matched: 'I'm not sure which
    you prefer — A or B?' is legitimate turn-ending behavior, not flailing.
    """
    sents = _sentences(text)
    if not sents:
        return False
    if sents[-1].rstrip().endswith("?"):
        return True
    for s in sents:
        if s.rstrip().endswith("?") and marker_hit(s):
            return True
    return False


def should_nudge(text):
    if not text:
        return False
    return marker_hit(text) and not is_question_turn(text)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `python -m pytest tests/test_detection.py -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add hooks/oracle_hook.py tests/test_detection.py
git commit -m "feat: uncertainty detection with question-to-user suppression"
```

---

### Task 3: Transcript analysis (TDD)

**Files:**
- Modify: `hooks/oracle_hook.py` (append functions)
- Test: `tests/test_transcript.py`

**Interfaces:**
- Consumes: nothing from Task 2 (independent functions in same file).
- Produces: `load_entries(path: str) -> list[dict]`, `last_assistant_text(entries: list[dict]) -> str`, `oracle_consulted_this_turn(entries: list[dict]) -> bool`. Task 4 calls all three.

Transcript format (Claude Code session JSONL): one JSON object per line; assistant lines look like `{"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": "..."}, {"type": "tool_use", "name": "Task", "input": {"subagent_type": "oracle", ...}}]}}`; user lines have `"type": "user"` with content either a string or a list of blocks (`text` blocks for real prompts, `tool_result` blocks for tool returns). Verify block shapes against the research report and adjust if it contradicts this.

- [ ] **Step 1: Write the failing tests** — `tests/test_transcript.py`:

```python
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "hooks"))

from oracle_hook import load_entries, last_assistant_text, oracle_consulted_this_turn


def _assistant(text=None, tool=None):
    content = []
    if text is not None:
        content.append({"type": "text", "text": text})
    if tool is not None:
        content.append({"type": "tool_use", "name": tool[0], "input": tool[1]})
    return {"type": "assistant", "message": {"role": "assistant", "content": content}}


def _user_prompt(text):
    return {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": text}]}}


def _user_tool_result():
    return {"type": "user", "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "x", "content": "ok"}]}}


# --- load_entries ---

def test_load_entries_skips_malformed_lines(tmp_path):
    p = tmp_path / "t.jsonl"
    p.write_text('{"type": "user"}\nnot json at all\n{"type": "assistant"}\n', encoding="utf-8")
    entries = load_entries(str(p))
    assert [e["type"] for e in entries] == ["user", "assistant"]


def test_load_entries_missing_file_returns_empty():
    assert load_entries("Z:/definitely/not/here.jsonl") == []


# --- last_assistant_text ---

def test_last_assistant_text_takes_final_text_message():
    entries = [_assistant(text="first"), _user_tool_result(), _assistant(text="I'm stuck on the failing mock.")]
    assert last_assistant_text(entries) == "I'm stuck on the failing mock."


def test_last_assistant_text_skips_tool_use_only_entries():
    entries = [_assistant(text="real text"), _assistant(tool=("Bash", {"command": "ls"}))]
    assert last_assistant_text(entries) == "real text"


def test_last_assistant_text_ignores_user_messages():
    # User saying "I'm not sure" must never be what gets scanned.
    entries = [_assistant(text="done."), _user_prompt("I'm not sure what I want here")]
    assert last_assistant_text(entries) == "done."


def test_last_assistant_text_empty_transcript():
    assert last_assistant_text([]) == ""


# --- oracle_consulted_this_turn ---

def test_consulted_true_when_oracle_task_after_last_user_prompt():
    entries = [
        _user_prompt("fix the bug"),
        _assistant(text="consulting", tool=("Task", {"subagent_type": "oracle", "prompt": "brief"})),
        _user_tool_result(),
        _assistant(text="implementing the plan"),
    ]
    assert oracle_consulted_this_turn(entries)


def test_consulted_matches_plugin_scoped_name():
    entries = [
        _user_prompt("fix the bug"),
        _assistant(tool=("Task", {"subagent_type": "oracle:oracle"})),
    ]
    assert oracle_consulted_this_turn(entries)


def test_consulted_false_when_consult_was_previous_turn():
    entries = [
        _user_prompt("first ask"),
        _assistant(tool=("Task", {"subagent_type": "oracle"})),
        _user_prompt("second ask"),
        _assistant(text="I'm stuck."),
    ]
    assert not oracle_consulted_this_turn(entries)


def test_consulted_false_for_other_agents():
    entries = [
        _user_prompt("fix"),
        _assistant(tool=("Task", {"subagent_type": "general-purpose"})),
    ]
    assert not oracle_consulted_this_turn(entries)


def test_tool_results_do_not_count_as_user_prompts():
    # tool_result user entries must not reset the turn boundary
    entries = [
        _user_prompt("fix"),
        _assistant(tool=("Task", {"subagent_type": "oracle"})),
        _user_tool_result(),
        _assistant(text="still this turn"),
    ]
    assert oracle_consulted_this_turn(entries)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `python -m pytest tests/test_transcript.py -v`
Expected: FAIL with ImportError (names not defined).

- [ ] **Step 3: Append implementation** to `hooks/oracle_hook.py`:

```python
import json
import os


def load_entries(path):
    entries = []
    try:
        with open(path, encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    obj = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if isinstance(obj, dict):
                    entries.append(obj)
    except OSError:
        return []
    return entries


def _content_blocks(entry):
    msg = entry.get("message") or {}
    content = msg.get("content")
    if isinstance(content, list):
        return [b for b in content if isinstance(b, dict)]
    return []


def last_assistant_text(entries):
    """Text of the final assistant message that has text blocks.

    Only assistant entries are scanned — user messages, tool results and
    hook-injected context must never be matched (spec: assistant-text-only).
    """
    for entry in reversed(entries):
        if entry.get("type") != "assistant":
            continue
        texts = [b.get("text", "") for b in _content_blocks(entry) if b.get("type") == "text"]
        joined = "\n".join(t for t in texts if t).strip()
        if joined:
            return joined
    return ""


def _is_real_user_prompt(entry):
    if entry.get("type") != "user":
        return False
    msg = entry.get("message") or {}
    content = msg.get("content")
    if isinstance(content, str):
        return bool(content.strip())
    blocks = _content_blocks(entry)
    has_text = any(b.get("type") == "text" for b in blocks)
    has_tool_result = any(b.get("type") == "tool_result" for b in blocks)
    return has_text and not has_tool_result


def oracle_consulted_this_turn(entries):
    start = 0
    for i, entry in enumerate(entries):
        if _is_real_user_prompt(entry):
            start = i
    for entry in entries[start:]:
        if entry.get("type") != "assistant":
            continue
        for b in _content_blocks(entry):
            if b.get("type") == "tool_use" and b.get("name") == "Task":
                subagent = str((b.get("input") or {}).get("subagent_type", "")).lower()
                if "oracle" in subagent:
                    return True
    return False
```

(Place the `import json, os` merge at the top of the file with the existing imports — one import block, no duplicates.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `python -m pytest tests/test_transcript.py -v`
Expected: all PASS. Also run `python -m pytest -q` — full suite green.

- [ ] **Step 5: Commit**

```bash
git add hooks/oracle_hook.py tests/test_transcript.py
git commit -m "feat: transcript parsing - assistant-only scan, turn-scoped oracle detection"
```

---

### Task 4: Hook entrypoints + wiring (TDD) **[research-informed]**

**Files:**
- Modify: `hooks/oracle_hook.py` (cooldown, `run_stop`, `run_session_start`, `main`)
- Create: `hooks/hooks.json`
- Test: `tests/test_stop_entry.py`

**Interfaces:**
- Consumes: `should_nudge`, `load_entries`, `last_assistant_text`, `oracle_consulted_this_turn` (Tasks 2–3).
- Produces: CLI contract `python hooks/oracle_hook.py stop|session-start`; `run_stop(stdin_text: str, now: float|None = None) -> tuple[int, str]` returning (exit_code, stdout).

Contracts confirmed by the research report (`docs/research/2026-07-23-anthropic-docs-report.md`): Stop stdin JSON has `session_id`, `transcript_path`, `stop_hook_active`; blocking output is exit 0 + stdout `{"decision": "block", "reason": "..."}`; Claude Code itself caps Stop blocks at 8 per turn (our cooldown is an extra belt). SessionStart context injection uses the JSON envelope `{"hookSpecificOutput": {"hookEventName": "SessionStart", "additionalContext": "..."}}` on stdout, exit 0.

- [ ] **Step 1: Write the failing tests** — `tests/test_stop_entry.py`:

```python
import json
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "hooks"))

import oracle_hook
from oracle_hook import run_stop, run_session_start, DOCTRINE


def _write_transcript(tmp_path, entries):
    p = tmp_path / "transcript.jsonl"
    p.write_text("\n".join(json.dumps(e) for e in entries) + "\n", encoding="utf-8")
    return str(p)


def _assistant_text(text):
    return {"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": text}]}}


def _user_prompt(text):
    return {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": text}]}}


def _payload(tmp_path, entries, session="sess-1", stop_hook_active=False):
    return json.dumps({
        "session_id": session,
        "transcript_path": _write_transcript(tmp_path, entries),
        "stop_hook_active": stop_hook_active,
    })


def _fresh_session():
    return f"sess-{time.time_ns()}"


def test_blocks_on_stuck_turn(tmp_path):
    payload = _payload(tmp_path, [_user_prompt("fix it"), _assistant_text("I'm stuck. The mock never fires.")], session=_fresh_session())
    code, out = run_stop(payload)
    assert code == 0
    decision = json.loads(out)
    assert decision["decision"] == "block"
    assert "oracle" in decision["reason"].lower()


def test_silent_when_no_marker(tmp_path):
    payload = _payload(tmp_path, [_user_prompt("fix it"), _assistant_text("Done. Tests pass.")], session=_fresh_session())
    assert run_stop(payload) == (0, "")


def test_silent_when_stop_hook_active(tmp_path):
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck.")], session=_fresh_session(), stop_hook_active=True)
    assert run_stop(payload) == (0, "")


def test_silent_when_oracle_already_consulted(tmp_path):
    entries = [
        _user_prompt("fix it"),
        {"type": "assistant", "message": {"role": "assistant", "content": [
            {"type": "tool_use", "name": "Task", "input": {"subagent_type": "oracle"}}]}},
        _assistant_text("I'm stuck even after the consult."),
    ]
    payload = _payload(tmp_path, entries, session=_fresh_session())
    assert run_stop(payload) == (0, "")


def test_silent_on_malformed_stdin():
    assert run_stop("this is not json") == (0, "")


def test_silent_on_missing_transcript():
    payload = json.dumps({"session_id": _fresh_session(), "transcript_path": "Z:/nope.jsonl", "stop_hook_active": False})
    assert run_stop(payload) == (0, "")


def test_cooldown_blocks_only_once(tmp_path):
    session = _fresh_session()
    entries = [_user_prompt("fix"), _assistant_text("I'm stuck. No idea.")]
    payload = _payload(tmp_path, entries, session=session)
    t0 = 1_000_000.0
    code, out = run_stop(payload, now=t0)
    assert json.loads(out)["decision"] == "block"
    # 30s later, same session: suppressed by cooldown
    assert run_stop(payload, now=t0 + 30) == (0, "")
    # after cooldown expiry: allowed again
    code2, out2 = run_stop(payload, now=t0 + oracle_hook.COOLDOWN_SECONDS + 1)
    assert json.loads(out2)["decision"] == "block"


def test_session_start_emits_additional_context_envelope():
    code, out = run_session_start()
    assert code == 0
    envelope = json.loads(out)
    hso = envelope["hookSpecificOutput"]
    assert hso["hookEventName"] == "SessionStart"
    assert hso["additionalContext"] == DOCTRINE
    assert "oracle" in DOCTRINE.lower()
    # doctrine must stay tiny (spec: 3-6 lines, minimal injection surface)
    assert len(DOCTRINE.splitlines()) <= 12
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `python -m pytest tests/test_stop_entry.py -v`
Expected: FAIL with ImportError.

- [ ] **Step 3: Append implementation** to `hooks/oracle_hook.py` (merge imports at top: add `sys`, `tempfile`, `time`):

```python
import sys
import tempfile
import time

COOLDOWN_SECONDS = 120

NUDGE = (
    "You stated uncertainty this turn without consulting the oracle. "
    "Dispatch the `oracle` agent now with a full brief — Goal, Problem (errors verbatim), "
    "Tried (attempts + why each failed), Context (files/constraints), Question (specific ask) — "
    "then implement its plan. If the oracle model is unavailable, retry the dispatch with model \"opus\"."
)

DOCTRINE = """<oracle-plugin>
Uncertainty is a signal, not a failure. The moment you notice you are unsure, stuck, confused, or going in circles: do NOT keep attempting solo and pollute your context — dispatch the `oracle` agent first, then implement its plan yourself.
When summoning the oracle, ALWAYS send a full brief: Goal (what the task ultimately wants), Problem (the exact blocker, errors quoted verbatim), Tried (attempts made + why each failed), Context (relevant files/paths, versions, platform, project rules), Question (the specific ask, not "help"). The oracle shares none of your context — a thin brief wastes the consult and forces a second round-trip.
If the dispatch fails because the model is unavailable, retry the same Agent call with model "opus".
This applies at every tier: strong models may consult the oracle for a fresh-context second opinion.
</oracle-plugin>"""


def _state_path(session_id):
    safe = "".join(c for c in str(session_id) if c.isalnum() or c in "-_") or "unknown"
    d = os.path.join(tempfile.gettempdir(), "claude-oracle")
    os.makedirs(d, exist_ok=True)
    return os.path.join(d, safe + ".json")


def _in_cooldown(session_id, now):
    try:
        with open(_state_path(session_id), encoding="utf-8") as f:
            last = json.load(f).get("last_block", 0)
        return (now - last) < COOLDOWN_SECONDS
    except (OSError, ValueError):
        return False


def _record_block(session_id, now):
    try:
        with open(_state_path(session_id), "w", encoding="utf-8") as f:
            json.dump({"last_block": now}, f)
    except OSError:
        pass


def run_stop(stdin_text, now=None):
    """Returns (exit_code, stdout). Failure posture: (0, "") on anything unexpected."""
    try:
        payload = json.loads(stdin_text)
        if not isinstance(payload, dict):
            return 0, ""
        if payload.get("stop_hook_active"):
            return 0, ""
        transcript_path = payload.get("transcript_path")
        if not transcript_path:
            return 0, ""
        entries = load_entries(transcript_path)
        if not entries:
            return 0, ""
        if not should_nudge(last_assistant_text(entries)):
            return 0, ""
        if oracle_consulted_this_turn(entries):
            return 0, ""
        session_id = payload.get("session_id", "unknown")
        if now is None:
            now = time.time()
        if _in_cooldown(session_id, now):
            return 0, ""
        _record_block(session_id, now)
        return 0, json.dumps({"decision": "block", "reason": NUDGE})
    except Exception:
        return 0, ""


def run_session_start():
    envelope = {
        "hookSpecificOutput": {
            "hookEventName": "SessionStart",
            "additionalContext": DOCTRINE,
        }
    }
    return 0, json.dumps(envelope)


def main(argv):
    mode = argv[1] if len(argv) > 1 else ""
    if mode == "session-start":
        code, out = run_session_start()
    elif mode == "stop":
        code, out = run_stop(sys.stdin.read())
    else:
        return 0
    if out:
        sys.stdout.write(out)
    return code


if __name__ == "__main__":
    sys.exit(main(sys.argv))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `python -m pytest -q`
Expected: full suite PASS.

- [ ] **Step 5: Write `hooks/hooks.json`.** Portability constraint (report §10): `python3` often absent on Windows Git Bash; `python` often absent on modern Ubuntu. Use shell-form fallback chaining — if the first interpreter is missing (exit 127, stdin unconsumed) the second runs; the script itself always exits 0, so double-execution cannot happen on success:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python \"${CLAUDE_PLUGIN_ROOT}/hooks/oracle_hook.py\" session-start || python3 \"${CLAUDE_PLUGIN_ROOT}/hooks/oracle_hook.py\" session-start"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python \"${CLAUDE_PLUGIN_ROOT}/hooks/oracle_hook.py\" stop || python3 \"${CLAUDE_PLUGIN_ROOT}/hooks/oracle_hook.py\" stop"
          }
        ]
      }
    ]
  }
}
```

No matcher on SessionStart — doctrine applies on startup, resume, clear, and compact alike.

- [ ] **Step 6: CLI smoke check**

Run: `echo '{"session_id":"smoke","transcript_path":"/nope","stop_hook_active":false}' | python hooks/oracle_hook.py stop; echo "exit=$?"`
Expected: no output, `exit=0`.

Run: `python hooks/oracle_hook.py session-start | head -2`
Expected: doctrine text starting with `<oracle-plugin>`.

- [ ] **Step 7: Commit**

```bash
git add hooks/oracle_hook.py hooks/hooks.json tests/test_stop_entry.py
git commit -m "feat: stop-hook safety net + session-start doctrine, wired via hooks.json"
```

---

### Task 5: Oracle agent definition **[research-informed]**

**Files:**
- Create: `agents/oracle.md`

**Interfaces:**
- Consumes: plugin name `oracle` (Task 1); doctrine brief fields (Task 4) — the agent prompt's brief contract must use the SAME five field names: Goal, Problem, Tried, Context, Question.

- [ ] **Step 1: Write `agents/oracle.md`.** Baseline below; refine the body per the research report's prompt-engineering guidance (role definition, output format, XML structuring, description-writing for proactive delegation). Frontmatter constraints are hard: `model: fable` alias, read-only tools, no Edit/Write.

```markdown
---
name: oracle
description: Senior consultant running the best available model. Use PROACTIVELY the moment you are unsure, stuck, confused, going in circles, or want a second opinion — BEFORE attempting solo and polluting your context. Send a full brief: Goal, Problem (errors verbatim), Tried, Context (files/constraints), Question. Read-only advisor; it returns a diagnosis and plan for YOU to implement.
tools: Read, Grep, Glob, Bash
model: fable
---

You are the Oracle: a senior consultant summoned by another Claude session that has hit uncertainty. You share NONE of the caller's conversation context — the brief and the codebase are all you have.

## Brief contract

A proper brief contains: **Goal**, **Problem** (errors verbatim), **Tried** (attempts + why each failed), **Context** (files/paths, versions, constraints), **Question** (specific ask).

If the brief is missing Goal, Tried, or the verbatim error: your FIRST line must request the missing fields, then answer as best you can with what you have. Do not guess silently.

## Method

1. Read the relevant code yourself (Read/Grep/Glob; Bash for read-only inspection like `git log`, `ls`). Verify the brief's claims against the code — do not trust them blindly.
2. Diagnose the root cause, not the symptom.
3. Produce a concrete plan the caller can execute.

## Hard rules

- NEVER edit, write, or create files. NEVER run state-changing commands. You are an advisor, not a fixer.
- Do not attempt the task yourself; the caller implements.
- Be brief: this is a consult, not a takeover.

## Output format

Respond with exactly these sections:

**Diagnosis** — root cause in 1-3 sentences, citing file:line evidence you verified.
**Plan** — numbered, concrete steps the caller executes (exact files, functions, commands).
**Pitfalls** — 1-3 traps the caller is likely to hit, only if real.

Your final message is consumed by another model, not a human — no pleasantries, no restating the brief.
```

- [ ] **Step 2: Sanity check** — confirm frontmatter parses (visually: `---` fences, valid YAML, no tabs) and that `model:` uses an alias, not an ID. Note (report §4): an unavailable model alias falls back to the inherited (caller's) model automatically; the doctrine's explicit `opus` retry is the stronger path and stays.

- [ ] **Step 3: Commit**

```bash
git add agents/oracle.md
git commit -m "feat: oracle agent - read-only best-model consultant"
```

---

### Task 6: README + repo polish **[research-informed]**

**Files:**
- Create: `README.md`

**Interfaces:**
- Consumes: everything prior; install instructions must match `.claude-plugin/marketplace.json` (marketplace name `claude-oracle`, plugin name `oracle`).

- [ ] **Step 1: Write `README.md`** covering, in this order (exact install command syntax from the research report — assume GitHub repo `Techn0Ninja27/claude-oracle`):
  - Title + one-line pitch: weaker (or any) model consults a best-model read-only oracle when unsure, instead of flailing solo — fewer wasted tokens, better code.
  - **How it works** — 3 bullets: doctrine (SessionStart), oracle agent (fable alias, read-only, full-brief contract), Stop-hook safety net (conservative markers, question-to-user + user-text suppression, cooldown, fail-open).
  - **Install** — marketplace add + plugin install commands, plus local-directory variant.
  - **Usage** — nothing to do manually; example of what a consult looks like (brief fields listed); note any-tier usage (second opinion).
  - **The brief contract** — the five fields, one line each.
  - **Configuration** — none in v1; model aliases fable→opus fallback explained.
  - **Requirements** — Claude Code with plugin support, Python 3.8+ on PATH as `python`.
  - **Development** — `python -m pytest -q`; repo layout table; link to spec + plan under `docs/`.
  - **License** — MIT.
  Keep it tight and professional — no marketing fluff, no emoji walls. This is the public face of the repo.

- [ ] **Step 2: Verify all commands/paths in README against the actual repo** (manifest names, file paths, pytest command).

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: README with install, usage, brief contract, development guide"
```

---

### Task 7: Review pass + fixes

- [ ] **Step 1: Dispatch code-reviewer subagent(s)** over the full repo (hooks script + tests + agent + manifests + README) checking: spec conformance (every spec bullet implemented), fail-open posture actually total (no code path that can exit non-zero or crash the hook), false-positive surface of the marker list, hooks.json schema correctness, README accuracy.
- [ ] **Step 2: Triage findings** (superpowers:receiving-code-review — verify before implementing), fix real issues TDD-style, re-run `python -m pytest -q`.
- [ ] **Step 3: Commit fixes**

```bash
git add -A
git commit -m "fix: address review findings"
```

---

### Task 8: Integration smoke test (live Claude Code)

- [ ] **Step 1: SessionStart + plugin load smoke.** `--plugin-dir` is UNCONFIRMED in docs (report §11) — wire the two hooks directly into a temp `--settings` JSON instead (commands pointing at `C:/proga/claude-oracle/hooks/oracle_hook.py` with forward slashes, same fallback chaining as hooks.json). From a scratch temp dir: `claude -p "Reply with the single word READY" --model haiku --settings <temp-settings.json>`. Expected: exits 0, replies READY, no hook errors in output.
- [ ] **Step 2: Stop-hook live check.** Same setup, prompt: `claude -p "Say exactly: I'm stuck and cannot figure out this problem. Then stop." --model haiku --output-format json`. Expected: the stop hook blocks once; final output shows the model continued after the nudge (or, if it dispatched an oracle consult, that's a pass too). Confirm no infinite loop (session terminates).
- [ ] **Step 3: Record results** in `docs/plans/2026-07-23-oracle-plugin.md` under a "Smoke results" heading (date, commands, outcome), commit:

```bash
git add docs/plans/2026-07-23-oracle-plugin.md
git commit -m "test: record live integration smoke results"
```

---

## Smoke results

(to be filled by Task 8)
