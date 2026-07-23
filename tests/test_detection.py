import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "hooks"))

from oracle_hook import marker_hit, is_question_turn, should_nudge


# --- marker_hit ---

def test_marker_hit_im_not_sure():
    assert marker_hit("I'm not sure why this test fails.")

def test_marker_hit_case_insensitive():
    assert marker_hit("I AM STUCK on this segfault.")

def test_marker_hit_cant_figure_out():
    assert marker_hit("I can't figure out where the config is loaded.")

def test_marker_hit_negative_plain_text():
    assert not marker_hit("All tests pass. Done.")

def test_marker_hit_negative_near_miss():
    # "sure" alone, "not sure" inside other words must not fire
    assert not marker_hit("Make sure the tests pass. This is not surprising.")


# --- is_question_turn (asking-the-user suppression) ---

def test_question_final_sentence():
    assert is_question_turn("I'm not sure which option fits. Do you prefer A or B?")

def test_question_marker_sentence_itself():
    assert is_question_turn("I'm not sure which one you want — A or B?  Meanwhile I'll wait.")

def test_not_question_plain_statement():
    assert not is_question_turn("I'm stuck. The build fails with a linker error.")


# --- should_nudge (composition) ---

def test_nudge_on_stuck_statement():
    assert should_nudge("I'm stuck. The mock never gets called and I can't figure out why.")

def test_no_nudge_when_asking_user():
    assert not should_nudge("I'm not sure what scope you want here — full rewrite or patch?")

def test_no_nudge_without_marker():
    assert not should_nudge("Refactored the parser; all 34 tests pass.")

def test_no_nudge_empty_text():
    assert not should_nudge("")


# --- quoted/fenced-text exemption (quoting is not stating) ---

def test_no_nudge_marker_inside_code_fence():
    text = "Fixed. The old error was:\n```\nI'm not sure what to do here\n```\nAll tests pass now."
    assert not should_nudge(text)

def test_no_nudge_marker_inside_inline_code():
    assert not should_nudge("The log line `cannot figure out the encoding` is expected and harmless.")

def test_no_nudge_marker_inside_double_quotes():
    assert not should_nudge('The marker list includes "I\'m not sure", quoted here, not stated.')

def test_nudge_survives_stripping_when_genuinely_stuck():
    # markers OUTSIDE quoted spans must still fire
    assert should_nudge("I'm stuck. The command `npm test` fails and I can't figure out why.")


def test_marker_hit_survives_newline_split():
    assert marker_hit("I'm not\nsure why this fails.")

def test_marker_hit_survives_double_space():
    assert marker_hit("I'm  not sure why this fails.")


def test_nudge_marker_between_closed_fences_survives():
    # markers BETWEEN complete fence pairs are genuine statements, not quotes
    text = "```\ncode a\n```\nI'm stuck on this linker error.\n```\ncode b\n```"
    assert should_nudge(text)


def test_no_nudge_marker_inside_blockquote():
    text = "The reviewer wrote:\n> I'm not sure this is right\nI disagree; the code is fine."
    assert not should_nudge(text)

def test_no_nudge_marker_inside_nested_blockquote():
    assert not should_nudge("Quoting the thread:\n> > I am stuck on this\nResolved upstream.")

def test_nudge_marker_outside_blockquote_survives():
    assert should_nudge("> old log line, irrelevant\nI'm stuck on this linker error.")

def test_nudge_midline_gt_is_not_blockquote():
    assert should_nudge("The assert 3 > 2 holds, yet I'm stuck on the failing test.")
