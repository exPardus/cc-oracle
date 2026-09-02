"""Differential parity: the Go binary must behave exactly like the Python hook.

The unit tests on each side prove each side self-consistent. Only this file
proves they agree. It drives BOTH implementations with byte-identical stdin,
env, and transcript files, and asserts byte-identical stdout and exit codes.

Skipped unless a Go binary is available. Point at one explicitly with
ORACLE_GO_BIN, otherwise the host-native build under hooks/bin is used.

Real session transcripts (multi-megabyte, full of odd real-world shapes) are
the highest-value corpus here, so they are included when present. They are
read from the user's Claude projects directory, copied to a temp dir, and
never written back or committed.
"""
import json
import os
import platform
import subprocess
import sys
import time
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parent.parent
PY_HOOK = str(ROOT / "hooks" / "oracle_hook.py")


def _default_go_bin():
    goos = {"windows": "windows", "darwin": "darwin", "linux": "linux"}.get(sys.platform, sys.platform)
    if sys.platform.startswith("win"):
        goos = "windows"
    machine = platform.machine().lower()
    goarch = "arm64" if machine in ("arm64", "aarch64") else "amd64"
    ext = ".exe" if goos == "windows" else ""
    return ROOT / "hooks" / "bin" / f"oracle-hook-{goos}-{goarch}{ext}"


GO_BIN = Path(os.environ.get("ORACLE_GO_BIN") or _default_go_bin())

pytestmark = pytest.mark.skipif(
    not GO_BIN.exists(), reason=f"Go binary not built at {GO_BIN}"
)


def _run(cmd, stdin_bytes, env_extra):
    env = dict(os.environ)
    env.pop("CC_ORACLE_DISABLE", None)
    env.update(env_extra)
    return subprocess.run(
        cmd, input=stdin_bytes, capture_output=True, env=env, timeout=60
    )


def _both(mode, stdin_bytes, tmp_path, env_extra=None):
    """Run both implementations in isolated state dirs; return (py, go) results."""
    env_extra = dict(env_extra or {})
    py_data = tmp_path / "oracle"          # basename must identify this plugin
    go_data = tmp_path / "go" / "oracle"
    py_data.mkdir(parents=True, exist_ok=True)
    go_data.mkdir(parents=True, exist_ok=True)

    py_env = dict(env_extra, CLAUDE_PLUGIN_DATA=str(py_data))
    go_env = dict(env_extra, CLAUDE_PLUGIN_DATA=str(go_data))
    py = _run([sys.executable, PY_HOOK, mode], stdin_bytes, py_env)
    go = _run([str(GO_BIN), mode], stdin_bytes, go_env)
    return py, go


def _semantic(raw):
    """Parsed form of a hook's stdout, or None if it is not JSON."""
    if not raw.strip():
        return None
    try:
        return json.loads(raw.decode("utf-8"))
    except (ValueError, UnicodeDecodeError):
        return None


def _assert_identical(py, go, label):
    assert py.returncode == go.returncode, (
        f"{label}: exit code differs — python={py.returncode} go={go.returncode}\n"
        f"go stderr: {go.stderr[:500]!r}"
    )
    if py.stdout == go.stdout:
        return

    # Byte mismatch. Say WHICH kind, so a cosmetic encoding drift is never
    # mistaken for a behavioral one (and vice versa). Both are failures: the
    # Go side owns a Python-compatible JSON encoder precisely so that the
    # bytes match, and a drift there is a real regression in this port.
    py_sem, go_sem = _semantic(py.stdout), _semantic(go.stdout)
    if py_sem is not None and py_sem == go_sem:
        kind = ("SAME DECISION, DIFFERENT BYTES — JSON encoding drift "
                "(separators, \\uXXXX escaping, or HTML escaping)")
    else:
        kind = "DIFFERENT DECISION — behavioral divergence"
    raise AssertionError(
        f"{label}: {kind}\n"
        f"python: {py.stdout[:800]!r}\n"
        f"go:     {go.stdout[:800]!r}"
    )


