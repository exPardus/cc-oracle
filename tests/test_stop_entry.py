import json
import os
import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "hooks"))

from oracle_hook import run_stop, run_session_start, DOCTRINE, _state_path, _record_block, _already_blocked


@pytest.fixture(autouse=True)
def _no_real_sleep(monkeypatch):
    # Every test whose turn ends without assistant text walks the full
    # flush-retry budget, so without this the suite burns that budget in real
    # wall clock — and silently grows slower each time the budget is widened.
    # Tests that care about the waits re-stub _sleep themselves.
    import oracle_hook
    monkeypatch.setattr(oracle_hook, "_sleep", lambda _seconds: None)


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


def test_flush_race_rereads_late_assistant_text(tmp_path, monkeypatch):
    # Live incident (macOS, 7 concurrent Stop hooks): at Stop time the harness
    # had flushed only a thinking-block entry; the final text landed on disk
    # milliseconds after the hook read the transcript, so a genuinely stuck
    # turn was waved through. When the current turn has no assistant text yet,
    # the hook must wait briefly and re-read instead of staying silent.
    _isolate_state(monkeypatch, tmp_path)
    import oracle_hook
    thinking_only = {"type": "assistant", "message": {"role": "assistant", "content": [
        {"type": "thinking", "thinking": "hmm"}]}}
    early = [_user_prompt("trigger it"), thinking_only]
    late = early + [_assistant_text("I'm stuck. The final text arrived late.")]
    path = _write_transcript(tmp_path, early)

    def flush_lands(_seconds):
        _write_transcript(tmp_path, late)
    monkeypatch.setattr(oracle_hook, "_sleep", flush_lands)

    payload = json.dumps({"session_id": _fresh_session(), "prompt_id": "p-1",
                          "transcript_path": path, "stop_hook_active": False})
    code, out = run_stop(payload)
    assert code == 0
    assert json.loads(out)["decision"] == "block"


def test_flush_race_fires_when_the_turn_opened_with_a_preamble(tmp_path, monkeypatch):
    # The shape that matters: a real agentic turn opens with a preamble text
    # block before its tool calls. A retry probe that asks "does this turn have
    # any assistant text" is satisfied by that preamble, breaks out with zero
    # re-reads, and judges the turn on stale text while the real final message
    # is still in flight — the exact race this hook exists to close, sailing
    # straight through on the COMMON turn shape rather than the rare one.
    _isolate_state(monkeypatch, tmp_path)
    import oracle_hook
    early = [
        _user_prompt("fix the mock"),
        _assistant_text("Let me look at this."),
        {"type": "assistant", "message": {"role": "assistant", "content": [
            {"type": "tool_use", "name": "Bash", "input": {"command": "pytest"}}]}},
        {"type": "user", "message": {"role": "user", "content": [
            {"type": "tool_result", "content": "1 failed"}]}},
        {"type": "assistant", "message": {"role": "assistant", "content": [
            {"type": "thinking", "thinking": "hmm"}]}},
    ]
    late = early + [_assistant_text("I'm stuck. The mock never fires.")]
    path = _write_transcript(tmp_path, early)

    def flush_lands(_seconds):
        _write_transcript(tmp_path, late)
    monkeypatch.setattr(oracle_hook, "_sleep", flush_lands)

    payload = json.dumps({"session_id": _fresh_session(), "prompt_id": "p-1",
                          "transcript_path": path, "stop_hook_active": False})
    code, out = run_stop(payload)
    assert code == 0
    assert json.loads(out)["decision"] == "block"


def test_flush_retry_survives_a_torn_re_read(tmp_path, monkeypatch):
    # A re-read that lands mid-write returns no entries (locked file on
    # Windows, or every line torn). That is the condition the retry exists to
    # ride out, so it must spend the next delay rather than abandon the budget.
    _isolate_state(monkeypatch, tmp_path)
    import oracle_hook
    thinking_only = {"type": "assistant", "message": {"role": "assistant", "content": [
        {"type": "thinking", "thinking": "hmm"}]}}
    early = [_user_prompt("trigger it"), thinking_only]
    late = early + [_assistant_text("I'm stuck. That read was torn.")]
    path = _write_transcript(tmp_path, early)

    waits = []

    def torn_then_flushed(seconds):
        waits.append(seconds)
        if len(waits) == 1:
            Path(path).write_text("", encoding="utf-8")   # torn / mid-write
        else:
            _write_transcript(tmp_path, late)
    monkeypatch.setattr(oracle_hook, "_sleep", torn_then_flushed)

    payload = json.dumps({"session_id": _fresh_session(), "prompt_id": "p-1",
                          "transcript_path": path, "stop_hook_active": False})
    code, out = run_stop(payload)
    assert code == 0
    assert json.loads(out)["decision"] == "block"
    assert len(waits) == 2, "torn read must cost one delay, not the whole budget"


