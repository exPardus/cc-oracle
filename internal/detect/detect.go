// Package detect ports the stuckness-marker detection of hooks/oracle_hook.py.
//
// Behavior is frozen: the Python source is the specification. Two of its
// patterns lean on PCRE features RE2 does not have, and both are restructured
// rather than approximated — see noIdeaFamily (lookahead) and sentences
// (lookbehind). Python's `\s` on str is Unicode-aware while Go's is ASCII
// only, so every whitespace class here is spelled [\s\p{Z}\x{0085}\x{000B}]
// (equivalently unicode.IsSpace for the hand-rolled scans).
//
// This package has no dependencies outside the standard library and imports
// no other package of this module: it is the leaf of the phase-1 layout.
//
// All regexes are compiled once at package init. This binary runs on every
// turn of every session; per-call compilation is exactly the cost the rewrite
// exists to remove.
package detect

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Markers is the baseline marker set, in the Python source's order.
//
// Conservative by design: a false positive (annoying block) is worse than a
// miss. Entries from "hit a brick wall" down are FAMILY KEYS, not substrings —
// each maps to a first-person-anchored regex in familyPatterns, so benign
// third-person / negated / past-resolved / meta-mention uses never match. The
// keys stay in this list so the config markers.remove knob can drop a whole
// family like any core marker.
var Markers = []string{
	"i'm not sure",
	"i am not sure",
	"i'm unsure",
	"i am unsure",
	"i'm stuck",
	"i am stuck",
	"can't figure out",
	"cannot figure out",
	"i'm confused",
	"i am confused",
	"not certain why",
	"unsure how to proceed",
	// Deflection variant families.
	"hit a brick wall",
	"at a dead end",
	"i'm stumped",
	"i'm at a loss",
	"out of ideas",
	"going in circles",
	"can't work out",
	"no idea how",
}

// adv is the first-person anchoring rule's adverb slot: the pronoun must sit
// against the idiom's verb phrase with at most an explicit NON-NEGATING
// intensifier between them ("I really have no idea why", "I'm completely out
// of ideas"). It is a closed allowlist — never \w+ — because that is what
// keeps "not" breaking adjacency, so "I am not out of ideas" cannot match and
// third-party subjects ("I think the DFS hit a dead end") still cannot match.
const adv = `(?:really |completely |totally |honestly |just |simply |absolutely |genuinely )?`

// noIdeaFamily is the one family whose Python pattern uses a negative
// lookahead. Its regex below is written WITHOUT the lookahead, capturing the
// interrogative it ended on; familyMatcher.search re-imposes the exclusion by
// inspecting the text after each match site. See the type's comment.
const noIdeaFamily = "no idea how"

// noIdeaHedges is Python's (?! long| many| much| big| often), leading space
// included: "how long/many/much/big/often" is a hedge about an unknown
// quantity, not stuckness on the task.
var noIdeaHedges = []string{" long", " many", " much", " big", " often"}

var familyPatterns = map[string]string{
	"hit a brick wall": `\b(?:i|we)(?:'ve| have)? ` + adv + `(?:hit|kept hitting) (?:a |the )?brick wall\b` +
		`|\b(?:i'm|i am|we're|we are) ` + adv + `hitting (?:a |the )?brick wall\b` +
		`|\b(?:i|we) ` + adv + `keep hitting (?:a |the )?brick wall\b`,
	"at a dead end": `\b(?:i'm|i am|we're|we are) ` + adv + `at a dead end\b` +
		`|\b(?:i|we)(?:'ve| have)? ` + adv + `(?:hit|reached) a dead end\b`,
	"i'm stumped":   `\b(?:i'm|i am|we're|we are) ` + adv + `stumped\b`,
	"i'm at a loss": `\b(?:i'm|i am|we're|we are) ` + adv + `at a loss\b`,
	"out of ideas": `\b(?:i'm|i am|we're|we are) ` + adv + `(?:running )?out of ideas\b` +
		`|\b(?:i've|we've|i|we) ` + adv + `(?:run|ran) out of ideas\b`,
	"going in circles": `\b(?:i'm|i am|we're|we are) ` + adv + `going (?:around |round )?in circles\b` +
		`|\b(?:i|we) ` + adv + `keep going (?:around |round )?in circles\b` +
		`|\b(?:i|we)(?:'ve| have) ` + adv + `been going (?:around |round )?in circles\b`,
	"can't work out": `\b(?:i|we) ` + adv + `(?:can't|cannot|can not) ` + adv + `work out\b`,
	// Python: no idea (?:how(?! long| many| much| big| often)|why)\b
	// The lookahead is stripped here and reapplied per match site; the
	// interrogative is captured so the match site knows whether to check.
	noIdeaFamily: `\b(?:i|we) ` + adv + `have ` + adv + `no idea (how|why)\b` +
		`|\b(?:i've|we've) ` + adv + `(?:got )?no idea (how|why)\b`,
}