def _transcript(tmp_path, entries, name="t.jsonl"):
    p = tmp_path / name
    p.write_text("\n".join(json.dumps(e) for e in entries) + "\n", encoding="utf-8")
    return str(p)


def _assistant(text):
    return {"type": "assistant", "message": {"role": "assistant",
            "content": [{"type": "text", "text": text}]}}


def _user(text):
    return {"type": "user", "message": {"role": "user",
            "content": [{"type": "text", "text": text}]}}


def _payload(transcript_path, prompt_id="p-1", stop_hook_active=False, session=None):
    return json.dumps({
        "session_id": session or f"diff-{time.time_ns()}",
        "prompt_id": prompt_id,
        "transcript_path": transcript_path,
        "stop_hook_active": stop_hook_active,
    }).encode("utf-8")


# --- the full detection corpus, driven end to end through both binaries -------

FIRING = [
    "I'm not sure why this test fails.",
    "I AM STUCK on this segfault.",
    "I can't figure out where the config is loaded.",
    "I'm stuck. The mock never gets called and I can't figure out why.",
    "I hit a brick wall trying to trace the leak.",
    "I hit the brick wall on this refactor.",
    "I'm hitting a brick wall with this linker error.",
    "I kept hitting the brick wall on the auth flow.",
    "I'm at a dead end with this stack trace.",
    "I hit a dead end debugging the race.",
    "I reached a dead end on the migration.",
    "I'm stumped by this segfault.",
    "I am stumped. The trace makes no sense.",
    "I'm at a loss with this flaky test.",
    "I'm out of ideas on the deadlock.",
    "I'm running out of ideas for this build failure.",
    "I keep going in circles on this config issue.",
    "I've been going around in circles with the types.",
    "I'm going round in circles trying to reproduce it.",
    "I can't work out where the config is loaded.",
    "I cannot work out why the mock never fires.",
    "I have no idea how to unblock this build.",
    "I have no idea why the pipeline segfaults.",
    "I really have no idea why the mock never fires.",
    "I'm completely out of ideas on this deadlock.",
    "I'm totally stumped by this segfault.",
    "I am honestly at a loss with this flaky test.",
    "I just can't work out where the config is loaded.",
    "I simply cannot work out why it fails.",
    "I have absolutely no idea how to unblock this build.",
    "I've run out of ideas on this race condition.",
    "We ran out of ideas after the third bisect.",
    "I genuinely hit a brick wall with the linker.",
    "I'm stuck. The command `npm test` fails and I can't figure out why.",
    "```\ncode a\n```\nI'm stuck on this linker error.\n```\ncode b\n```",
    "> old log line, irrelevant\nI'm stuck on this linker error.",
    "The assert 3 > 2 holds, yet I'm stuck on the failing test.",
    "I'm not\nsure why this fails.",
    "I'm  not sure why this fails.",
]

SILENT = [
    "All tests pass. Done.",
    "Make sure the tests pass. This is not surprising.",
    "Refactored the parser; all 34 tests pass.",
    "I'm not sure what scope you want here — full rewrite or patch?",
    "I'm not sure which option fits. Do you prefer A or B?",
    "I'm not sure which one you want — A or B?  Meanwhile I'll wait.",
    "Fixed. The old error was:\n```\nI'm not sure what to do here\n```\nAll tests pass now.",
    "The log line `cannot figure out the encoding` is expected and harmless.",
    'The marker list includes "I\'m not sure", quoted here, not stated.',
    "The reviewer wrote:\n> I'm not sure this is right\nI disagree; the code is fine.",
    "Quoting the thread:\n> > I am stuck on this\nResolved upstream.",
    "I do not have any idea generator wired up yet, but the stub works. Done.",
    "I am not out of ideas yet — next I will bisect the failing commit again.",
    "I have not hit a brick wall; progress is steady. Continuing.",
    "I have no idea how long the full build takes on CI, so I set the timeout to 60 minutes. Done.",
    "I have no idea how many rows the table holds in prod, so the migration batches by 1000. Shipped.",
    "I have no idea how much memory the worker peaks at, so I set a conservative limit. Done.",
    "I have no idea how big the upload can get, so the handler streams to disk. Done.",
    "I have no idea how often the cron fires in staging, so I added idempotency. Done.",
    "We built a brick wall texture for the level. Done.",
    "The street is a dead end; the depot sits at its end. Route mapped.",
    "Everything worked out fine after the rebase.",
    "The workout routine parser now passes all tests.",
    "These ideas are out of scope for v1. Shipped the rest.",
    "The loop iterates in circles of radius r. Implemented.",
    "The DFS backtracks whenever it has hit a dead end, which is expected.",
    "The animation keeps the icons going in circles as designed.",
    "The user had no idea how the crash happened, so I added logging.",
    "This plugin matches phrases like hit a brick wall in assistant text.",
    "The maze solver marks a cell dead when the walker has reached a dead end.",
    "Users who are out of ideas can consult the docs. Shipped.",
]

