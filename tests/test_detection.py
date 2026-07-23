import sys
from pathlib import Path

import pytest

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


# --- deflection variant families (live-smoke: models phrase stuckness in
# --- idioms the original marker list never matched, e.g. "hit brick wall") ---

@pytest.mark.parametrize("text", [
    # brick wall family (documented live deflection)
    "I hit a brick wall trying to trace the leak.",
    "I hit the brick wall on this refactor.",
    "I hit brick wall on step 3.",
    "I'm hitting a brick wall with this linker error.",
    "I kept hitting the brick wall on the auth flow.",
    # dead end family
    "I'm at a dead end with this stack trace.",
    "I hit a dead end debugging the race.",
    "I reached a dead end on the migration.",
    # stumped / at a loss / out of ideas
    "I'm stumped by this segfault.",
    "I am stumped. The trace makes no sense.",
    "I'm at a loss with this flaky test.",
    "I am at a loss here.",
    "I'm out of ideas on the deadlock.",
    "I'm running out of ideas for this build failure.",
    # circles family (doctrine's own language)
    "I keep going in circles on this config issue.",
    "I've been going around in circles with the types.",
    "I'm going round in circles trying to reproduce it.",
    # British "work out" variant of figure-out
    "I can't work out where the config is loaded.",
    "I cannot work out why the mock never fires.",
    # no-idea family
    "I have no idea how to unblock this build.",
    "I have no idea why the pipeline segfaults.",
])
def test_marker_variant_families_fire(text):
    assert should_nudge(text)


# Intensifier adverbs between pronoun and idiom are normal first-person
# stuckness phrasing and must not defeat the anchor (re-review repros). The
# allowlist is explicit non-negating adverbs — NOT \w+ — so "not" keeps
# breaking adjacency.
@pytest.mark.parametrize("text", [
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
])
def test_adverb_tolerant_families_fire(text):
    assert should_nudge(text)


@pytest.mark.parametrize("text", [
    "I do not have any idea generator wired up yet, but the stub works. Done.",
    "I am not out of ideas yet — next I will bisect the failing commit again.",
    "I have not hit a brick wall; progress is steady. Continuing.",
])
def test_negation_still_breaks_adjacency(text):
    assert not should_nudge(text)


# "no idea how <duration/quantity>" is a benign hedge about an unknown
# quantity, not stuckness on the task (re-review repro).
@pytest.mark.parametrize("text", [
    "I have no idea how long the full build takes on CI, so I set the timeout to 60 minutes. Done.",
    "I have no idea how many rows the table holds in prod, so the migration batches by 1000. Shipped.",
    "I have no idea how much memory the worker peaks at, so I set a conservative limit. Done.",
    "I have no idea how big the upload can get, so the handler streams to disk. Done.",
    "I have no idea how often the cron fires in staging, so I added idempotency. Done.",
])
def test_no_idea_quantity_hedges_do_not_fire(text):
    assert not should_nudge(text)


@pytest.mark.parametrize("text", [
    "We built a brick wall texture for the level. Done.",
    "The street is a dead end; the depot sits at its end. Route mapped.",
    "Everything worked out fine after the rebase.",
    "The workout routine parser now passes all tests.",
    "These ideas are out of scope for v1. Shipped the rest.",
    "The loop iterates in circles of radius r. Implemented.",
])
def test_variant_near_misses_do_not_fire(text):
    assert not should_nudge(text)


# Idiom families must be anchored to first-person present stuckness: benign
# third-person, negated, past-resolved, or meta-mention uses must not block
# (review repros). Doctrine: a miss beats a false positive.
@pytest.mark.parametrize("text", [
    "The DFS backtracks whenever it has hit a dead end, which is expected.",
    "The animation keeps the icons going in circles as designed.",
    "The user had no idea how the crash happened, so I added logging.",
    "I am not out of ideas yet — next I will bisect the failing commit.",
    "This plugin matches phrases like hit a brick wall in assistant text.",
    "The maze solver marks a cell dead when the walker has reached a dead end.",
    "Users who are out of ideas can consult the docs. Shipped.",
])
def test_anchored_families_ignore_benign_uses(text):
    assert not should_nudge(text)
