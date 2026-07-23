import json
import os
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "hooks"))

from oracle_hook import run_stop, run_session_start, DOCTRINE, _state_path, _record_block, _already_blocked


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
    # Basename must identify THIS plugin — foreign-looking dirs are rejected.
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(tmp_path / "oracle"))


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


def test_stale_marker_from_previous_turn_does_not_block(tmp_path, monkeypatch):
    # Only the CURRENT turn's assistant text may be scanned: a stuck statement
    # from an earlier turn must not trigger a block when the final turn ends
    # without text (e.g. tool_use only).
    _isolate_state(monkeypatch, tmp_path)
    entries = [
        _user_prompt("first ask"),
        _assistant_text("I'm stuck. No idea."),
        _user_prompt("second ask"),
        {"type": "assistant", "message": {"role": "assistant", "content": [
            {"type": "tool_use", "name": "Bash", "input": {"command": "ls"}}]}},
    ]
    assert run_stop(_payload(tmp_path, entries)) == (0, "")


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
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(tmp_path / "oracle"))
    assert _state_path("a/b") != _state_path("ab")


def test_state_dir_is_namespaced_under_plugin_data(monkeypatch, tmp_path):
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(tmp_path / "oracle"))
    p = _state_path("some-session")
    assert Path(p).parent.name == "oracle-state"
    assert Path(p).parent.parent == tmp_path / "oracle"


def test_state_path_ignores_foreign_plugin_data_env(monkeypatch, tmp_path):
    # Live incident (plan doc, retry section): CLAUDE_PLUGIN_DATA leaked from
    # an unrelated plugin's env and redirected our state file into its data
    # dir. A dir that does not identify THIS plugin must be ignored.
    foreign = tmp_path / "codex-openai-codex"
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(foreign))
    p = Path(_state_path("some-session"))
    assert foreign not in p.parents


def test_state_path_accepts_own_plugin_data_env(monkeypatch, tmp_path):
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(tmp_path / "oracle"))
    p = Path(_state_path("some-session"))
    assert tmp_path / "oracle" in p.parents


def test_state_path_accepts_marketplace_scoped_own_dir(monkeypatch, tmp_path):
    # Harness data dirs can be scoped "<plugin>-<marketplace>". The scoped
    # name is derived from the manifests — previously this test hardcoded a
    # wrong marketplace name ("claude-oracle") and only proved the hook
    # agreed with its own mistake.
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(tmp_path / "oracle-cc-oracle"))
    p = Path(_state_path("some-session"))
    assert tmp_path / "oracle-cc-oracle" in p.parents


def test_own_dir_names_agree_with_manifests_on_disk():
    # The allowlist must track the manifest JSONs: a future rename of the
    # plugin or marketplace goes red here instead of silently stranding
    # state+config in the OS temp dir.
    import oracle_hook
    root = Path(oracle_hook.__file__).resolve().parent.parent / ".claude-plugin"
    plugin = json.loads((root / "plugin.json").read_text(encoding="utf-8"))["name"]
    market = json.loads((root / "marketplace.json").read_text(encoding="utf-8"))["name"]
    assert oracle_hook._PLUGIN_NAME == plugin
    assert oracle_hook._MARKETPLACE_NAME == market
    assert oracle_hook._OWN_DATA_DIR_NAMES == frozenset(
        (plugin, plugin + "-" + market, plugin + "@" + market)
    )


def test_state_path_rejects_oracle_prefixed_foreign_plugin(monkeypatch, tmp_path):
    # Open startswith("oracle-") prefix would accept an unrelated plugin whose
    # name merely begins with ours. The allowlist must be exact known forms.
    foreign = tmp_path / "oracle-db-tools"
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(foreign))
    p = Path(_state_path("some-session"))
    assert foreign not in p.parents


def test_interrupted_state_write_preserves_previous_record(monkeypatch, tmp_path):
    # A crash mid-write must not clobber the existing record — a truncated
    # state file would let the same prompt be blocked twice.
    _isolate_state(monkeypatch, tmp_path)
    import oracle_hook
    session = _fresh_session()
    _record_block(session, "p-1")

    def boom(*args, **kwargs):
        raise OSError("disk full")
    monkeypatch.setattr(oracle_hook.json, "dump", boom)
    _record_block(session, "p-2")
    assert _already_blocked(session, "p-1")


def test_stale_state_files_pruned_on_write(monkeypatch, tmp_path):
    _isolate_state(monkeypatch, tmp_path)
    _record_block("old-session", "p-1")
    _record_block("fresh-session", "p-1")
    old = Path(_state_path("old-session"))
    stale = time.time() - 40 * 86400
    os.utime(old, (stale, stale))
    _record_block("new-session", "p-1")
    assert not old.exists()
    assert Path(_state_path("fresh-session")).exists()


def test_stdin_bom_tolerated(tmp_path, monkeypatch):
    # Windows pipes can prepend a UTF-8 BOM; it must not disable the hook.
    _isolate_state(monkeypatch, tmp_path)
    payload = "﻿" + _payload(tmp_path, [_user_prompt("fix"), _assistant_text("I'm stuck. No idea.")])
    code, out = run_stop(payload)
    assert json.loads(out)["decision"] == "block"


def test_main_returns_zero_when_stdin_read_raises(monkeypatch):
    class BrokenStdin:
        def read(self):
            raise OSError("pipe gone")
    monkeypatch.setattr(sys, "stdin", BrokenStdin())
    from oracle_hook import main
    assert main(["oracle_hook.py", "stop"]) == 0


def test_string_false_stop_hook_active_does_not_suppress(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    payload_dict = json.loads(_payload(tmp_path, [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")]))
    payload_dict["stop_hook_active"] = "false"
    code, out = run_stop(json.dumps(payload_dict))
    assert json.loads(out)["decision"] == "block"