# Cases where the lookahead rewrite could plausibly diverge from Python's RE2-less
# original. These are the whole reason this file exists.
LOOKAHEAD_EDGE = [
    "I have no idea how long CI takes, and separately I'm stuck on the linker error.",
    "I have no idea how longitude is computed here.",
    "I have no idea how longer builds affect cache hits.",
    "I have no idea how many. I have no idea why it crashes.",
    "I have no idea howling wolves got in the test fixture.",
    "I have no idea how much. Done.",
]


@pytest.mark.parametrize("text", FIRING + SILENT + LOOKAHEAD_EDGE)
def test_parity_detection_corpus(tmp_path, text):
    path = _transcript(tmp_path, [_user("do the thing"), _assistant(text)])
    py, go = _both("stop", _payload(path), tmp_path)
    _assert_identical(py, go, f"text={text!r}")


# --- payload-shape parity -----------------------------------------------------

def test_parity_malformed_stdin(tmp_path):
    py, go = _both("stop", b"this is not json", tmp_path)
    _assert_identical(py, go, "malformed stdin")


def test_parity_garbage_bytes_stdin(tmp_path):
    py, go = _both("stop", b"\xff\xfe not json at all", tmp_path)
    _assert_identical(py, go, "garbage bytes stdin")


def test_parity_bom_prefixed_stdin(tmp_path):
    path = _transcript(tmp_path, [_user("x"), _assistant("I'm stuck. No idea.")])
    py, go = _both("stop", b"\xef\xbb\xbf" + _payload(path), tmp_path)
    _assert_identical(py, go, "BOM stdin")


def test_parity_empty_stdin(tmp_path):
    py, go = _both("stop", b"", tmp_path)
    _assert_identical(py, go, "empty stdin")


def test_parity_json_array_payload(tmp_path):
    py, go = _both("stop", b'["not", "an", "object"]', tmp_path)
    _assert_identical(py, go, "array payload")


def test_parity_missing_transcript(tmp_path):
    payload = json.dumps({"session_id": "s", "prompt_id": "p",
                          "transcript_path": "Z:/nope.jsonl",
                          "stop_hook_active": False}).encode()
    py, go = _both("stop", payload, tmp_path)
    _assert_identical(py, go, "missing transcript")


def test_parity_no_transcript_key(tmp_path):
    py, go = _both("stop", b'{"session_id":"s"}', tmp_path)
    _assert_identical(py, go, "no transcript key")


@pytest.mark.parametrize("value", [True, False, "false", "true", 0, 1, None])
def test_parity_stop_hook_active_variants(tmp_path, value):
    path = _transcript(tmp_path, [_user("x"), _assistant("I'm stuck. No idea.")])
    payload = json.dumps({"session_id": f"s-{time.time_ns()}", "prompt_id": "p-1",
                          "transcript_path": path,
                          "stop_hook_active": value}).encode()
    py, go = _both("stop", payload, tmp_path)
    _assert_identical(py, go, f"stop_hook_active={value!r}")


def test_parity_corrupted_transcript(tmp_path):
    p = tmp_path / "corrupt.jsonl"
    p.write_text("garbage\n{{{not json\n", encoding="utf-8")
    py, go = _both("stop", _payload(str(p)), tmp_path)
    _assert_identical(py, go, "corrupted transcript")


