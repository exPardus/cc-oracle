#!/usr/bin/env python3
"""claude-oracle hooks: SessionStart doctrine injection + Stop-hook safety net.

Stdlib only. Failure posture: any unexpected input -> exit 0, never wedge a session.
"""
import json
import os
import re
import sys
import tempfile

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
# Quoting is not stating: markers inside fenced code, inline code, or
# double-quoted strings are exempt. Single quotes are NOT stripped —
# apostrophes in contractions ("I'm") would corrupt matching.
_FENCED_CODE = re.compile(r"```.*?```", re.DOTALL)
_INLINE_CODE = re.compile(r"`[^`\n]*`")
_DOUBLE_QUOTED = re.compile(r'"[^"\n]*"')


def _strip_quoted(text):
    text = _FENCED_CODE.sub(" ", text)
    text = _INLINE_CODE.sub(" ", text)
    text = _DOUBLE_QUOTED.sub(" ", text)
    return text


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
    stripped = _strip_quoted(text)
    return marker_hit(stripped) and not is_question_turn(stripped)


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


def _is_oracle_subagent(name):
    # Exact-name rule: "oracle" or plugin-scoped "<plugin>:oracle".
    # Never a bare substring test — "my-oracledb-helper" must not count.
    return name == "oracle" or name.endswith(":oracle")


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
                if _is_oracle_subagent(subagent):
                    return True
    return False
