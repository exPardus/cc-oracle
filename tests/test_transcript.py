import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "hooks"))

from oracle_hook import load_entries, last_assistant_text, oracle_consulted_this_turn


def _assistant(text=None, tool=None):
    content = []
    if text is not None:
        content.append({"type": "text", "text": text})
    if tool is not None:
        content.append({"type": "tool_use", "name": tool[0], "input": tool[1]})
    return {"type": "assistant", "message": {"role": "assistant", "content": content}}


def _user_prompt(text):
    return {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": text}]}}


def _user_tool_result():
    return {"type": "user", "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "x", "content": "ok"}]}}


# --- load_entries ---

def test_load_entries_skips_malformed_lines(tmp_path):
    p = tmp_path / "t.jsonl"
    p.write_text('{"type": "user"}\nnot json at all\n{"type": "assistant"}\n', encoding="utf-8")
    entries = load_entries(str(p))
    assert [e["type"] for e in entries] == ["user", "assistant"]


def test_load_entries_missing_file_returns_empty():
    assert load_entries("Z:/definitely/not/here.jsonl") == []


# --- last_assistant_text ---

def test_last_assistant_text_takes_final_text_message():
    entries = [_assistant(text="first"), _user_tool_result(), _assistant(text="I'm stuck on the failing mock.")]
    assert last_assistant_text(entries) == "I'm stuck on the failing mock."


def test_last_assistant_text_skips_tool_use_only_entries():
    entries = [_assistant(text="real text"), _assistant(tool=("Bash", {"command": "ls"}))]
    assert last_assistant_text(entries) == "real text"


def test_last_assistant_text_ignores_user_messages():
    # User saying "I'm not sure" must never be what gets scanned.
    entries = [_assistant(text="done."), _user_prompt("I'm not sure what I want here")]
    assert last_assistant_text(entries) == "done."


def test_last_assistant_text_empty_transcript():
    assert last_assistant_text([]) == ""


# --- oracle_consulted_this_turn ---

def test_consulted_true_when_oracle_task_after_last_user_prompt():
    entries = [
        _user_prompt("fix the bug"),
        _assistant(text="consulting", tool=("Task", {"subagent_type": "oracle", "prompt": "brief"})),
        _user_tool_result(),
        _assistant(text="implementing the plan"),
    ]
    assert oracle_consulted_this_turn(entries)


def test_consulted_matches_plugin_scoped_name():
    entries = [
        _user_prompt("fix the bug"),
        _assistant(tool=("Task", {"subagent_type": "oracle:oracle"})),
    ]
    assert oracle_consulted_this_turn(entries)


def test_consulted_rejects_substring_lookalikes():
    # exact-name rule: an unrelated agent containing "oracle" must NOT count
    entries = [
        _user_prompt("fix the bug"),
        _assistant(tool=("Task", {"subagent_type": "my-oracledb-helper"})),
    ]
    assert not oracle_consulted_this_turn(entries)


def test_consulted_false_when_consult_was_previous_turn():
    entries = [
        _user_prompt("first ask"),
        _assistant(tool=("Task", {"subagent_type": "oracle"})),
        _user_prompt("second ask"),
        _assistant(text="I'm stuck."),
    ]
    assert not oracle_consulted_this_turn(entries)


def test_consulted_false_for_other_agents():
    entries = [
        _user_prompt("fix"),
        _assistant(tool=("Task", {"subagent_type": "general-purpose"})),
    ]
    assert not oracle_consulted_this_turn(entries)


def test_tool_results_do_not_count_as_user_prompts():
    # tool_result user entries must not reset the turn boundary
    entries = [
        _user_prompt("fix"),
        _assistant(tool=("Task", {"subagent_type": "oracle"})),
        _user_tool_result(),
        _assistant(text="still this turn"),
    ]
    assert oracle_consulted_this_turn(entries)