def test_parity_invalid_utf8_transcript(tmp_path):
    p = tmp_path / "t.jsonl"
    p.write_bytes(
        b'{"type": "user", "message": {"role": "user", "content": "fix \xff it"}}\n'
        + json.dumps(_assistant("I'm stuck. No idea.")).encode("utf-8") + b"\n"
    )
    py, go = _both("stop", _payload(str(p)), tmp_path)
    _assert_identical(py, go, "invalid utf8 transcript")


def test_parity_empty_transcript_file(tmp_path):
    p = tmp_path / "empty.jsonl"
    p.write_text("", encoding="utf-8")
    py, go = _both("stop", _payload(str(p)), tmp_path)
    _assert_identical(py, go, "empty transcript")


def test_parity_sidechain_entries_ignored(tmp_path):
    entries = [
        _user("fix"),
        dict(_assistant("I'm stuck in a subagent."), isSidechain=True),
        _assistant("Done. All tests pass."),
    ]
    py, go = _both("stop", _payload(_transcript(tmp_path, entries)), tmp_path)
    _assert_identical(py, go, "sidechain ignored")


@pytest.mark.parametrize("sidechain", [True, "false", 0, 1, "", None, [], {}, "yes"])
def test_parity_sidechain_truthiness(tmp_path, sidechain):
    """Python treats isSidechain as a truthiness test, so "false" SKIPS the entry."""
    stuck = dict(_assistant("I'm stuck. No idea."))
    stuck["isSidechain"] = sidechain
    entries = [_user("fix"), _assistant("earlier text"), stuck]
    py, go = _both("stop", _payload(_transcript(tmp_path, entries)), tmp_path)
    _assert_identical(py, go, f"isSidechain={sidechain!r}")


def test_parity_oracle_consulted(tmp_path):
    entries = [
        _user("fix it"),
        {"type": "assistant", "message": {"role": "assistant", "content": [
            {"type": "tool_use", "name": "Task", "input": {"subagent_type": "oracle"}}]}},
        _assistant("I'm stuck even after the consult."),
    ]
    py, go = _both("stop", _payload(_transcript(tmp_path, entries)), tmp_path)
    _assert_identical(py, go, "oracle consulted")


@pytest.mark.parametrize("subagent", [
    "oracle", "oracle:oracle", "my-oracledb-helper", "general-purpose",
    "ORACLE", "plugin:oracle", "oracle-db", 42, None,
])
def test_parity_subagent_name_forms(tmp_path, subagent):
    entries = [
        _user("fix it"),
        {"type": "assistant", "message": {"role": "assistant", "content": [
            {"type": "tool_use", "name": "Task", "input": {"subagent_type": subagent}}]}},
        _assistant("I'm stuck even after that."),
    ]
    py, go = _both("stop", _payload(_transcript(tmp_path, entries)), tmp_path)
    _assert_identical(py, go, f"subagent_type={subagent!r}")


def test_parity_stale_marker_previous_turn(tmp_path):
    entries = [
        _user("first ask"),
        _assistant("I'm stuck. No idea."),
        _user("second ask"),
        {"type": "assistant", "message": {"role": "assistant", "content": [
            {"type": "tool_use", "name": "Bash", "input": {"command": "ls"}}]}},
    ]
    py, go = _both("stop", _payload(_transcript(tmp_path, entries)), tmp_path)
    _assert_identical(py, go, "stale marker previous turn")


def test_parity_string_content_entry(tmp_path):
    entries = [_user("x"),
               {"type": "assistant", "message": {"role": "assistant",
                                                 "content": "I'm stuck on the mock."}}]
    py, go = _both("stop", _payload(_transcript(tmp_path, entries)), tmp_path)
    _assert_identical(py, go, "string content")


def test_parity_null_content_entry(tmp_path):
    entries = [_user("x"),
               {"type": "assistant", "message": {"role": "assistant", "content": None}},
               _assistant("I'm stuck. No idea.")]
    py, go = _both("stop", _payload(_transcript(tmp_path, entries)), tmp_path)
    _assert_identical(py, go, "null content")