def test_flush_retry_stays_silent_on_keyboard_interrupt(tmp_path, monkeypatch):
    # KeyboardInterrupt is a BaseException, so the hook's `except Exception`
    # does not catch it: Ctrl+C during the wait would traceback and exit
    # nonzero, and hooks.json turns a nonzero exit into a re-run of the hook.
    _isolate_state(monkeypatch, tmp_path)
    import oracle_hook

    def interrupted(_seconds):
        raise KeyboardInterrupt
    monkeypatch.setattr(oracle_hook, "_sleep", interrupted)

    entries = [
        _user_prompt("x"),
        {"type": "assistant", "message": {"role": "assistant", "content": [
            {"type": "thinking", "thinking": "hmm"}]}},
    ]
    assert run_stop(_payload(tmp_path, entries)) == (0, "")


def test_flush_race_catches_text_landing_on_the_final_retry(tmp_path, monkeypatch):
    # Pins the retry budget as SUFFICIENT, not merely bounded: text that lands
    # only on the last scheduled wait must still be caught. Without this, a
    # future trim of _FLUSH_DELAYS would silently reopen the race that the
    # other two tests keep passing through.
    _isolate_state(monkeypatch, tmp_path)
    import oracle_hook
    thinking_only = {"type": "assistant", "message": {"role": "assistant", "content": [
        {"type": "thinking", "thinking": "hmm"}]}}
    early = [_user_prompt("trigger it"), thinking_only]
    late = early + [_assistant_text("I'm stuck. This one crawled onto disk.")]
    path = _write_transcript(tmp_path, early)

    waits = []

    def flush_lands_last(seconds):
        waits.append(seconds)
        if len(waits) == len(oracle_hook._FLUSH_DELAYS):
            _write_transcript(tmp_path, late)
    monkeypatch.setattr(oracle_hook, "_sleep", flush_lands_last)

    payload = json.dumps({"session_id": _fresh_session(), "prompt_id": "p-1",
                          "transcript_path": path, "stop_hook_active": False})
    code, out = run_stop(payload)
    assert code == 0
    assert json.loads(out)["decision"] == "block"


def test_flush_retry_is_bounded_and_fails_open(tmp_path, monkeypatch):
    # A turn that truly ends without assistant text (tool_use only) must stay
    # silent after a BOUNDED number of re-reads — never spin, never block.
    _isolate_state(monkeypatch, tmp_path)
    import oracle_hook
    sleeps = []
    monkeypatch.setattr(oracle_hook, "_sleep", lambda s: sleeps.append(s))
    entries = [
        _user_prompt("x"),
        {"type": "assistant", "message": {"role": "assistant", "content": [
            {"type": "tool_use", "name": "Bash", "input": {"command": "ls"}}]}},
    ]
    assert run_stop(_payload(tmp_path, entries)) == (0, "")
    assert sleeps == list(oracle_hook._FLUSH_DELAYS)


def test_flush_delays_back_off_and_stay_within_budget():
    # The schedule itself is the fix, so assert on it directly. The incident
    # measured ~50ms of lag under contention; a budget only a few multiples
    # above that reopens the race on a slower disk, and an unbounded one
    # stalls every text-less turn. Strictly increasing waits spend the early
    # retries cheaply and the late ones where the lag actually lives.
    import oracle_hook
    delays = oracle_hook._FLUSH_DELAYS
    assert list(delays) == sorted(set(delays)), "waits must strictly increase"
    assert 0.3 <= sum(delays) <= 1.0, "total wait outside the 300ms-1s budget"


def test_no_retry_when_turn_already_has_text(tmp_path, monkeypatch):
    # The common path — text present on first read — must not pay any latency.
    _isolate_state(monkeypatch, tmp_path)
    import oracle_hook
    sleeps = []
    monkeypatch.setattr(oracle_hook, "_sleep", lambda s: sleeps.append(s))
    payload = _payload(tmp_path, [_user_prompt("fix it"), _assistant_text("Done. Tests pass.")])
    assert run_stop(payload) == (0, "")
    assert sleeps == []


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


# --- last_assistant_message fast path ----------------------------------------

def _tool_use(tid, name):
    return {"type": "assistant", "message": {"role": "assistant", "content": [
        {"type": "tool_use", "id": tid, "name": name, "input": {}}]}}


def _tool_result(tid, is_error):
    return {"type": "user", "message": {"role": "user", "content": [
        {"type": "tool_result", "tool_use_id": tid, "is_error": is_error, "content": "x"}]}}


def test_last_assistant_message_is_used_and_skips_the_flush_wait(tmp_path, monkeypatch):
    # The harness hands us the final text from memory, so it can never be the
    # stale copy the retry existed to work around. When present, no wait at all.
    _isolate_state(monkeypatch, tmp_path)
    import oracle_hook
    sleeps = []
    monkeypatch.setattr(oracle_hook, "_sleep", lambda s: sleeps.append(s))
    # Transcript tail is a thinking block only, which WOULD trigger the retry.
    entries = [_user_prompt("x"), {"type": "assistant", "message": {"role": "assistant",
               "content": [{"type": "thinking", "thinking": "hmm"}]}}]
    payload = json.dumps({
        "session_id": _fresh_session(), "prompt_id": "p-1",
        "transcript_path": _write_transcript(tmp_path, entries),
        "stop_hook_active": False,
        "last_assistant_message": "I'm stuck. The mock never fires.",
    })
    code, out = run_stop(payload)
    assert json.loads(out)["decision"] == "block"
    assert sleeps == [], "the direct field must not pay the flush budget"