// familyMatcher wraps a family regex with the lookahead emulation.
//
// RE2 has no lookahead, and a separate veto regex over the whole text would be
// wrong: a message holding both a genuine "no idea why" and a benign "no idea
// how long" would have the real hit suppressed. Instead the pattern matches
// without the exclusion, and each match site is inspected — a match that ended
// on "how" is skipped when the text right after it begins with a hedge word,
// and scanning continues. That is exactly the lookahead, per match site.
type familyMatcher struct {
	re *regexp.Regexp
	// hedged marks the "no idea how" family: its matches need the suffix
	// check. Every other family matches outright.
	hedged bool
}

var familyRes = compileFamilies()

func compileFamilies() map[string]*familyMatcher {
	out := make(map[string]*familyMatcher, len(familyPatterns))
	for key, pat := range familyPatterns {
		out[key] = &familyMatcher{re: regexp.MustCompile(pat), hedged: key == noIdeaFamily}
	}
	return out
}

func (f *familyMatcher) search(low string) bool {
	if !f.hedged {
		return f.re.MatchString(low)
	}
	for _, m := range f.re.FindAllStringSubmatchIndex(low, -1) {
		if matchedHow(low, m) && hasHedgeSuffix(low[m[1]:]) {
			continue // quantity hedge: skip this site, keep scanning.
		}
		return true
	}
	return false
}

// matchedHow reports whether the match ended on "how" rather than "why". The
// pattern has one capture group per top-level alternative; exactly one of them
// is set for any match.
func matchedHow(low string, m []int) bool {
	for g := 1; 2*g+1 < len(m); g++ {
		if m[2*g] >= 0 {
			return low[m[2*g]:m[2*g+1]] == "how"
		}
	}
	return false
}

func hasHedgeSuffix(rest string) bool {
	for _, h := range noIdeaHedges {
		if strings.HasPrefix(rest, h) {
			return true
		}
	}
	return false
}

// wsRun is Python's \s+ for str patterns: Unicode whitespace, not Go's
// ASCII-only \s. \p{Z} plus NEL and VT round the class out to the full
// White_Space property, so non-breaking spaces and friends collapse
// identically.
var wsRun = regexp.MustCompile(`[\s\p{Z}\x{0085}\x{000B}]+`)

// Quoting is not stating: markers inside fenced code, blockquote lines,
// inline code, or double-quoted strings are exempt. Single quotes are NOT
// stripped — apostrophes in contractions ("I'm") would corrupt matching.
var (
	fencedCode   = regexp.MustCompile("(?s)```.*?```")
	inlineCode   = regexp.MustCompile("`[^`\n]*`")
	doubleQuoted = regexp.MustCompile(`"[^"\n]*"`)
	// Markdown blockquote lines (incl. nested "> >"); anchored to line start
	// so a mid-line ">" (comparisons, shell redirects) is never quoting.
	blockquoteLine = regexp.MustCompile(`(?m)^[ \t]{0,3}>[^\n]*$`)
)

// Normalize mirrors Python _normalize_marker: trim, lowercase, then collapse
// whitespace runs to a single space.
func Normalize(s string) string {
	return wsRun.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), " ")
}