def test_parity_missing_message_key(tmp_path):
    entries = [_user("x"), {"type": "assistant"}, _assistant("I'm stuck. No idea.")]
    py, go = _both("stop", _payload(_transcript(tmp_path, entries)), tmp_path)
    _assert_identical(py, go, "missing message key")


# --- per-turn guard parity (stateful sequence) --------------------------------

def test_parity_turn_guard_sequence(tmp_path):
    """Block once, wave through on repeat, block again on a new prompt_id."""
    path = _transcript(tmp_path, [_user("fix"), _assistant("I'm stuck. No idea.")])
    session = f"diff-{time.time_ns()}"
    for prompt_id in ("p-1", "p-1", "p-2", "p-2", "p-3"):
        payload = _payload(path, prompt_id=prompt_id, session=session)
        py, go = _both("stop", payload, tmp_path)
        _assert_identical(py, go, f"turn guard prompt_id={prompt_id}")


# --- session-start parity -----------------------------------------------------

def test_parity_session_start(tmp_path):
    py, go = _both("session-start", b"{}", tmp_path)
    _assert_identical(py, go, "session-start")


def test_parity_session_start_empty_stdin(tmp_path):
    py, go = _both("session-start", b"", tmp_path)
    _assert_identical(py, go, "session-start empty stdin")


def test_parity_unknown_mode(tmp_path):
    py, go = _both("bogus-mode", b"", tmp_path)
    _assert_identical(py, go, "unknown mode")


def test_parity_kill_switch(tmp_path):
    path = _transcript(tmp_path, [_user("x"), _assistant("I'm stuck. No idea.")])
    py, go = _both("stop", _payload(path), tmp_path, {"CC_ORACLE_DISABLE": "1"})
    _assert_identical(py, go, "kill switch stop")
    py, go = _both("session-start", b"{}", tmp_path, {"CC_ORACLE_DISABLE": "1"})
    _assert_identical(py, go, "kill switch session-start")


# --- config parity ------------------------------------------------------------

def _write_configs(tmp_path, cfg):
    """config.json must land in BOTH implementations' isolated data dirs."""
    for base in (tmp_path / "oracle", tmp_path / "go" / "oracle"):
        d = base / "oracle-state"
        d.mkdir(parents=True, exist_ok=True)
        (d / "config.json").write_text(json.dumps(cfg), encoding="utf-8")


@pytest.mark.parametrize("cfg", [
    {"stop_hook": False},
    {"doctrine": False},
    {"stop_hook": "no", "doctrine": 0, "state_dir": 42},
    {"markers": {"add": ["No Clue  How"]}},
    {"markers": {"remove": ["i'm stuck"]}},
    {"markers": {"remove": ["hit a brick wall"]}},
    {"markers": {"add": ["ok marker", 42], "remove": "i'm stuck"}},
    {"markers": {"remove": ["never was a marker"]}},
    {"markers": "not a dict"},
])
def test_parity_config_variants(tmp_path, cfg):
    _write_configs(tmp_path, cfg)
    path = _transcript(tmp_path, [_user("x"), _assistant("I'm stuck. No idea.")])
    py, go = _both("stop", _payload(path), tmp_path)
    _assert_identical(py, go, f"config={cfg} stop")
    py, go = _both("session-start", b"{}", tmp_path)
    _assert_identical(py, go, f"config={cfg} session-start")


def test_parity_malformed_config(tmp_path):
    for base in (tmp_path / "oracle", tmp_path / "go" / "oracle"):
        d = base / "oracle-state"
        d.mkdir(parents=True, exist_ok=True)
        (d / "config.json").write_text("{not json", encoding="utf-8")
    path = _transcript(tmp_path, [_user("x"), _assistant("I'm stuck. No idea.")])
    py, go = _both("stop", _payload(path), tmp_path)
    _assert_identical(py, go, "malformed config")


