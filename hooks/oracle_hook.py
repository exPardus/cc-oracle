#!/usr/bin/env python3
"""claude-oracle hooks: SessionStart doctrine injection + Stop-hook safety net.

Stdlib only. Failure posture: any unexpected input -> exit 0, never wedge a session.
"""
import hashlib
import json
import os
import re
import sys
import tempfile
import time
from pathlib import Path

# Portability floor: everything below this version gets a graceful no-op,
# never a traceback in the user's session. Source syntax must stay parseable
# by older interpreters so the guard in main() is actually reached.
MIN_PYTHON = (3, 9)

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
    # Deflection variant families: live smoke showed models phrase stuckness
    # in idioms the core list never matched ("hit brick wall" — plan retry
    # section). These entries are FAMILY KEYS, not substrings: each maps to a
    # first-person-anchored regex in _FAMILY_PATTERNS below, so benign
    # third-person / negated / past-resolved / meta-mention uses ("it has hit
    # a dead end", "I am not out of ideas yet", "phrases like hit a brick
    # wall") never match. A miss beats a false positive. Keys stay in MARKERS
    # so the config markers.remove knob can drop a family like any core marker.
    "hit a brick wall",
    "at a dead end",
    "i'm stumped",
    "i'm at a loss",
    "out of ideas",
    "going in circles",
    "can't work out",
    "no idea how",
)

# First-person subject fragments. Anchoring rule: the pronoun must sit
# against the idiom's verb phrase with at most an explicit NON-NEGATING
# intensifier adverb between them ("I really have no idea why", "I'm
# completely out of ideas"). The adverb slot is a closed allowlist — never
# \w+ — so "not" keeps breaking adjacency ("I am not out of ideas") and
# third-party subjects ("I think the DFS hit a dead end") still cannot match.
_ADV = r"(?:really |completely |totally |honestly |just |simply |absolutely |genuinely )?"
_FAMILY_PATTERNS = {
    "hit a brick wall":
        r"\b(?:i|we)(?:'ve| have)? " + _ADV + r"(?:hit|kept hitting) (?:a |the )?brick wall\b"
        r"|\b(?:i'm|i am|we're|we are) " + _ADV + r"hitting (?:a |the )?brick wall\b"
        r"|\b(?:i|we) " + _ADV + r"keep hitting (?:a |the )?brick wall\b",
    "at a dead end":
        r"\b(?:i'm|i am|we're|we are) " + _ADV + r"at a dead end\b"
        r"|\b(?:i|we)(?:'ve| have)? " + _ADV + r"(?:hit|reached) a dead end\b",
    "i'm stumped":
        r"\b(?:i'm|i am|we're|we are) " + _ADV + r"stumped\b",
    "i'm at a loss":
        r"\b(?:i'm|i am|we're|we are) " + _ADV + r"at a loss\b",
    "out of ideas":
        r"\b(?:i'm|i am|we're|we are) " + _ADV + r"(?:running )?out of ideas\b"
        r"|\b(?:i've|we've|i|we) " + _ADV + r"(?:run|ran) out of ideas\b",
    "going in circles":
        r"\b(?:i'm|i am|we're|we are) " + _ADV + r"going (?:around |round )?in circles\b"
        r"|\b(?:i|we) " + _ADV + r"keep going (?:around |round )?in circles\b"
        r"|\b(?:i|we)(?:'ve| have) " + _ADV + r"been going (?:around |round )?in circles\b",
    "can't work out":
        r"\b(?:i|we) " + _ADV + r"(?:can't|cannot|can not) " + _ADV + r"work out\b",
    # "how long/many/much/big/often" is a hedge about an unknown quantity,
    # not stuckness — excluded via lookahead.
    "no idea how":
        r"\b(?:i|we) " + _ADV + r"have " + _ADV + r"no idea (?:how(?! long| many| much| big| often)|why)\b"
        r"|\b(?:i've|we've) " + _ADV + r"(?:got )?no idea (?:how(?! long| many| much| big| often)|why)\b",
}
_FAMILY_RES = {key: re.compile(pat) for key, pat in _FAMILY_PATTERNS.items()}

