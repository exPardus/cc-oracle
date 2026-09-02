// Package hook holds the two entry points the harness invokes: the Stop-hook
// safety net and the SessionStart doctrine injection.
//
// Failure posture, inherited from the Python original: nothing may escape.
// An uncaught panic would exit nonzero, and hooks.json turns a nonzero exit
// into a re-run of the hook, so both entry points recover and fall back to
// (0, "") — silent, session untouched.
package hook

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/exPardus/cc-oracle/internal/config"
	"github.com/exPardus/cc-oracle/internal/detect"
	"github.com/exPardus/cc-oracle/internal/pyjson"
	"github.com/exPardus/cc-oracle/internal/state"
	"github.com/exPardus/cc-oracle/internal/transcript"
)

// Nudge and Doctrine are copied verbatim from the Python implementation. The
// differential test compares emitted bytes, so any edit here has to be made on
// both sides.
const briefTail = "Dispatch the `oracle` agent now with a full brief \u2014 Goal, Problem (errors verbatim), " +
	"Tried (attempts + why each failed), Context (files/constraints), Question (specific ask) \u2014 " +
	"then implement its plan."

// Nudge is the wording used when the triggering sentence cannot be isolated.
const Nudge = "You stated uncertainty this turn without consulting the oracle. " + briefTail

// quoteLimit is long enough to carry the claim, short enough that the nudge
// stays scannable.
const quoteLimit = 160

// quoteForNudge collapses a matched sentence into a short quotable fragment.
// Double quotes fold to single so the fragment cannot terminate the quoting
// around it, and whitespace collapses so a multi-line statement does not
// sprawl across the nudge.
func quoteForNudge(sentence string) string {
	text := strings.TrimSpace(whitespaceRun.ReplaceAllString(sentence, " "))
	text = strings.ReplaceAll(text, `"`, "'")
	if len([]rune(text)) > quoteLimit {
		text = strings.TrimRight(string([]rune(text)[:quoteLimit]), " ") + "..."
	}
	return text
}

// MarkerNudge is the nudge for a stated-uncertainty trigger, quoting the
// model's own words back so the brief starts from something concrete.
func MarkerNudge(sentence string) string {
	quoted := quoteForNudge(sentence)
	if quoted == "" {
		return Nudge
	}
	return `You stated uncertainty this turn without consulting the oracle: "` + quoted + `". ` + briefTail
}

func formatTools(tools []string) string {
	var seen []string
	for _, t := range tools {
		if t == "" {
			continue
		}
		dup := false
		for _, s := range seen {
			if s == t {
				dup = true
				break
			}
		}
		if !dup {
			seen = append(seen, t)
		}
	}
	if len(seen) == 0 {
		return "unknown"
	}
	return strings.Join(seen, ", ")
}

// StreakNudge is the nudge for the behavioral trigger: repeated tool failures
// with no uncertainty stated at all.
func StreakNudge(count int, tools []string) string {
	return strconv.Itoa(count) + " consecutive tool failures this turn (" +
		formatTools(tools) + ") without consulting the oracle. " + briefTail
}

const Doctrine = `<oracle-plugin>
Uncertainty is a signal, not a failure. The moment you notice you are unsure, stuck, confused, or going in circles: do NOT keep attempting solo and pollute your context — dispatch the ` + "`oracle`" + ` agent first, then implement its plan yourself.
When summoning the oracle, ALWAYS send a full brief: Goal (what the task ultimately wants), Problem (the exact blocker, errors quoted verbatim), Tried (attempts made + why each failed), Context (relevant files/paths, versions, platform, project rules), Question (the specific ask, not "help"). The oracle shares none of your context — a thin brief wastes the consult and forces a second round-trip.
If the dispatch errors for any reason, retry the same Agent call once with model "opus".
This applies at every tier: strong models may consult the oracle for a fresh-context second opinion.
</oracle-plugin>`

// FlushDelays is the flush-race backoff schedule. Worst case it adds the sum
// (350ms, nearer 400ms on Windows, whose ~15.6ms timer granularity rounds every
// sleep up) to turns that genuinely end without assistant text — rare, and far
// cheaper than silently missing a stuck turn. A live incident measured ~50ms of
// lag with 7 Stop hooks running concurrently, and contention is exactly what
// inflates flush lag, so that figure is a floor rather than a typical case.
var FlushDelays = []time.Duration{
	50 * time.Millisecond,
	100 * time.Millisecond,
	200 * time.Millisecond,
}