@pytest.mark.parametrize("body", [
    # cp1252 "é" — what Notepad's "ANSI" save produces, and an ordinary way for
    # a Windows user to end up with a non-UTF-8 config.
    b'{"stop_hook": false, "state_dir": "/opt/jos\xe9/oracle"}',
    b'{"doctrine": false, "note": "caf\xe9"}',
    b'{"stop_hook": false, "n": "\x80"}',
    b'{"stop_hook": false, "n": "\xe2\x82"}',
])
def test_parity_non_utf8_config(tmp_path, body):
    """Python decodes config.json strictly; Go must not be more permissive.

    Python's read_text(encoding="utf-8") raises on the first bad byte and the
    whole config is discarded. Go's json decoder would substitute U+FFFD and
    honour the file, so a config could apply under one half of the dispatch
    chain and not the other.
    """
    for base in (tmp_path / "oracle", tmp_path / "go" / "oracle"):
        d = base / "oracle-state"
        d.mkdir(parents=True, exist_ok=True)
        (d / "config.json").write_bytes(body)
    path = _transcript(tmp_path, [_user("x"), _assistant("I'm stuck. No idea.")])
    py, go = _both("stop", _payload(path), tmp_path)
    _assert_identical(py, go, f"non-utf8 config stop: {body!r}")
    py, go = _both("session-start", b"{}", tmp_path)
    _assert_identical(py, go, f"non-utf8 config session-start: {body!r}")


def test_parity_added_marker_fires(tmp_path):
    _write_configs(tmp_path, {"markers": {"add": ["no clue how"]}})
    path = _transcript(tmp_path, [_user("x"), _assistant("I have no clue how to proceed here.")])
    py, go = _both("stop", _payload(path), tmp_path)
    _assert_identical(py, go, "added marker")


def test_parity_foreign_plugin_data_ignored(tmp_path):
    """A leaked CLAUDE_PLUGIN_DATA from another plugin must be rejected identically.

    Both sides then fall back to the OS temp dir, which would be the SAME
    directory for both — so the first one to run records the block and the
    second correctly reports the prompt as already blocked. That is the
    per-turn guard working, not a divergence, but it would make this test
    compare a first run against a second.

    Give each implementation its own temp root instead. Python resolves the
    temp dir from TMPDIR, TEMP, TMP; Go from TMP, TEMP, USERPROFILE. Setting
    all three keeps each side self-consistent while separating the two.
    """
    foreign = tmp_path / "codex-openai-codex"
    (foreign / "oracle-state").mkdir(parents=True)
    (foreign / "oracle-state" / "config.json").write_text(
        json.dumps({"stop_hook": False}), encoding="utf-8")
    path = _transcript(tmp_path, [_user("x"), _assistant("I'm stuck. No idea.")])
    payload = _payload(path)

    def env_for(name):
        root = tmp_path / f"tmp-{name}"
        root.mkdir(exist_ok=True)
        env = dict(os.environ)
        env.pop("CC_ORACLE_DISABLE", None)
        env["CLAUDE_PLUGIN_DATA"] = str(foreign)
        env.update(TMPDIR=str(root), TEMP=str(root), TMP=str(root))
        return env

    py = _run([sys.executable, PY_HOOK, "stop"], payload, env_for("py"))
    go = _run([str(GO_BIN), "stop"], payload, env_for("go"))
    _assert_identical(py, go, "foreign plugin data")
    # Both must have IGNORED the foreign config (which disables the stop hook).
    assert py.stdout, "foreign config silenced the hook; the guard is not working"


# --- real session transcripts: the highest-value corpus -----------------------

def _real_transcripts(limit=12):
    base = Path.home() / ".claude" / "projects"
    if not base.is_dir():
        return []
    found = sorted(base.glob("*/*.jsonl"), key=lambda p: p.stat().st_size, reverse=True)
    return found[:limit]


REAL = _real_transcripts()


@pytest.mark.skipif(not REAL, reason="no real transcripts available")
@pytest.mark.parametrize("src", REAL, ids=lambda p: f"{p.stat().st_size // 1024}KB")
def test_parity_on_real_transcripts(tmp_path, src):
    """Real transcripts carry shapes no synthetic fixture thinks to produce."""
    dst = tmp_path / "real.jsonl"
    dst.write_bytes(src.read_bytes())
    py, go = _both("stop", _payload(str(dst)), tmp_path)
    _assert_identical(py, go, f"real transcript {src.name}")


