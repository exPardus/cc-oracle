"""v1.1 configuration surface + portability floor.

Config precedence: defaults < user (~/.claude/oracle.json) < project
(<cwd>/.claude/oracle.json). Env CC_ORACLE_DISABLE=1 is a global kill-switch.
Failure posture everywhere: malformed config -> that layer is ignored, the
plugin keeps its defaults (fail open, never wedge a session).
"""
import json
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "hooks"))

from oracle_hook import (
    MARKERS,
    MIN_PYTHON,
    effective_markers,
    load_config,
    run_session_start,
    run_stop,
)


def _fresh_session():
    return f"sess-{time.time_ns()}"


def _write_transcript(tmp_path, entries):
    p = tmp_path / "transcript.jsonl"
    p.write_text("\n".join(json.dumps(e) for e in entries) + "\n", encoding="utf-8")
    return str(p)


def _assistant_text(text):
    return {"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": text}]}}


def _user_prompt(text):
    return {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": text}]}}


def _payload(tmp_path, entries, cwd=None, prompt_id="p-1"):
    return json.dumps({
        "session_id": _fresh_session(),
        "prompt_id": prompt_id,
        "transcript_path": _write_transcript(tmp_path, entries),
        "stop_hook_active": False,
        "cwd": cwd,
    })


def _project(tmp_path, cfg):
    proj = tmp_path / "proj"
    (proj / ".claude").mkdir(parents=True, exist_ok=True)
    (proj / ".claude" / "oracle.json").write_text(json.dumps(cfg), encoding="utf-8")
    return str(proj)


def _isolate(monkeypatch, tmp_path):
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(tmp_path / "plugin-data"))
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setenv("USERPROFILE", str(tmp_path / "home"))
    monkeypatch.delenv("CC_ORACLE_DISABLE", raising=False)


def _user_home_config(tmp_path, cfg):
    d = tmp_path / "home" / ".claude"
    d.mkdir(parents=True, exist_ok=True)
    (d / "oracle.json").write_text(json.dumps(cfg), encoding="utf-8")


# --- load_config / effective_markers -----------------------------------------