// sleep is indirected so tests can drive the race deterministically without
// touching process-wide state.
var sleep = time.Sleep

// RunStop implements the Stop hook. It returns (exitCode, stdout) and is
// total: any unexpected input yields (0, "").
func RunStop(stdinText string) (code int, out string) {
	defer func() {
		if r := recover(); r != nil {
			code, out = 0, ""
		}
	}()

	if config.DisabledByEnv() {
		return 0, ""
	}

	// Windows pipes can prepend a UTF-8 BOM. Python lstrips every leading one.
	payload, ok := decodeObject(strings.TrimLeft(stdinText, "\uFEFF"))
	if !ok {
		return 0, ""
	}

	// Python tests `is True`, so only a JSON boolean true suppresses. The
	// string "false" must not suppress, and neither must the string "true".
	if isJSONTrue(payload["stop_hook_active"]) {
		return 0, ""
	}

	cfg := config.Load()
	if !cfg.StopHook {
		return 0, ""
	}

	transcriptPath := jsonStringOnly(payload["transcript_path"])
	if transcriptPath == "" {
		return 0, ""
	}

	// Scan the CURRENT turn only: a stuck statement from a previous turn was
	// already stop-checked and must not retrigger. LoadTurn returns exactly
	// LoadEntries(path)[TurnStart(...):] without parsing the entries before the
	// boundary. It is empty only when the whole transcript is, so this also
	// stands in for Python's "no entries at all" check.
	entries := transcript.LoadTurn(transcriptPath)
	if len(entries) == 0 {
		return 0, ""
	}

	// The harness hands us the turn's final assistant text directly. It comes
	// from memory rather than disk, so it is never the stale copy the flush-race
	// retry existed to work around -- when it is present, that whole backoff
	// budget is skipped. The transcript is still read, for the consult check and
	// the failure streak, but nothing waits on it.
	text := jsonStringOnly(payload["last_assistant_message"])
	if strings.TrimSpace(text) == "" {
		// Older harnesses omit the field. Fall back to the transcript and to the
		// bounded re-read that covers its flush race: the harness can fire Stop
		// before the turn's final text reaches disk. See TurnTailFlushed for why
		// the probe is the turn's LAST assistant entry, not "any text in turn".
		for _, delay := range FlushDelays {
			if transcript.TurnTailFlushed(entries) {
				break
			}
			sleep(delay)
			// Recomputing the boundary is deliberate: if the user started a new
			// turn during the wait, LoadTurn moves past the old one and the stuck
			// statement goes unnudged. That is the fail-open direction.
			fresh := transcript.LoadTurn(transcriptPath)
			// An empty re-read means a torn or locked file mid-write, exactly what
			// the retry exists to ride out. Keep the entries already held and
			// spend the next delay rather than abandoning the budget.
			if len(fresh) > 0 {
				entries = fresh
			}
		}
		text = transcript.LastAssistantText(entries)
	}

	markers := config.EffectiveMarkers(cfg)
	stripped := detect.StripQuoted(text)
	// A turn that ends by asking the user something is legitimate, whatever else
	// happened in it. This exemption governs BOTH triggers.
	if detect.IsQuestionTurn(stripped, markers) {
		return 0, ""
	}

	var reason string
	if sentence := detect.FirstMarkerSentence(stripped, markers); sentence != "" {
		reason = MarkerNudge(sentence)
	} else if cfg.FailureStreak > 0 {
		if count, tools := transcript.FailureStreak(entries); count >= cfg.FailureStreak {
			reason = StreakNudge(count, tools)
		}
	}
	if reason == "" {
		return 0, ""
	}

	if transcript.OracleConsultedThisTurn(entries) {
		return 0, ""
	}

	sessionID := pythonStr(payload["session_id"], "unknown")
	promptID := truthyPythonStr(payload["prompt_id"])
	if promptID != "" {
		blocked, usable := state.BlockState(sessionID, promptID, cfg.StateDir, config.OracleDataDir())
		// A record that decodes to a non-object makes Python's _already_blocked
		// raise AttributeError, which its own except (OSError, ValueError) does
		// not catch; it reaches run_stop's catch-all and the hook goes silent
		// without blocking. Reproduce that rather than blocking.
		if !usable {
			return 0, ""
		}
		if blocked {
			return 0, ""
		}
		state.RecordBlock(sessionID, promptID, cfg.StateDir, config.OracleDataDir())
	}

	encoded, err := pyjson.Marshal(pyjson.Object{
		{Key: "decision", Value: "block"},
		{Key: "reason", Value: reason},
	})
	if err != nil {
		return 0, ""
	}
	return 0, string(encoded)
}