@pytest.mark.skipif(not REAL, reason="no real transcripts available")
@pytest.mark.parametrize("src", REAL[:4], ids=lambda p: f"{p.stat().st_size // 1024}KB")
def test_parity_real_transcript_with_appended_stuck_turn(tmp_path, src):
    """Same, but forced down the blocking path so the write side is compared too."""
    dst = tmp_path / "real.jsonl"
    body = src.read_bytes()
    if not body.endswith(b"\n"):
        body += b"\n"
    body += json.dumps(_user("now fix it")).encode() + b"\n"
    body += json.dumps(_assistant("I'm stuck. The mock never fires.")).encode() + b"\n"
    dst.write_bytes(body)
    py, go = _both("stop", _payload(str(dst)), tmp_path)
    _assert_identical(py, go, f"real+stuck {src.name}")


# --- progress-stall families --------------------------------------------------

STALL_FIRING = [
    "I'm not making progress on this deadlock.",
    "I am not making any progress here.",
    "We are not making much progress on the migration.",
    "I can't make progress until the linker is fixed.",
    "I keep getting the same TypeError from the parser.",
    "I keep hitting the same assertion failure.",
    "We have been seeing the same timeout all afternoon.",
    "I still can't get the mock to fire.",
    "I still cannot reproduce the crash locally.",
    "I'm not getting anywhere with this stack trace.",
    "This is not getting me anywhere.",
]

STALL_SILENT = [
    "The build is not making progress because the queue is paused. Fixed.",
    "The retry keeps getting the same result, which is correct. Done.",
    "The migration still cannot run on Postgres 12; documented that. Shipped.",
    "I am not out of ideas, and I am making progress. Continuing.",
    "This did not get anywhere near the memory limit. Shipped.",
    "Progress bars now render correctly. Done.",
    "I tried everything on the checklist and all of it passed. Done.",
    "I still can't tell which layout you prefer - A or B?",
]


@pytest.mark.parametrize("text", STALL_FIRING + STALL_SILENT)
def test_parity_progress_stall_corpus(tmp_path, text):
    path = _transcript(tmp_path, [_user("do the thing"), _assistant(text)])
    py, go = _both("stop", _payload(path), tmp_path)
    _assert_identical(py, go, f"stall text={text!r}")


# --- last_assistant_message fast path ----------------------------------------

def _payload_with_message(transcript_path, message, prompt_id="p-1", session=None):
    body = {
        "session_id": session or f"diff-{time.time_ns()}",
        "prompt_id": prompt_id,
        "transcript_path": transcript_path,
        "stop_hook_active": False,
    }
    if message is not _MISSING:
        body["last_assistant_message"] = message
    return json.dumps(body).encode("utf-8")


_MISSING = object()


@pytest.mark.parametrize("message", [
    "I'm stuck. The mock never fires.",
    "All done. Tests pass.",
    "",
    "   ",
    None,
    42,
    [],
    {},
    _MISSING,
    "I still can't get the mock to fire.",
    "Should I use A or B?",
])
def test_parity_last_assistant_message_variants(tmp_path, message):
    """The direct field must be honoured, or fall back, identically on both sides."""
    path = _transcript(tmp_path, [_user("x"), _assistant("I'm stuck. Stale on-disk text.")])
    payload = _payload_with_message(path, message)
    py, go = _both("stop", payload, tmp_path)
    label = "MISSING" if message is _MISSING else repr(message)
    _assert_identical(py, go, f"last_assistant_message={label}")


def test_parity_direct_message_on_unflushed_transcript(tmp_path):
    """Tail is a thinking block only: the field must win without any waiting."""
    thinking = {"type": "assistant", "message": {"role": "assistant",
                "content": [{"type": "thinking", "thinking": "hmm"}]}}
    path = _transcript(tmp_path, [_user("x"), thinking])
    payload = _payload_with_message(path, "I'm stuck. The final text arrived late.")
    py, go = _both("stop", payload, tmp_path)
    _assert_identical(py, go, "direct message, unflushed transcript")