def test_defaults_when_no_config(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    cfg = load_config(str(tmp_path / "empty-proj"))
    assert cfg["stop_hook"] is True
    assert cfg["doctrine"] is True
    assert effective_markers(cfg) == set(MARKERS)


def test_project_config_adds_and_removes_markers(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    proj = _project(tmp_path, {"markers": {"add": ["No Clue  How"], "remove": ["I'M CONFUSED"]}})
    marks = effective_markers(load_config(proj))
    assert "no clue how" in marks          # normalized: lowercase, collapsed whitespace
    assert "i'm confused" not in marks     # remove is case-insensitive
    assert "i'm stuck" in marks            # rest of builtins intact


def test_project_overrides_user(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    _user_home_config(tmp_path, {"stop_hook": True, "markers": {"add": ["from user"]}})
    proj = _project(tmp_path, {"stop_hook": False})
    cfg = load_config(proj)
    assert cfg["stop_hook"] is False       # project wins per-key
    assert "from user" in effective_markers(cfg)  # unshadowed user keys survive


def test_user_config_applies_without_project(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    _user_home_config(tmp_path, {"doctrine": False})
    cfg = load_config(str(tmp_path / "no-proj"))
    assert cfg["doctrine"] is False


def test_malformed_project_config_ignored(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    proj = tmp_path / "proj"
    (proj / ".claude").mkdir(parents=True)
    (proj / ".claude" / "oracle.json").write_text("{not json", encoding="utf-8")
    cfg = load_config(str(proj))
    assert cfg["stop_hook"] is True
    assert effective_markers(cfg) == set(MARKERS)


def test_wrong_types_ignored_per_key(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    proj = _project(tmp_path, {
        "stop_hook": "no",                  # not a bool -> ignored
        "doctrine": 0,                      # not a bool -> ignored
        "markers": {"add": ["ok marker", 42], "remove": "i'm stuck"},  # non-str / non-list filtered
    })
    cfg = load_config(proj)
    assert cfg["stop_hook"] is True
    assert cfg["doctrine"] is True
    marks = effective_markers(cfg)
    assert "ok marker" in marks
    assert "i'm stuck" in marks             # bad remove shape -> no removal


def test_remove_unknown_marker_is_noop(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    proj = _project(tmp_path, {"markers": {"remove": ["never was a marker"]}})
    assert effective_markers(load_config(proj)) == set(MARKERS)


# --- run_stop wiring ----------------------------------------------------------

def test_stop_hook_false_silences_block(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    proj = _project(tmp_path, {"stop_hook": False})
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")], cwd=proj)
    assert run_stop(payload) == (0, "")


def test_added_marker_triggers_block(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    proj = _project(tmp_path, {"markers": {"add": ["going in circles"]}})
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I keep going in circles here.")], cwd=proj)
    code, out = run_stop(payload)
    assert json.loads(out)["decision"] == "block"


def test_removed_marker_no_longer_blocks(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    proj = _project(tmp_path, {"markers": {"remove": ["i'm stuck"]}})
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")], cwd=proj)
    assert run_stop(payload) == (0, "")


def test_env_kill_switch_silences_stop(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    monkeypatch.setenv("CC_ORACLE_DISABLE", "1")
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")])
    assert run_stop(payload) == (0, "")


def test_missing_cwd_still_blocks_with_defaults(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")], cwd=None)
    code, out = run_stop(payload)
    assert json.loads(out)["decision"] == "block"


# --- run_session_start wiring -------------------------------------------------

def test_doctrine_false_silences_session_start(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    proj = _project(tmp_path, {"doctrine": False})
    assert run_session_start(json.dumps({"cwd": proj})) == (0, "")


def test_session_start_default_still_injects(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    code, out = run_session_start(json.dumps({"cwd": str(tmp_path / "no-proj")}))
    assert code == 0
    assert "additionalContext" in out


def test_session_start_empty_stdin_fails_open_to_injection(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    code, out = run_session_start("")
    assert code == 0
    assert "additionalContext" in out


def test_env_kill_switch_silences_session_start(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    monkeypatch.setenv("CC_ORACLE_DISABLE", "1")
    assert run_session_start("") == (0, "")


# --- portability floor --------------------------------------------------------

def test_min_python_floor_declared():
    assert MIN_PYTHON == (3, 9)
    assert sys.version_info >= MIN_PYTHON  # test env itself must satisfy the floor


def test_transcript_with_invalid_utf8_still_detected(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    p = tmp_path / "transcript.jsonl"
    bad = b'{"type": "user", "message": {"role": "user", "content": "fix \xff it"}}\n'
    good = json.dumps(_assistant_text("I'm stuck. No idea.")).encode("utf-8") + b"\n"
    p.write_bytes(bad + good)
    payload = json.dumps({
        "session_id": _fresh_session(), "prompt_id": "p-1",
        "transcript_path": str(p), "stop_hook_active": False,
    })
    code, out = run_stop(payload)
    assert json.loads(out)["decision"] == "block"  # one bad byte must not kill detection


def test_hook_source_utf8_and_compiles():
    # Portability floor: source must be valid UTF-8 (Python 3 default source
    # encoding on every OS) and compile cleanly.
    path = Path(__file__).resolve().parent.parent / "hooks" / "oracle_hook.py"
    src = path.read_bytes().decode("utf-8")
    compile(src, str(path), "exec")


def test_stop_output_is_ascii_safe(monkeypatch, tmp_path):
    # Emitted JSON must survive any console codepage: ensure_ascii escaping.
    _isolate(monkeypatch, tmp_path)
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")])
    _, out = run_stop(payload)
    assert out and all(ord(c) < 128 for c in out)
