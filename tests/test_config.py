"""v1.1 configuration surface + portability floor.

Config is plugin-local: a single optional file at
<CLAUDE_PLUGIN_DATA or OS temp>/oracle-state/config.json — the exact same
base-dir resolution the hook already uses for its per-turn state, so the
location is environment-independent (no cwd, no HOME).

Knobs (all optional; every default reproduces v1 behavior exactly):
  stop_hook (bool), doctrine (bool), markers.add/.remove (list of str),
  state_dir (str — where per-turn block-state files live).
Failure posture: malformed file or wrong-typed key -> ignored, defaults
apply (fail open, never wedge a session).
"""
import json
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "hooks"))

import oracle_hook
from oracle_hook import (
    MARKERS,
    MIN_PYTHON,
    _config_path,
    _state_path,
    effective_markers,
    load_config,
    main,
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


def _payload(tmp_path, entries, prompt_id="p-1"):
    return json.dumps({
        "session_id": _fresh_session(),
        "prompt_id": prompt_id,
        "transcript_path": _write_transcript(tmp_path, entries),
        "stop_hook_active": False,
    })


def _isolate(monkeypatch, tmp_path):
    # Basename must identify THIS plugin — foreign-looking dirs are rejected
    # by the state/config path resolution (foreign-env guard).
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(tmp_path / "oracle"))
    monkeypatch.delenv("CC_ORACLE_DISABLE", raising=False)


def _write_config(tmp_path, cfg):
    d = tmp_path / "oracle" / "oracle-state"
    d.mkdir(parents=True, exist_ok=True)
    (d / "config.json").write_text(json.dumps(cfg), encoding="utf-8")


# --- config location ----------------------------------------------------------

def test_config_path_uses_state_dir_logic(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    p = Path(_config_path())
    assert p.name == "config.json"
    assert p.parent.name == "oracle-state"
    assert p.parent.parent == tmp_path / "oracle"


def test_foreign_env_does_not_redirect_config(monkeypatch, tmp_path):
    # The foreign-CLAUDE_PLUGIN_DATA guard governs the config location too:
    # a leaked env var must not let another plugin's dir supply our config.
    foreign = tmp_path / "codex-openai-codex"
    d = foreign / "oracle-state"
    d.mkdir(parents=True)
    (d / "config.json").write_text(json.dumps({"stop_hook": False}), encoding="utf-8")
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(foreign))
    monkeypatch.delenv("CC_ORACLE_DISABLE", raising=False)
    assert foreign not in Path(_config_path()).parents
    cfg = load_config()
    assert cfg["stop_hook"] is True  # foreign config never read


def test_config_path_falls_back_to_tempdir(monkeypatch, tmp_path):
    monkeypatch.delenv("CLAUDE_PLUGIN_DATA", raising=False)
    p = Path(_config_path())
    assert p.name == "config.json"
    assert p.parent.name == "oracle-state"


# --- load_config / effective_markers -----------------------------------------

