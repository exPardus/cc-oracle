import json
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "hooks"))

from oracle_hook import run_stop, run_session_start, DOCTRINE, _state_path


def _write_transcript(tmp_path, entries):
    p = tmp_path / "transcript.jsonl"
    p.write_text("\n".join(json.dumps(e) for e in entries) + "\n", encoding="utf-8")
    return str(p)


def _assistant_text(text):
    return {"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": text}]}}


def _user_prompt(text):
    return {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": text}]}}


def _payload(tmp_path, entries, session=None, prompt_id="p-1", stop_hook_active=False):
    return json.dumps({
        "session_id": session or _fresh_session(),
        "prompt_id": prompt_id,
        "transcript_path": _write_transcript(tmp_path, entries),
        "stop_hook_active": stop_hook_active,
    })


def _fresh_session():
    return f"sess-{time.time_ns()}"


def _isolate_state(monkeypatch, tmp_path):
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(tmp_path / "plugin-data"))


def test_blocks_on_stuck_turn(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    payload = _payload(tmp_path, [_user_prompt("fix it"), _assistant_text("I'm stuck. The mock never fires.")])
    code, out = run_stop(payload)
    assert code == 0
    decision = json.loads(out)
    assert decision["decision"] == "block"
    assert "oracle" in decision["reason"].lower()


def test_silent_when_no_marker(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    payload = _payload(tmp_path, [_user_prompt("fix it"), _assistant_text("Done. Tests pass.")])
    assert run_stop(payload) == (0, "")


def test_silent_when_stop_hook_active(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    payload = _payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck.")], stop_hook_active=True)
    assert run_stop(payload) == (0, "")


def test_silent_when_oracle_already_consulted(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    entries = [
        _user_prompt("fix it"),
        {"type": "assistant", "message": {"role": "assistant", "content": [
            {"type": "tool_use", "name": "Task", "input": {"subagent_type": "oracle"}}]}},
        _assistant_text("I'm stuck even after the consult."),
    ]
    payload = _payload(tmp_path, entries)
    assert run_stop(payload) == (0, "")


def test_silent_on_malformed_stdin():
    assert run_stop("this is not json") == (0, "")


def test_silent_on_missing_transcript(monkeypatch, tmp_path):
    _isolate_state(monkeypatch, tmp_path)
    payload = json.dumps({"session_id": _fresh_session(), "prompt_id": "p-1",
                          "transcript_path": "Z:/nope.jsonl", "stop_hook_active": False})
    assert run_stop(payload) == (0, "")


def test_silent_on_corrupted_transcript(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    p = tmp_path / "corrupt.jsonl"
    p.write_text("garbage\n{{{not json\n", encoding="utf-8")
    payload = json.dumps({"session_id": _fresh_session(), "prompt_id": "p-1",
                          "transcript_path": str(p), "stop_hook_active": False})
    assert run_stop(payload) == (0, "")


def test_turn_guard_blocks_once_per_prompt(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    session = _fresh_session()
    entries = [_user_prompt("fix"), _assistant_text("I'm stuck. No idea.")]
    payload_a = _payload(tmp_path, entries, session=session, prompt_id="p-1")
    code, out = run_stop(payload_a)
    assert json.loads(out)["decision"] == "block"
    # same turn (same prompt_id): waved through
    assert run_stop(payload_a) == (0, "")
    # NEW turn (new prompt_id), same session: eligible again — no wall-clock window
    payload_b = _payload(tmp_path, entries, session=session, prompt_id="p-2")
    code2, out2 = run_stop(payload_b)
    assert json.loads(out2)["decision"] == "block"


def test_session_start_emits_additional_context_envelope():
    code, out = run_session_start()
    assert code == 0
    envelope = json.loads(out)
    hso = envelope["hookSpecificOutput"]
    assert hso["hookEventName"] == "SessionStart"
    assert hso["additionalContext"] == DOCTRINE
    assert "oracle" in DOCTRINE.lower()
    # doctrine must stay tiny (spec: 3-6 doctrine lines + 2 wrapper tags)
    assert len(DOCTRINE.splitlines()) <= 8


def test_state_paths_distinct_for_colliding_session_ids(monkeypatch, tmp_path):
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(tmp_path / "plugin-data"))
    assert _state_path("a/b") != _state_path("ab")


def test_state_dir_is_namespaced_under_plugin_data(monkeypatch, tmp_path):
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(tmp_path / "data"))
    p = _state_path("some-session")
    assert Path(p).parent.name == "oracle-state"
    assert Path(p).parent.parent == tmp_path / "data"


def test_string_false_stop_hook_active_does_not_suppress(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    payload_dict = json.loads(_payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")]))
    payload_dict["stop_hook_active"] = "false"
    code, out = run_stop(json.dumps(payload_dict))
    assert json.loads(out)["decision"] == "block"