// RunSessionStart injects the doctrine. Its failure posture is the INVERSE of
// RunStop's: config trouble means the doctrine IS injected, since that is the
// default behavior. Only an explicit, well-formed opt-out silences it.
//
// The stdin payload is accepted but unused — the config location is
// environment-independent.
func RunSessionStart(stdinText string) (code int, out string) {
	defer func() {
		if r := recover(); r != nil {
			// Inverse posture: still emit the doctrine if we can, since that is
			// the default. Only fall silent if even encoding fails.
			code, out = 0, doctrineEnvelope()
		}
	}()
	_ = stdinText

	if config.DisabledByEnv() {
		return 0, ""
	}
	if !config.Load().Doctrine {
		return 0, ""
	}
	return 0, doctrineEnvelope()
}

func doctrineEnvelope() string {
	encoded, err := pyjson.Marshal(pyjson.Object{
		{Key: "hookSpecificOutput", Value: pyjson.Object{
			{Key: "hookEventName", Value: "SessionStart"},
			{Key: "additionalContext", Value: Doctrine},
		}},
	})
	if err != nil {
		return ""
	}
	return string(encoded)
}

// --- payload helpers ---------------------------------------------------------

// decodeObject mirrors json.loads + isinstance(payload, dict): a document that
// is valid JSON but not an object is rejected, as is malformed JSON.
func decodeObject(s string) (map[string]json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return nil, false
	}
	// A JSON null unmarshals into a nil map without error; Python's isinstance
	// check rejects None, so reject it here too.
	if obj == nil {
		return nil, false
	}
	return obj, true
}

func isJSONTrue(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "true"
}

// jsonStringOnly returns the value only when it is a JSON string, mirroring the
// places where Python would go on to use it as a path.
func jsonStringOnly(raw json.RawMessage) string {
	if len(raw) == 0 || raw[0] != '"' {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// pythonStr reproduces str(payload.get(key, fallback)) for the scalar shapes a
// session id can realistically take.
//
// This only affects which state file a session maps to, and both
// implementations are internally consistent, so an inexact rendering of an
// exotic value cannot change a decision. It matters only in that two distinct
// ids must not collide here while staying distinct in Python — hence null
// rendering as "None" rather than "".
func pythonStr(raw json.RawMessage, fallback string) string {
	trimmed := string(bytes.TrimSpace(raw))
	switch {
	case len(trimmed) == 0:
		return fallback // key absent
	case trimmed == "null":
		return "None"
	case trimmed == "true":
		return "True"
	case trimmed == "false":
		return "False"
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return fallback
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		// A JSON number with no '.', 'e' or 'E' decodes to a Python int, which
		// str() renders exactly as written.
		if !strings.ContainsAny(trimmed, ".eE") {
			return trimmed
		}
		// Anything else decodes to a Python float, and str() of an integral
		// float keeps its trailing ".0" — so 1e3 renders "1000.0" rather than
		// "1000" and stays distinct from the integer 1000.
		s := strconv.FormatFloat(f, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eEni") { // n, i: nan and inf spellings
			s += ".0"
		}
		return s
	}
	// Containers: not reachable for a real session id, and any stable rendering
	// preserves distinctness, which is all this value is used for.
	return trimmed
}

// truthyPythonStr reproduces `payload.get("prompt_id") or ""` followed by
// `if prompt_id:` — the value is used only when Python would consider it truthy.
func truthyPythonStr(raw json.RawMessage) string {
	trimmed := string(bytes.TrimSpace(raw))
	switch trimmed {
	case "", "null", "false", "0", `""`, "[]", "{}":
		return ""
	}
	// Numeric zero in any spelling is falsy.
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil && f == 0 {
		return ""
	}
	return pythonStr(raw, "")
}

// whitespaceRun mirrors Python's `\s+` on str patterns, which is Unicode-aware
// where Go's `\s` is ASCII-only.
var whitespaceRun = regexp.MustCompile(`[\s\p{Z}\x{0085}\x{000B}]+`)