def test_last_assistant_message_wins_over_stale_transcript_text(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    entries = [_user_prompt("x"), _assistant_text("I'm stuck. Stale on-disk text.")]
    payload = json.dumps({
        "session_id": _fresh_session(), "prompt_id": "p-1",
        "transcript_path": _write_transcript(tmp_path, entries),
        "stop_hook_active": False,
        "last_assistant_message": "All done. Tests pass.",
    })
    assert run_stop(payload) == (0, "")


def test_blank_last_assistant_message_falls_back_to_transcript(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    entries = [_user_prompt("x"), _assistant_text("I'm stuck. No idea.")]
    for value in ("", "   ", None, 42, [], {}):
        payload_dict = {
            "session_id": _fresh_session(), "prompt_id": "p-1",
            "transcript_path": _write_transcript(tmp_path, entries),
            "stop_hook_active": False, "last_assistant_message": value,
        }
        code, out = run_stop(json.dumps(payload_dict))
        assert json.loads(out)["decision"] == "block", value


def test_nudge_quotes_the_triggering_sentence(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    entries = [_user_prompt("x"), _assistant_text(
        "Refactored it. I'm stuck on the failing mock. Tests still red.")]
    _, out = run_stop(_payload(tmp_path, entries))
    reason = json.loads(out)["reason"]
    assert '"I\'m stuck on the failing mock."' in reason
    assert "full brief" in reason


# --- behavioral trigger: consecutive failures --------------------------------

def test_consecutive_failures_block_without_any_marker(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    entries = [_user_prompt("fix the build")]
    for i, tool in enumerate(("PowerShell", "PowerShell", "Bash")):
        entries.append(_tool_use(f"t{i}", tool))
        entries.append(_tool_result(f"t{i}", True))
    entries.append(_assistant_text("Trying another approach."))
    code, out = run_stop(_payload(tmp_path, entries))
    reason = json.loads(out)["reason"]
    assert json.loads(out)["decision"] == "block"
    assert "3 consecutive tool failures" in reason
    assert "PowerShell" in reason


def test_two_failures_do_not_block(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    entries = [_user_prompt("fix")]
    for i in range(2):
        entries.append(_tool_use(f"t{i}", "Bash"))
        entries.append(_tool_result(f"t{i}", True))
    entries.append(_assistant_text("Trying another approach."))
    assert run_stop(_payload(tmp_path, entries)) == (0, "")


def test_probe_failures_alone_never_block(tmp_path, monkeypatch):
    # Read/Glob/Grep failing in a row is path-hunting, not flailing.
    _isolate_state(monkeypatch, tmp_path)
    entries = [_user_prompt("find it")]
    for i, tool in enumerate(("Read", "Glob", "Grep", "LS")):
        entries.append(_tool_use(f"t{i}", tool))
        entries.append(_tool_result(f"t{i}", True))
    entries.append(_assistant_text("Looking elsewhere."))
    assert run_stop(_payload(tmp_path, entries)) == (0, "")


def test_success_breaks_the_failure_run(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    entries = [_user_prompt("fix")]
    for i, err in enumerate((True, True, False, True)):
        entries.append(_tool_use(f"t{i}", "Bash"))
        entries.append(_tool_result(f"t{i}", err))
    entries.append(_assistant_text("Continuing."))
    assert run_stop(_payload(tmp_path, entries)) == (0, "")


def test_streak_respects_the_question_exemption(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    entries = [_user_prompt("fix")]
    for i in range(3):
        entries.append(_tool_use(f"t{i}", "Bash"))
        entries.append(_tool_result(f"t{i}", True))
    entries.append(_assistant_text("That failed three times. Should I try the other approach?"))
    assert run_stop(_payload(tmp_path, entries)) == (0, "")


def test_streak_respects_oracle_already_consulted(tmp_path, monkeypatch):
    _isolate_state(monkeypatch, tmp_path)
    entries = [_user_prompt("fix"),
               {"type": "assistant", "message": {"role": "assistant", "content": [
                   {"type": "tool_use", "name": "Task", "input": {"subagent_type": "oracle"}}]}}]
    for i in range(3):
        entries.append(_tool_use(f"t{i}", "Bash"))
        entries.append(_tool_result(f"t{i}", True))
    entries.append(_assistant_text("Continuing."))
    assert run_stop(_payload(tmp_path, entries)) == (0, "")


def test_failure_streak_helper_counts_runs():
    from oracle_hook import failure_streak
    entries = [_tool_use("a", "PowerShell"), _tool_result("a", True),
               _tool_use("r", "Read"), _tool_result("r", True),
               _tool_use("b", "Bash"), _tool_result("b", True)]
    # a probe failure neither extends nor breaks the run
    assert failure_streak(entries) == (2, ("PowerShell", "Bash"))