# This plugin's identity, derived from the manifest JSONs at import so the
# allowlist can never drift from what the harness actually names our data
# dir (a hardcoded-but-wrong marketplace name would reject our OWN scoped
# dir and silently strand state+config in the OS temp dir). Used to verify a
# CLAUDE_PLUGIN_DATA env var actually belongs to us — the var is inherited by
# child processes, so a foreign plugin's value can leak into our environment
# (live incident: codex's data dir received our state file). The allowlist is
# EXACT known forms, never a prefix test: an open startswith("oracle-") would
# accept an unrelated "oracle-db-tools" plugin.
def _manifest_names():
    plugin, market = "oracle", "cc-oracle"  # fallback if manifests unreadable
    try:
        manifest_dir = Path(__file__).resolve().parent.parent / ".claude-plugin"
        raw = json.loads((manifest_dir / "plugin.json").read_text(encoding="utf-8"))
        if isinstance(raw, dict) and isinstance(raw.get("name"), str) and raw["name"].strip():
            plugin = raw["name"].strip()
        raw = json.loads((manifest_dir / "marketplace.json").read_text(encoding="utf-8"))
        if isinstance(raw, dict) and isinstance(raw.get("name"), str) and raw["name"].strip():
            market = raw["name"].strip()
    except Exception:
        pass
    return plugin, market


_PLUGIN_NAME, _MARKETPLACE_NAME = _manifest_names()
_OWN_DATA_DIR_NAMES = frozenset((
    _PLUGIN_NAME,
    _PLUGIN_NAME + "-" + _MARKETPLACE_NAME,
    _PLUGIN_NAME + "@" + _MARKETPLACE_NAME,
))


def _own_plugin_data():
    env = os.environ.get("CLAUDE_PLUGIN_DATA", "")
    if not env:
        return None
    if os.path.basename(os.path.normpath(env)) in _OWN_DATA_DIR_NAMES:
        return env
    return None


# Configuration surface (v1.1). Plugin-local: one optional file at
# <own CLAUDE_PLUGIN_DATA or OS temp>/oracle-state/config.json — the same
# base-dir resolution as the per-turn state, so the location is
# environment-independent (no cwd, no HOME) and, like the state, cannot be
# redirected by a foreign plugin's leaked CLAUDE_PLUGIN_DATA. Every default
# reproduces zero-config behavior exactly; a malformed file or wrong-typed
# key is ignored — config can only tune behavior, never break it.
DEFAULTS = {
    "stop_hook": True,
    "doctrine": True,
    "markers_add": (),
    "markers_remove": (),
    "state_dir": None,
}
KILL_SWITCH_ENV = "CC_ORACLE_DISABLE"


def _oracle_data_dir():
    base = _own_plugin_data() or tempfile.gettempdir()
    return Path(base) / "oracle-state"


def _config_path():
    return str(_oracle_data_dir() / "config.json")


def _normalize_marker(m):
    return re.sub(r"\s+", " ", m.strip().lower())