def test_defaults_when_no_config(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    cfg = load_config()
    assert cfg["stop_hook"] is True
    assert cfg["doctrine"] is True
    assert cfg["state_dir"] is None
    assert effective_markers(cfg) == set(MARKERS)


def test_config_adds_and_removes_markers(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    _write_config(tmp_path, {"markers": {"add": ["No Clue  How"], "remove": ["I'M CONFUSED"]}})
    marks = effective_markers(load_config())
    assert "no clue how" in marks          # normalized: lowercase, collapsed whitespace
    assert "i'm confused" not in marks     # remove is case-insensitive
    assert "i'm stuck" in marks            # rest of builtins intact


def test_malformed_config_ignored(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    d = tmp_path / "plugin-data" / "oracle-state"
    d.mkdir(parents=True)
    (d / "config.json").write_text("{not json", encoding="utf-8")
    cfg = load_config()
    assert cfg["stop_hook"] is True
    assert effective_markers(cfg) == set(MARKERS)


def test_wrong_types_ignored_per_key(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    _write_config(tmp_path, {
        "stop_hook": "no",                  # not a bool -> ignored
        "doctrine": 0,                      # not a bool -> ignored
        "state_dir": 42,                    # not a str -> ignored
        "markers": {"add": ["ok marker", 42], "remove": "i'm stuck"},  # non-str / non-list filtered
    })
    cfg = load_config()
    assert cfg["stop_hook"] is True
    assert cfg["doctrine"] is True
    assert cfg["state_dir"] is None
    marks = effective_markers(cfg)
    assert "ok marker" in marks
    assert "i'm stuck" in marks             # bad remove shape -> no removal


def test_remove_unknown_marker_is_noop(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    _write_config(tmp_path, {"markers": {"remove": ["never was a marker"]}})
    assert effective_markers(load_config()) == set(MARKERS)


# --- run_stop wiring ----------------------------------------------------------

def test_stop_hook_false_silences_block(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    _write_config(tmp_path, {"stop_hook": False})
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")])
    assert run_stop(payload) == (0, "")


def test_added_marker_triggers_block(monkeypatch, tmp_path):
    # "going in circles" became a built-in variant family, so the add-knob is
    # exercised with a phrase NOT in the baseline set.
    _isolate(monkeypatch, tmp_path)
    _write_config(tmp_path, {"markers": {"add": ["no clue how"]}})
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I have no clue how to proceed here.")])
    code, out = run_stop(payload)
    assert json.loads(out)["decision"] == "block"


def test_removed_variant_family_marker_no_longer_blocks(monkeypatch, tmp_path):
    # Config mutates the POST-variant-family set: families are baseline and
    # individually removable like any core marker.
    _isolate(monkeypatch, tmp_path)
    _write_config(tmp_path, {"markers": {"remove": ["hit a brick wall"]}})
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I hit a brick wall tracing the leak.")])
    assert run_stop(payload) == (0, "")


def test_removed_marker_no_longer_blocks(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    _write_config(tmp_path, {"markers": {"remove": ["i'm stuck"]}})
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")])
    assert run_stop(payload) == (0, "")


def test_zero_config_blocks_like_v1(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")])
    code, out = run_stop(payload)
    assert json.loads(out)["decision"] == "block"


def test_env_kill_switch_silences_stop(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    monkeypatch.setenv("CC_ORACLE_DISABLE", "1")
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")])
    assert run_stop(payload) == (0, "")


# --- state_dir knob -----------------------------------------------------------

def test_state_dir_knob_relocates_state_files(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    custom = tmp_path / "custom-state"
    _write_config(tmp_path, {"state_dir": str(custom)})
    session = _fresh_session()
    entries = [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")]
    payload = json.dumps({
        "session_id": session, "prompt_id": "p-1",
        "transcript_path": _write_transcript(tmp_path, entries),
        "stop_hook_active": False,
    })
    code, out = run_stop(payload)
    assert json.loads(out)["decision"] == "block"
    assert list(custom.glob("*.json"))               # state recorded in custom dir
    # per-turn guard still works through the custom location
    assert run_stop(payload) == (0, "")


def test_state_path_default_unchanged_from_v1(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    p = Path(_state_path("some-session"))
    assert p.parent.name == "oracle-state"
    assert p.parent.parent == tmp_path / "oracle"


def test_prune_never_deletes_config(monkeypatch, tmp_path):
    # config.json shares the oracle-state dir with per-turn state files; the
    # 30-day pruning sweep must exempt it regardless of age.
    import os as _os
    import time as _time
    from oracle_hook import _record_block
    _isolate(monkeypatch, tmp_path)
    _write_config(tmp_path, {"doctrine": True})
    cfg_file = tmp_path / "oracle" / "oracle-state" / "config.json"
    stale = _time.time() - 40 * 86400
    _os.utime(cfg_file, (stale, stale))
    _record_block(_fresh_session(), "p-1")  # triggers the pruning sweep
    assert cfg_file.exists()


# --- run_session_start wiring -------------------------------------------------

def test_doctrine_false_silences_session_start(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    _write_config(tmp_path, {"doctrine": False})
    assert run_session_start() == (0, "")


def test_session_start_default_still_injects(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    code, out = run_session_start()
    assert code == 0
    assert "additionalContext" in out


def test_env_kill_switch_silences_session_start(monkeypatch, tmp_path):
    _isolate(monkeypatch, tmp_path)
    monkeypatch.setenv("CC_ORACLE_DISABLE", "1")
    assert run_session_start() == (0, "")


# --- portability floor --------------------------------------------------------

def test_min_python_floor_declared():
    assert MIN_PYTHON == (3, 9)
    assert sys.version_info >= MIN_PYTHON  # test env itself must satisfy the floor


def test_below_floor_main_is_silent_noop(monkeypatch, capsys):
    # Floor semantics: an ancient interpreter gets exit 0 and no output,
    # never a traceback — and stdin is never touched (nothing to wedge on).
    monkeypatch.setattr(oracle_hook.sys, "version_info", (3, 8, 0))
    assert main(["oracle_hook.py", "stop"]) == 0
    assert main(["oracle_hook.py", "session-start"]) == 0
    assert capsys.readouterr().out == ""


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


def test_hook_source_has_no_310_syntax():
    # Guard against 3.10+ syntax creeping in (match/case, PEP 604 unions in
    # annotations, parenthesized context managers are the usual suspects).
    import ast
    path = Path(__file__).resolve().parent.parent / "hooks" / "oracle_hook.py"
    tree = ast.parse(path.read_text(encoding="utf-8"))
    assert not any(type(n).__name__ in ("Match", "MatchCase") for n in ast.walk(tree))


def test_stop_output_is_ascii_safe(monkeypatch, tmp_path):
    # Emitted JSON must survive any console codepage: ensure_ascii escaping.
    _isolate(monkeypatch, tmp_path)
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")])
    _, out = run_stop(payload)
    assert out and all(ord(c) < 128 for c in out)
