"""End-to-end smoke of the live CLI path: real interpreter, real stdin/stdout."""
import json
import subprocess
import sys
from pathlib import Path

HOOK = str(Path(__file__).resolve().parent.parent / "hooks" / "oracle_hook.py")


def _run(mode, stdin_text="", env_extra=None):
    import os
    env = dict(os.environ)
    if env_extra:
        env.update(env_extra)
    return subprocess.run(
        [sys.executable, HOOK, mode],
        input=stdin_text, capture_output=True, text=True, env=env, timeout=30,
    )


def test_cli_stop_blocks_then_waves_through(tmp_path):
    transcript = tmp_path / "t.jsonl"
    entries = [
        {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": "fix"}]}},
        {"type": "assistant", "message": {"role": "assistant", "content": [
            {"type": "text", "text": "I am stuck. The mock never fires."}]}},
    ]
    transcript.write_text("\n".join(json.dumps(e) for e in entries) + "\n", encoding="utf-8")
    payload = json.dumps({
        "session_id": "smoke-1", "prompt_id": "p-1",
        "transcript_path": str(transcript), "stop_hook_active": False,
    })
    env = {"CLAUDE_PLUGIN_DATA": str(tmp_path / "plugdata")}

    first = _run("stop", payload, env)
    assert first.returncode == 0
    assert json.loads(first.stdout)["decision"] == "block"

    second = _run("stop", payload, env)
    assert second.returncode == 0
    assert second.stdout == ""


def test_cli_stop_garbage_stdin_exits_zero_silent():
    r = _run("stop", "\xff not json at all")
    assert r.returncode == 0
    assert r.stdout == ""


def test_cli_session_start_emits_envelope():
    r = _run("session-start")
    assert r.returncode == 0
    envelope = json.loads(r.stdout)
    assert envelope["hookSpecificOutput"]["hookEventName"] == "SessionStart"


def test_cli_unknown_mode_exits_zero():
    r = _run("bogus-mode")
    assert r.returncode == 0
    assert r.stdout == ""