def load_config():
    """Effective config: DEFAULTS overlaid with the well-typed keys of
    config.json. Anything malformed is silently dropped (fail open)."""
    cfg = dict(DEFAULTS)
    try:
        raw = json.loads(Path(_config_path()).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return cfg
    if not isinstance(raw, dict):
        return cfg
    for key in ("stop_hook", "doctrine"):
        if isinstance(raw.get(key), bool):
            cfg[key] = raw[key]
    if isinstance(raw.get("state_dir"), str) and raw["state_dir"].strip():
        cfg["state_dir"] = raw["state_dir"]
    markers = raw.get("markers")
    if isinstance(markers, dict):
        for key, dest in (("add", "markers_add"), ("remove", "markers_remove")):
            val = markers.get(key)
            if isinstance(val, list):
                cleaned = tuple(_normalize_marker(v) for v in val if isinstance(v, str) and v.strip())
                if cleaned:
                    cfg[dest] = cleaned
    return cfg


def effective_markers(cfg):
    # Config mutates the POST-variant-family set: the built-in families are
    # the baseline, add/remove apply on top of the full MARKERS tuple.
    marks = {_normalize_marker(m) for m in MARKERS}
    marks -= set(cfg.get("markers_remove", ()))
    marks |= set(cfg.get("markers_add", ()))
    return marks


def _disabled_by_env():
    return os.environ.get(KILL_SWITCH_ENV, "").strip().lower() in ("1", "true", "yes")


_SENTENCE_SPLIT = re.compile(r"(?<=[.!?])\s+")
# Quoting is not stating: markers inside fenced code, inline code, or
# double-quoted strings are exempt. Single quotes are NOT stripped —
# apostrophes in contractions ("I'm") would corrupt matching.
_FENCED_CODE = re.compile(r"```.*?```", re.DOTALL)
_INLINE_CODE = re.compile(r"`[^`\n]*`")
_DOUBLE_QUOTED = re.compile(r'"[^"\n]*"')
# Markdown blockquote lines (incl. nested "> >"); anchored to line start so
# a mid-line ">" (comparisons, shell redirects) is never treated as quoting.
_BLOCKQUOTE_LINE = re.compile(r"^[ \t]{0,3}>[^\n]*$", re.MULTILINE)


def _strip_quoted(text):
    text = _FENCED_CODE.sub(" ", text)
    text = _BLOCKQUOTE_LINE.sub(" ", text)
    text = _INLINE_CODE.sub(" ", text)
    text = _DOUBLE_QUOTED.sub(" ", text)
    return text


def _sentences(text):
    return [s for s in _SENTENCE_SPLIT.split(text.strip()) if s]


def marker_hit(text, markers=MARKERS):
    low = re.sub(r"\s+", " ", text.lower())
    for m in markers:
        family = _FAMILY_RES.get(m)
        if family is not None:
            if family.search(low):
                return True
        elif m in low:
            return True
    return False


def is_question_turn(text, markers=MARKERS):
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
        if s.rstrip().endswith("?") and marker_hit(s, markers):
            return True
    return False


def should_nudge(text, markers=MARKERS):
    if not text:
        return False
    stripped = _strip_quoted(text)
    return marker_hit(stripped, markers) and not is_question_turn(stripped, markers)


def load_entries(path):
    # errors="replace": one stray invalid byte must not disable the hook.
    entries = []
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    obj = json.loads(line)
                except json.JSONDecodeError:
                    continue
                # Sidechain (subagent) entries share the transcript file; their
                # text and tool_use are not the main thread's and never count.
                if isinstance(obj, dict) and not obj.get("isSidechain"):
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
        content = (entry.get("message") or {}).get("content")
        if isinstance(content, str):
            joined = content.strip()
        else:
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


def _turn_start(entries):
    start = 0
    for i, entry in enumerate(entries):
        if _is_real_user_prompt(entry):
            start = i
    return start


# Subagent dispatch tool is named "Task" in older harnesses, "Agent" in newer.
_DISPATCH_TOOLS = ("Task", "Agent")


def oracle_consulted_this_turn(entries):
    for entry in entries[_turn_start(entries):]:
        if entry.get("type") != "assistant":
            continue
        for b in _content_blocks(entry):
            if b.get("type") == "tool_use" and b.get("name") in _DISPATCH_TOOLS:
                subagent = str((b.get("input") or {}).get("subagent_type", "")).lower()
                if _is_oracle_subagent(subagent):
                    return True
    return False


NUDGE = (
    "You stated uncertainty this turn without consulting the oracle. "
    "Dispatch the `oracle` agent now with a full brief — Goal, Problem (errors verbatim), "
    "Tried (attempts + why each failed), Context (files/constraints), Question (specific ask) — "
    "then implement its plan."
)

DOCTRINE = """<oracle-plugin>
Uncertainty is a signal, not a failure. The moment you notice you are unsure, stuck, confused, or going in circles: do NOT keep attempting solo and pollute your context — dispatch the `oracle` agent first, then implement its plan yourself.
When summoning the oracle, ALWAYS send a full brief: Goal (what the task ultimately wants), Problem (the exact blocker, errors quoted verbatim), Tried (attempts made + why each failed), Context (relevant files/paths, versions, platform, project rules), Question (the specific ask, not "help"). The oracle shares none of your context — a thin brief wastes the consult and forces a second round-trip.
If the dispatch errors for any reason, retry the same Agent call once with model "opus".
This applies at every tier: strong models may consult the oracle for a fresh-context second opinion.
</oracle-plugin>"""


def _state_path(session_id, cfg=None):
    override = (cfg or {}).get("state_dir")
    state_dir = Path(override) if override else _oracle_data_dir()
    state_dir.mkdir(parents=True, exist_ok=True)
    safe = hashlib.sha1(str(session_id).encode("utf-8", "surrogateescape")).hexdigest()[:16]
    return str(state_dir / (safe + ".json"))


def _already_blocked(session_id, prompt_id, cfg=None):
    try:
        with open(_state_path(session_id, cfg), encoding="utf-8") as f:
            return json.load(f).get("blocked_prompt") == prompt_id
    except (OSError, ValueError):
        return False


# Sessions older than this have long ended; their state files are dead weight.
_STATE_TTL_SECONDS = 30 * 86400

# The state_dir knob can point the sweep at a user's own folder, so deletion
# is allowlisted to the exact shapes WE create: 16-hex sha1-prefix state
# records and mkstemp ".tmp" leftovers. Anything else is never touched.
_STATE_FILE_RE = re.compile(r"^[0-9a-f]{16}\.json$")
_TMP_FILE_RE = re.compile(r"^tmp[A-Za-z0-9_]*\.tmp$")


def _prune_stale_state(state_dir):
    try:
        cutoff = time.time() - _STATE_TTL_SECONDS
        for name in os.listdir(state_dir):
            # config.json lives in the same dir and is long-lived by design —
            # never prune it, no matter how old (belt-and-braces: its name
            # does not match the allowlist anyway).
            if name == "config.json":
                continue
            if not (_STATE_FILE_RE.match(name) or _TMP_FILE_RE.match(name)):
                continue
            p = os.path.join(state_dir, name)
            try:
                if os.path.getmtime(p) < cutoff:
                    os.remove(p)
            except OSError:
                pass
    except OSError:
        pass


def _record_block(session_id, prompt_id, cfg=None):
    # Write-to-temp + os.replace: a crash mid-write must never truncate an
    # existing record (a truncated file would allow a double block).
    tmp = None
    try:
        path = _state_path(session_id, cfg)
        state_dir = os.path.dirname(path)
        _prune_stale_state(state_dir)
        fd, tmp = tempfile.mkstemp(dir=state_dir, suffix=".tmp")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump({"blocked_prompt": prompt_id}, f)
        os.replace(tmp, path)
        tmp = None
    except OSError:
        pass
    finally:
        if tmp is not None:
            try:
                os.remove(tmp)
            except OSError:
                pass


def run_stop(stdin_text):
    """Returns (exit_code, stdout). Failure posture: (0, "") on anything unexpected."""
    try:
        if _disabled_by_env():
            return 0, ""
        # Windows pipes can prepend a UTF-8 BOM.
        payload = json.loads(stdin_text.lstrip("﻿"))
        if not isinstance(payload, dict):
            return 0, ""
        if payload.get("stop_hook_active") is True:
            return 0, ""
        cfg = load_config()
        if cfg["stop_hook"] is not True:
            return 0, ""
        transcript_path = payload.get("transcript_path")
        if not transcript_path:
            return 0, ""
        entries = load_entries(transcript_path)
        if not entries:
            return 0, ""
        # Scan the CURRENT turn only: a stuck statement from a previous turn
        # was already stop-checked and must not retrigger.
        entries = entries[_turn_start(entries):]
        if not should_nudge(last_assistant_text(entries), effective_markers(cfg)):
            return 0, ""
        if oracle_consulted_this_turn(entries):
            return 0, ""
        session_id = payload.get("session_id", "unknown")
        prompt_id = payload.get("prompt_id") or ""
        if prompt_id and _already_blocked(session_id, prompt_id, cfg):
            return 0, ""
        if prompt_id:
            _record_block(session_id, prompt_id, cfg)
        return 0, json.dumps({"decision": "block", "reason": NUDGE})
    except Exception:
        return 0, ""


def run_session_start(stdin_text=""):
    """Failure posture is the inverse of run_stop's: config trouble means the
    doctrine IS injected (the default behavior) — only an explicit, well-formed
    opt-out silences it. stdin payload is accepted but unused (config location
    is environment-independent)."""
    if _disabled_by_env():
        return 0, ""
    try:
        if load_config()["doctrine"] is False:
            return 0, ""
    except Exception:
        pass
    envelope = {
        "hookSpecificOutput": {
            "hookEventName": "SessionStart",
            "additionalContext": DOCTRINE,
        }
    }
    return 0, json.dumps(envelope)


def main(argv):
    # Hook entry point: nothing may escape — an uncaught exception exits
    # nonzero and surfaces noise (or worse) in the session.
    try:
        if sys.version_info < MIN_PYTHON:
            return 0
        mode = argv[1] if len(argv) > 1 else ""
        if mode == "session-start":
            code, out = run_session_start(sys.stdin.read())
        elif mode == "stop":
            code, out = run_stop(sys.stdin.read())
        else:
            return 0
        if out:
            sys.stdout.write(out)
        return code
    except Exception:
        return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