# --- behavioral trigger: consecutive tool failures ---------------------------

def _tool_use(tid, name):
    return {"type": "assistant", "message": {"role": "assistant", "content": [
        {"type": "tool_use", "id": tid, "name": name, "input": {}}]}}


def _tool_result(tid, is_error):
    return {"type": "user", "message": {"role": "user", "content": [
        {"type": "tool_result", "tool_use_id": tid, "is_error": is_error, "content": "x"}]}}


def _turn(tools_errors, tail):
    entries = [_user("fix the build")]
    for i, (tool, err) in enumerate(tools_errors):
        entries.append(_tool_use(f"t{i}", tool))
        entries.append(_tool_result(f"t{i}", err))
    if tail is not None:
        entries.append(_assistant(tail))
    return entries


@pytest.mark.parametrize("tools_errors,tail,label", [
    ([("PowerShell", True), ("PowerShell", True), ("Bash", True)], "Trying another approach.", "3 exec failures"),
    ([("Bash", True), ("Bash", True)], "Trying another approach.", "2 exec failures"),
    ([("Read", True), ("Glob", True), ("Grep", True), ("LS", True)], "Looking elsewhere.", "probes only"),
    ([("Bash", True), ("Bash", False), ("Bash", True)], "Continuing.", "success breaks run"),
    ([("PowerShell", True), ("Read", True), ("Bash", True)], "Continuing.", "probe mid-run"),
    ([("Bash", True), ("Bash", True), ("Bash", True), ("Bash", True)], "Continuing.", "4 exec failures"),
    ([("Bash", True), ("Bash", True), ("Bash", True)], "Should I try the other approach?", "streak + question"),
    ([("Bash", True), ("Bash", True), ("Bash", True)], None, "streak, no trailing text"),
    ([("Write", True), ("Write", True), ("Write", True)], "Continuing.", "3 Write failures"),
])
def test_parity_failure_streak(tmp_path, tools_errors, tail, label):
    path = _transcript(tmp_path, _turn(tools_errors, tail))
    py, go = _both("stop", _payload(path), tmp_path)
    _assert_identical(py, go, f"streak: {label}")


def test_parity_streak_after_oracle_consult(tmp_path):
    entries = [_user("fix"),
               {"type": "assistant", "message": {"role": "assistant", "content": [
                   {"type": "tool_use", "name": "Task", "input": {"subagent_type": "oracle"}}]}}]
    for i in range(3):
        entries.append(_tool_use(f"t{i}", "Bash"))
        entries.append(_tool_result(f"t{i}", True))
    entries.append(_assistant("Continuing."))
    py, go = _both("stop", _payload(_transcript(tmp_path, entries)), tmp_path)
    _assert_identical(py, go, "streak after consult")


@pytest.mark.parametrize("is_error", [True, False, "true", "false", 1, 0, None, "", []])
def test_parity_is_error_truthiness(tmp_path, is_error):
    """is_error is read with Python truthiness; the two sides must agree."""
    entries = [_user("fix")]
    for i in range(3):
        entries.append(_tool_use(f"t{i}", "Bash"))
        entries.append(_tool_result(f"t{i}", is_error))
    entries.append(_assistant("Continuing."))
    py, go = _both("stop", _payload(_transcript(tmp_path, entries)), tmp_path)
    _assert_identical(py, go, f"is_error={is_error!r}")


@pytest.mark.parametrize("streak", [0, 1, 2, 3, 5, 99, "3", 3.5, True, -1, None])
def test_parity_failure_streak_config(tmp_path, streak):
    _write_configs(tmp_path, {"failure_streak": streak})
    path = _transcript(tmp_path, _turn(
        [("Bash", True), ("Bash", True), ("Bash", True)], "Continuing."))
    py, go = _both("stop", _payload(path), tmp_path)
    _assert_identical(py, go, f"failure_streak={streak!r}")
