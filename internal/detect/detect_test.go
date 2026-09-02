package detect

import (
	"strings"
	"testing"
)

// Ported one-for-one from tests/test_detection.py. Every assertion there,
// including every entry of every @pytest.mark.parametrize list, has a case
// here — each phrase is a regression someone paid for.

type textCase struct {
	name string
	text string
	want bool
}

// expect turns a parametrize list into table cases, using the phrase itself as
// the subtest name so a failure names the regression directly.
func expect(phrases []string, want bool) []textCase {
	cases := make([]textCase, 0, len(phrases))
	for _, p := range phrases {
		cases = append(cases, textCase{name: p, text: p, want: want})
	}
	return cases
}

func runMarkerHit(t *testing.T, cases []textCase) {
	t.Helper()
	markers := DefaultMarkerSet()
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := MarkerHit(c.text, markers); got != c.want {
				t.Errorf("MarkerHit(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

func runIsQuestionTurn(t *testing.T, cases []textCase) {
	t.Helper()
	markers := DefaultMarkerSet()
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := IsQuestionTurn(c.text, markers); got != c.want {
				t.Errorf("IsQuestionTurn(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

func runShouldNudge(t *testing.T, cases []textCase) {
	t.Helper()
	markers := DefaultMarkerSet()
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldNudge(c.text, markers); got != c.want {
				t.Errorf("ShouldNudge(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

// --- marker_hit ---

func TestMarkerHit(t *testing.T) {
	runMarkerHit(t, []textCase{
		{"im_not_sure", "I'm not sure why this test fails.", true},
		{"case_insensitive", "I AM STUCK on this segfault.", true},
		{"cant_figure_out", "I can't figure out where the config is loaded.", true},
		{"negative_plain_text", "All tests pass. Done.", false},
		// "sure" alone, "not sure" inside other words must not fire.
		{"negative_near_miss", "Make sure the tests pass. This is not surprising.", false},
		{"survives_newline_split", "I'm not\nsure why this fails.", true},
		{"survives_double_space", "I'm  not sure why this fails.", true},
	})
}

// --- is_question_turn (asking-the-user suppression) ---

func TestIsQuestionTurn(t *testing.T) {
	runIsQuestionTurn(t, []textCase{
		{"question_final_sentence", "I'm not sure which option fits. Do you prefer A or B?", true},
		{"question_marker_sentence_itself", "I'm not sure which one you want — A or B?  Meanwhile I'll wait.", true},
		{"not_question_plain_statement", "I'm stuck. The build fails with a linker error.", false},
	})
}

// --- should_nudge (composition) ---

func TestShouldNudge(t *testing.T) {
	runShouldNudge(t, []textCase{
		{"nudge_on_stuck_statement", "I'm stuck. The mock never gets called and I can't figure out why.", true},
		{"no_nudge_when_asking_user", "I'm not sure what scope you want here — full rewrite or patch?", false},
		{"no_nudge_without_marker", "Refactored the parser; all 34 tests pass.", false},
		{"no_nudge_empty_text", "", false},
	})
}

// --- quoted/fenced-text exemption (quoting is not stating) ---

func TestShouldNudgeQuotingExemption(t *testing.T) {
	runShouldNudge(t, []textCase{
		{
			"no_nudge_marker_inside_code_fence",
			"Fixed. The old error was:\n```\nI'm not sure what to do here\n```\nAll tests pass now.",
			false,
		},
		{
			"no_nudge_marker_inside_inline_code",
			"The log line `cannot figure out the encoding` is expected and harmless.",
			false,
		},
		{
			"no_nudge_marker_inside_double_quotes",
			`The marker list includes "I'm not sure", quoted here, not stated.`,
			false,
		},
		// markers OUTSIDE quoted spans must still fire
		{
			"nudge_survives_stripping_when_genuinely_stuck",
			"I'm stuck. The command `npm test` fails and I can't figure out why.",
			true,
		},
		// markers BETWEEN complete fence pairs are genuine statements, not quotes
		{
			"nudge_marker_between_closed_fences_survives",
			"```\ncode a\n```\nI'm stuck on this linker error.\n```\ncode b\n```",
			true,
		},
		{
			"no_nudge_marker_inside_blockquote",
			"The reviewer wrote:\n> I'm not sure this is right\nI disagree; the code is fine.",
			false,
		},
		{
			"no_nudge_marker_inside_nested_blockquote",
			"Quoting the thread:\n> > I am stuck on this\nResolved upstream.",
			false,
		},
		{
			"nudge_marker_outside_blockquote_survives",
			"> old log line, irrelevant\nI'm stuck on this linker error.",
			true,
		},
		{
			"nudge_midline_gt_is_not_blockquote",
			"The assert 3 > 2 holds, yet I'm stuck on the failing test.",
			true,
		},
	})
}

// --- deflection variant families (live-smoke: models phrase stuckness in
// --- idioms the original marker list never matched, e.g. "hit brick wall") ---

func TestMarkerVariantFamiliesFire(t *testing.T) {
	phrases := []string{
		// brick wall family (documented live deflection)
		"I hit a brick wall trying to trace the leak.",
		"I hit the brick wall on this refactor.",
		"I hit brick wall on step 3.",
		"I'm hitting a brick wall with this linker error.",
		"I kept hitting the brick wall on the auth flow.",
		// dead end family
		"I'm at a dead end with this stack trace.",
		"I hit a dead end debugging the race.",
		"I reached a dead end on the migration.",
		// stumped / at a loss / out of ideas
		"I'm stumped by this segfault.",
		"I am stumped. The trace makes no sense.",
		"I'm at a loss with this flaky test.",
		"I am at a loss here.",
		"I'm out of ideas on the deadlock.",
		"I'm running out of ideas for this build failure.",
		// circles family (doctrine's own language)
		"I keep going in circles on this config issue.",
		"I've been going around in circles with the types.",
		"I'm going round in circles trying to reproduce it.",
		// British "work out" variant of figure-out
		"I can't work out where the config is loaded.",
		"I cannot work out why the mock never fires.",
		// no-idea family
		"I have no idea how to unblock this build.",
		"I have no idea why the pipeline segfaults.",
	}
	runShouldNudge(t, expect(phrases, true))
}

// Intensifier adverbs between pronoun and idiom are normal first-person
// stuckness phrasing and must not defeat the anchor (re-review repros). The
// allowlist is explicit non-negating adverbs — NOT \w+ — so "not" keeps
// breaking adjacency.
func TestAdverbTolerantFamiliesFire(t *testing.T) {
	phrases := []string{
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
	}
	runShouldNudge(t, expect(phrases, true))
}

func TestNegationStillBreaksAdjacency(t *testing.T) {
	phrases := []string{
		"I do not have any idea generator wired up yet, but the stub works. Done.",
		"I am not out of ideas yet — next I will bisect the failing commit again.",
		"I have not hit a brick wall; progress is steady. Continuing.",
	}
	runShouldNudge(t, expect(phrases, false))
}

// "no idea how <duration/quantity>" is a benign hedge about an unknown
// quantity, not stuckness on the task (re-review repro).
func TestNoIdeaQuantityHedgesDoNotFire(t *testing.T) {
	phrases := []string{
		"I have no idea how long the full build takes on CI, so I set the timeout to 60 minutes. Done.",
		"I have no idea how many rows the table holds in prod, so the migration batches by 1000. Shipped.",
		"I have no idea how much memory the worker peaks at, so I set a conservative limit. Done.",
		"I have no idea how big the upload can get, so the handler streams to disk. Done.",
		"I have no idea how often the cron fires in staging, so I added idempotency. Done.",
	}
	runShouldNudge(t, expect(phrases, false))
}

func TestVariantNearMissesDoNotFire(t *testing.T) {
	phrases := []string{
		"We built a brick wall texture for the level. Done.",
		"The street is a dead end; the depot sits at its end. Route mapped.",
		"Everything worked out fine after the rebase.",
		"The workout routine parser now passes all tests.",
		"These ideas are out of scope for v1. Shipped the rest.",
		"The loop iterates in circles of radius r. Implemented.",
	}
	runShouldNudge(t, expect(phrases, false))
}

// Idiom families must be anchored to first-person present stuckness: benign
// third-person, negated, past-resolved, or meta-mention uses must not block
// (review repros). Doctrine: a miss beats a false positive.
func TestAnchoredFamiliesIgnoreBenignUses(t *testing.T) {
	phrases := []string{
		"The DFS backtracks whenever it has hit a dead end, which is expected.",
		"The animation keeps the icons going in circles as designed.",
		"The user had no idea how the crash happened, so I added logging.",
		"I am not out of ideas yet — next I will bisect the failing commit.",
		"This plugin matches phrases like hit a brick wall in assistant text.",
		"The maze solver marks a cell dead when the walker has reached a dead end.",
		"Users who are out of ideas can consult the docs. Shipped.",
	}
	runShouldNudge(t, expect(phrases, false))
}

// --- lookahead emulation (new code: RE2 has no (?!...), so the exclusion is
// --- reapplied per match site rather than as a veto over the whole text) ---

func TestNoIdeaHedgeIsPerMatchSiteNotPerMessage(t *testing.T) {
	runShouldNudge(t, []textCase{
		// A hedge sharing a message with a real marker must NOT suppress it.
		// A whole-text veto regex would wrongly return false here.
		{
			"hedge_plus_plain_marker",
			"I have no idea how long CI takes. I'm stuck on the linker error.",
			true,
		},
		// Both halves come from the same family: the hedged site is skipped,
		// scanning continues, and the genuine "no idea why" still fires.
		{
			"hedge_plus_same_family_hit",
			"I have no idea how long CI takes, and I have no idea why the pipeline segfaults.",
			true,
		},
		// Two "how" sites in one sentence: the first is hedged, the second is not.
		{
			"hedged_how_then_genuine_how",
			"I have no idea how many rows exist and I have no idea how to fix the query.",
			true,
		},
		// Ordering must not matter: genuine hit first, hedge second.
		{
			"genuine_hit_before_hedge",
			"I have no idea why the pipeline segfaults, and I have no idea how long CI takes.",
			true,
		},
		// Python's (?! long) has no word boundary of its own, so " longitude"
		// begins with " long" and the lookahead vetoes the site. Go matches
		// that behavior deliberately: no fire.
		{
			"how_longitude_matches_python_prefix_veto",
			"I have no idea how longitude is computed.",
			false,
		},
		// ...but the veto is still site-local, not message-wide.
		{
			"how_longitude_plus_real_marker",
			"I have no idea how longitude is computed, but I'm stuck on the linker error.",
			true,
		},
		// "however" fails the \b after "how" in both engines.
		{
			"no_idea_however_does_not_fire",
			"I have no idea however you slice it. Done.",
			false,
		},
	})
}

// --- API surface: marker set plumbing and normalization ---

func TestDefaultMarkerSet(t *testing.T) {
	set := DefaultMarkerSet()
	if len(set) != len(Markers) {
		t.Fatalf("DefaultMarkerSet() has %d entries, want %d", len(set), len(Markers))
	}
	for _, m := range Markers {
		if _, ok := set[m]; !ok {
			t.Errorf("DefaultMarkerSet() missing %q", m)
		}
		if got := Normalize(m); got != m {
			t.Errorf("Markers entry %q is not already normalized (got %q)", m, got)
		}
	}
	// Family keys stay in the marker set so markers.remove can drop a family.
	for key := range familyPatterns {
		if _, ok := set[key]; !ok {
			t.Errorf("family key %q missing from the default marker set", key)
		}
	}
}

func TestFamilyKeyRemovalDropsWholeFamily(t *testing.T) {
	markers := DefaultMarkerSet()
	delete(markers, "no idea how")
	if MarkerHit("I have no idea why the pipeline segfaults.", markers) {
		t.Error("removing the family key must drop the whole family")
	}
	if !MarkerHit("I'm stuck on this.", markers) {
		t.Error("removing one family must not affect other markers")
	}
}

func TestCustomMarkerIsPlainSubstring(t *testing.T) {
	markers := map[string]struct{}{"beats me": {}}
	if !MarkerHit("Honestly, beats me why it fails.", markers) {
		t.Error("a marker outside every family must match as a plain substring")
	}
	if MarkerHit("I'm stuck on this.", markers) {
		t.Error("only the supplied markers may match")
	}
}

func TestEmptyMarkerSetMatchesNothing(t *testing.T) {
	if MarkerHit("I'm stuck on this.", nil) {
		t.Error("a nil marker set must match nothing")
	}
	if MarkerHit("I'm stuck on this.", map[string]struct{}{}) {
		t.Error("an empty marker set must match nothing")
	}
	if ShouldNudge("I'm stuck on this.", nil) {
		t.Error("ShouldNudge with no markers must be false")
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"lowercases", "I'm Not Sure", "i'm not sure"},
		{"trims_and_collapses", "  I'm   NOT  sure  ", "i'm not sure"},
		{"tab_and_newline", "I'm\tnot\nsure", "i'm not sure"},
		{"empty", "", ""},
		{"whitespace_only", "   ", ""},
		// Unicode whitespace: Python's \s on str collapses these, Go's
		// ASCII-only \s would not - hence [\s\p{Z}\x{0085}\x{000B}].
		{"no_break_space", "i'm\u00a0not sure", "i'm not sure"},
		{"em_space", "i'm\u2003not\u2003sure", "i'm not sure"},
		{"next_line", "i'm\u0085not sure", "i'm not sure"},
		{"vertical_tab", "i'm\vnot sure", "i'm not sure"},
		{"ideographic_space", "i'm\u3000not sure", "i'm not sure"},
		{"unicode_space_trimmed", "\u00a0i'm not sure\u00a0", "i'm not sure"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := Normalize(c.in); got != c.want {
				t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestMarkerHitCollapsesUnicodeWhitespace(t *testing.T) {
	runMarkerHit(t, []textCase{
		{"nbsp_inside_marker", "I'm\u00a0not sure why this fails.", true},
		{"em_space_inside_marker", "I'm\u2003not\u2003sure why this fails.", true},
		{"vertical_tab_inside_marker", "I'm\vnot sure why this fails.", true},
		{"next_line_inside_marker", "I'm\u0085not sure why this fails.", true},
	})
}

// --- StripQuoted ---

func TestStripQuoted(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		absent  []string
		present []string
	}{
		{
			"fenced_code",
			"before\n```\nI'm not sure\n```\nafter",
			[]string{"I'm not sure"},
			[]string{"before", "after"},
		},
		{
			"inline_code",
			"the log `cannot figure out x` is fine",
			[]string{"cannot figure out"},
			[]string{"the log", "is fine"},
		},
		{
			"double_quoted",
			`includes "I'm not sure", quoted`,
			[]string{"I'm not sure"},
			[]string{"includes", "quoted"},
		},
		{
			"blockquote_line",
			"they said:\n> I am stuck on this\nbut it is fine",
			[]string{"I am stuck"},
			[]string{"they said:", "but it is fine"},
		},
		{
			"nested_blockquote_line",
			"thread:\n> > I am stuck on this\ndone",
			[]string{"I am stuck"},
			[]string{"thread:", "done"},
		},
		{
			// Single quotes are NOT stripped: apostrophes in contractions
			// would corrupt matching.
			"single_quotes_preserved",
			"I'm stuck and 'quoted' text stays",
			nil,
			[]string{"I'm stuck", "'quoted'"},
		},
		{
			// Mid-line ">" is a comparison or a shell redirect, not quoting.
			"midline_gt_preserved",
			"the assert 3 > 2 holds, yet I am stuck",
			nil,
			[]string{"I am stuck"},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := StripQuoted(c.in)
			for _, s := range c.absent {
				if strings.Contains(got, s) {
					t.Errorf("StripQuoted(%q) = %q, must not contain %q", c.in, got, s)
				}
			}
			for _, s := range c.present {
				if !strings.Contains(got, s) {
					t.Errorf("StripQuoted(%q) = %q, must contain %q", c.in, got, s)
				}
			}
		})
	}
}

// --- sentence split (hand-rolled: RE2 has no (?<=[.!?]) lookbehind) ---

func TestSentences(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace_only", "   \n\t ", nil},
		{"no_punctuation", "just a fragment", []string{"just a fragment"}},
		{"single_sentence", "Done.", []string{"Done."}},
		{"trims_input", "  Done. Shipped.  ", []string{"Done.", "Shipped."}},
		{"two_sentences", "A. B.", []string{"A.", "B."}},
		{"double_space", "A.  B.", []string{"A.", "B."}},
		{"newline_break", "A.\nB.", []string{"A.", "B."}},
		{"bang_and_question", "Wow! Really? Yes.", []string{"Wow!", "Really?", "Yes."}},
		// Punctuation not followed by whitespace never splits: decimals,
		// ellipses and abbreviations stay in one piece.
		{"decimal_not_split", "Version 1.2 shipped.", []string{"Version 1.2 shipped."}},
		{"ellipsis_no_space", "Wait...done.", []string{"Wait...done."}},
		{"ellipsis_with_space", "Wait... done.", []string{"Wait...", "done."}},
		{"unicode_space_break", "A.\u00a0B.", []string{"A.", "B."}},
		{"trailing_punctuation_only", "A. B", []string{"A.", "B"}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := sentences(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("sentences(%q) = %q, want %q", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("sentences(%q) = %q, want %q", c.in, got, c.want)
				}
			}
		})
	}
}

func TestIsQuestionTurnEdgeCases(t *testing.T) {
	runIsQuestionTurn(t, []textCase{
		{"empty", "", false},
		{"whitespace_only", "   ", false},
		{"bare_question", "Which one?", true},
		{"trailing_whitespace_after_question", "Which one?  \n ", true},
		// A question that carries no marker, followed by a statement, is not a
		// question turn: only the LAST sentence, or a marker-bearing question,
		// counts.
		{"non_marker_question_then_statement", "Ready? All tests pass.", false},
		{"marker_question_then_statement", "I'm stuck, right? Moving on.", true},
		{"statement_only", "I'm stuck. The build fails.", false},
	})
}

// --- progress-stall families (added after mining 1,741 real turns) ----------

func TestProgressStallFamiliesFire(t *testing.T) {
	for _, text := range []string{
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
	} {
		t.Run(text, func(t *testing.T) {
			if !ShouldNudge(text, DefaultMarkerSet()) {
				t.Error("expected a nudge")
			}
		})
	}
}

func TestProgressStallNearMissesDoNotFire(t *testing.T) {
	for _, text := range []string{
		// third person: the subject is not the model
		"The build is not making progress because the queue is paused. Fixed.",
		"The retry keeps getting the same result, which is correct. Done.",
		"The migration still cannot run on Postgres 12; documented that. Shipped.",
		// negated
		"I am not out of ideas, and I am making progress. Continuing.",
		// benign uses of the same words
		"This did not get anywhere near the memory limit. Shipped.",
		"Progress bars now render correctly. Done.",
		// question exemption still wins
		"I still can't tell which layout you prefer - A or B?",
	} {
		t.Run(text, func(t *testing.T) {
			if ShouldNudge(text, DefaultMarkerSet()) {
				t.Error("expected silence")
			}
		})
	}
}

func TestFirstMarkerSentence(t *testing.T) {
	set := DefaultMarkerSet()
	cases := []struct{ in, want string }{
		{"Refactored the parser. I'm stuck on the failing mock. Tests still red.",
			"I'm stuck on the failing mock."},
		{"All tests pass. Done.", ""},
		{"I'm not\nsure why this fails", "I'm not\nsure why this fails"},
	}
	for _, c := range cases {
		if got := FirstMarkerSentence(c.in, set); got != c.want {
			t.Errorf("FirstMarkerSentence(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
