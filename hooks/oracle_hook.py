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