// DefaultMarkerSet returns the baseline Markers as a normalized set. It exists
// so callers can get Python's default argument without detect depending on
// package config.
func DefaultMarkerSet() map[string]struct{} {
	set := make(map[string]struct{}, len(Markers))
	for _, m := range Markers {
		set[Normalize(m)] = struct{}{}
	}
	return set
}

// MarkerHit reports whether text states a stuckness marker.
//
// markers is the effective set (see config.EffectiveMarkers); entries that
// name an idiom family are matched by that family's anchored regex, all others
// by plain substring against the lowercased, whitespace-collapsed text.
// Mirrors Python marker_hit. A nil or empty set matches nothing — there is no
// implicit fallback to Markers; pass DefaultMarkerSet() for that.
func MarkerHit(text string, markers map[string]struct{}) bool {
	low := wsRun.ReplaceAllString(strings.ToLower(text), " ")
	for m := range markers {
		if fam, ok := familyRes[m]; ok {
			if fam.search(low) {
				return true
			}
		} else if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// StripQuoted removes fenced code, blockquote lines, inline code, and
// double-quoted spans, replacing each with a space — quoting a marker is not
// stating one. Single quotes are NOT stripped: apostrophes in "I'm" would
// corrupt matching.
func StripQuoted(text string) string {
	text = fencedCode.ReplaceAllString(text, " ")
	text = blockquoteLine.ReplaceAllString(text, " ")
	text = inlineCode.ReplaceAllString(text, " ")
	text = doubleQuoted.ReplaceAllString(text, " ")
	return text
}

// sentences mirrors Python _sentences: split on whitespace that FOLLOWS
// sentence punctuation, keeping the punctuation attached to the preceding
// sentence, dropping empty pieces, trimming the input first.
//
// Python spells that (?<=[.!?])\s+ and RE2 has no lookbehind, so the scan is
// by hand: find [.!?] followed by at least one whitespace rune, cut
// immediately after the punctuation, skip the whitespace run.
func sentences(text string) []string {
	t := strings.TrimSpace(text)
	if t == "" {
		return nil
	}
	var out []string
	start := 0
	// Byte-wise is safe: the delimiters are ASCII and no byte of a multi-byte
	// UTF-8 sequence is < 0x80.
	for i := 0; i < len(t); i++ {
		if c := t[i]; c != '.' && c != '!' && c != '?' {
			continue
		}
		cut := i + 1
		if cut >= len(t) {
			break
		}
		r, size := utf8.DecodeRuneInString(t[cut:])
		if !unicode.IsSpace(r) {
			continue
		}
		end := cut + size
		for end < len(t) {
			r, size := utf8.DecodeRuneInString(t[end:])
			if !unicode.IsSpace(r) {
				break
			}
			end += size
		}
		if piece := t[start:cut]; piece != "" {
			out = append(out, piece)
		}
		start = end
		i = end - 1
	}
	if start < len(t) {
		out = append(out, t[start:])
	}
	return out
}

func endsWithQuestion(s string) bool {
	return strings.HasSuffix(strings.TrimRightFunc(s, unicode.IsSpace), "?")
}

// IsQuestionTurn reports whether the turn is, or ends with, a question to the
// user. Marker-in-question always beats marker-matched: "I'm not sure which
// you prefer — A or B?" is legitimate turn-ending behavior, not flailing.
func IsQuestionTurn(text string, markers map[string]struct{}) bool {
	sents := sentences(text)
	if len(sents) == 0 {
		return false
	}
	if endsWithQuestion(sents[len(sents)-1]) {
		return true
	}
	for _, s := range sents {
		if endsWithQuestion(s) && MarkerHit(s, markers) {
			return true
		}
	}
	return false
}

// ShouldNudge is the composition: a marker outside quoted spans, and not a
// question turn.
func ShouldNudge(text string, markers map[string]struct{}) bool {
	if text == "" {
		return false
	}
	stripped := StripQuoted(text)
	return MarkerHit(stripped, markers) && !IsQuestionTurn(stripped, markers)
}
