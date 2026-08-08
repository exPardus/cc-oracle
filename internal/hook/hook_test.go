package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/exPardus/cc-oracle/internal/config"
	"github.com/exPardus/cc-oracle/internal/detect"
)

// Ports tests/test_stop_entry.py and tests/test_cli_smoke.py. Each test names
// the Python original it corresponds to.

// --- fixtures ----------------------------------------------------------------

// isolate points the plugin data dir at a temp directory whose basename
// identifies this plugin; a foreign-looking name would be rejected by the
// data-dir guard and the test would silently exercise the OS temp dir instead.
func isolate(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "oracle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_PLUGIN_DATA", dir)
	t.Setenv("CC_ORACLE_DISABLE", "")
	return dir
}

// stubSleep replaces the backoff sleep and records what was requested. Without
// it every text-less turn burns the real budget in wall clock.
func stubSleep(t *testing.T, fn func(time.Duration)) *[]time.Duration {
	t.Helper()
	var seen []time.Duration
	prev := sleep
	sleep = func(d time.Duration) {
		seen = append(seen, d)
		if fn != nil {
			fn(d)
		}
	}
	t.Cleanup(func() { sleep = prev })
	return &seen
}

func assistantText(text string) map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{
		"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}}}
}

func assistantThinking() map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{
		"role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": "hmm"}}}}
}

func assistantTool(name string, input map[string]any) map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{
		"role": "assistant", "content": []any{map[string]any{
			"type": "tool_use", "name": name, "input": input}}}}
}

func userPrompt(text string) map[string]any {
	return map[string]any{"type": "user", "message": map[string]any{
		"role": "user", "content": []any{map[string]any{"type": "text", "text": text}}}}
}

func userToolResult() map[string]any {
	return map[string]any{"type": "user", "message": map[string]any{
		"role": "user", "content": []any{map[string]any{
			"type": "tool_result", "content": "ok"}}}}
}

func writeTranscript(t *testing.T, dir string, entries []map[string]any) string {
	t.Helper()
	path := filepath.Join(dir, "transcript.jsonl")
	var b strings.Builder
	for _, e := range entries {
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

var sessionCounter int

func freshSession(t *testing.T) string {
	t.Helper()
	sessionCounter++
	return fmt.Sprintf("sess-%s-%d", t.Name(), sessionCounter)
}

func payload(t *testing.T, transcriptPath, session, promptID string, stopHookActive any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"session_id":       session,
		"prompt_id":        promptID,
		"transcript_path":  transcriptPath,
		"stop_hook_active": stopHookActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func decision(t *testing.T, out string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	return m
}

func assertBlocks(t *testing.T, code int, out string) {
	t.Helper()
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	d := decision(t, out)
	if d["decision"] != "block" {
		t.Errorf("decision = %v, want block", d["decision"])
	}
}

// silence returns an asserter for a (code, out) pair. Go only allows a
// multi-value call to be spread into a function whose parameters it exactly
// matches, so the *testing.T is bound first and the results applied second.
func silence(t *testing.T) func(int, string) {
	t.Helper()
	return func(code int, out string) {
		t.Helper()
		if code != 0 || out != "" {
			t.Errorf("got (%d, %q), want (0, \"\")", code, out)
		}
	}
}

// --- core stop behavior ------------------------------------------------------

func TestBlocksOnStuckTurn(t *testing.T) { // test_blocks_on_stuck_turn
	dir := isolate(t)
	stubSleep(t, nil)
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("fix it"), assistantText("I'm stuck. The mock never fires.")})
	code, out := RunStop(payload(t, path, freshSession(t), "p-1", false))
	assertBlocks(t, code, out)
	if !strings.Contains(strings.ToLower(decision(t, out)["reason"].(string)), "oracle") {
		t.Error("reason should mention the oracle")
	}
	_ = dir
}

func TestSilentWhenNoMarker(t *testing.T) { // test_silent_when_no_marker
	isolate(t)
	stubSleep(t, nil)
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("fix it"), assistantText("Done. Tests pass.")})
	silence(t)(RunStop(payload(t, path, freshSession(t), "p-1", false)))
}

func TestSilentWhenStopHookActive(t *testing.T) { // test_silent_when_stop_hook_active
	isolate(t)
	stubSleep(t, nil)
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("x"), assistantText("I'm stuck.")})
	silence(t)(RunStop(payload(t, path, freshSession(t), "p-1", true)))
}

func TestStringFalseStopHookActiveDoesNotSuppress(t *testing.T) {
	// test_string_false_stop_hook_active_does_not_suppress. Python tests
	// `is True`, so any non-boolean value must fall through.
	isolate(t)
	stubSleep(t, nil)
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("x"), assistantText("I'm stuck. No idea.")})
	for _, v := range []any{"false", "true", 0, 1, nil} {
		t.Run(fmt.Sprintf("%v", v), func(t *testing.T) {
			code, out := RunStop(payload(t, path, freshSession(t), "p-1", v))
			assertBlocks(t, code, out)
		})
	}
}

func TestSilentWhenOracleAlreadyConsulted(t *testing.T) { // test_silent_when_oracle_already_consulted
	isolate(t)
	stubSleep(t, nil)
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("fix it"),
		assistantTool("Task", map[string]any{"subagent_type": "oracle"}),
		assistantText("I'm stuck even after the consult."),
	})
	silence(t)(RunStop(payload(t, path, freshSession(t), "p-1", false)))
}

func TestSilentOnMalformedStdin(t *testing.T) { // test_silent_on_malformed_stdin
	isolate(t)
	for _, in := range []string{"this is not json", "", "[1,2,3]", "null", `"str"`, "42"} {
		t.Run(in, func(t *testing.T) { silence(t)(RunStop(in)) })
	}
}

func TestSilentOnMissingTranscript(t *testing.T) { // test_silent_on_missing_transcript
	isolate(t)
	stubSleep(t, nil)
	silence(t)(RunStop(payload(t, "Z:/nope.jsonl", freshSession(t), "p-1", false)))
}

func TestSilentOnCorruptedTranscript(t *testing.T) { // test_silent_on_corrupted_transcript
	isolate(t)
	stubSleep(t, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.jsonl")
	if err := os.WriteFile(path, []byte("garbage\n{{{not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	silence(t)(RunStop(payload(t, path, freshSession(t), "p-1", false)))
}

func TestStdinBOMTolerated(t *testing.T) { // test_stdin_bom_tolerated
	isolate(t)
	stubSleep(t, nil)
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("fix"), assistantText("I'm stuck. No idea.")})
	code, out := RunStop("\uFEFF" + payload(t, path, freshSession(t), "p-1", false))
	assertBlocks(t, code, out)
}

func TestTurnGuardBlocksOncePerPrompt(t *testing.T) { // test_turn_guard_blocks_once_per_prompt
	isolate(t)
	stubSleep(t, nil)
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("fix"), assistantText("I'm stuck. No idea.")})
	session := freshSession(t)

	code, out := RunStop(payload(t, path, session, "p-1", false))
	assertBlocks(t, code, out)
	// same turn (same prompt_id): waved through
	silence(t)(RunStop(payload(t, path, session, "p-1", false)))
	// NEW turn (new prompt_id), same session: eligible again, no wall-clock window
	code, out = RunStop(payload(t, path, session, "p-2", false))
	assertBlocks(t, code, out)
}

func TestStaleMarkerFromPreviousTurnDoesNotBlock(t *testing.T) {
	// test_stale_marker_from_previous_turn_does_not_block
	isolate(t)
	stubSleep(t, nil)
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("first ask"),
		assistantText("I'm stuck. No idea."),
		userPrompt("second ask"),
		assistantTool("Bash", map[string]any{"command": "ls"}),
	})
	silence(t)(RunStop(payload(t, path, freshSession(t), "p-1", false)))
}

func TestToolResultDoesNotResetTurnBoundary(t *testing.T) {
	isolate(t)
	stubSleep(t, nil)
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("fix"),
		assistantTool("Task", map[string]any{"subagent_type": "oracle"}),
		userToolResult(),
		assistantText("I'm stuck even so."),
	})
	silence(t)(RunStop(payload(t, path, freshSession(t), "p-1", false)))
}

// --- flush race --------------------------------------------------------------

func TestFlushRaceRereadsLateAssistantText(t *testing.T) {
	// test_flush_race_rereads_late_assistant_text
	isolate(t)
	dir := t.TempDir()
	early := []map[string]any{userPrompt("trigger it"), assistantThinking()}
	late := append(append([]map[string]any{}, early...),
		assistantText("I'm stuck. The final text arrived late."))
	path := writeTranscript(t, dir, early)
	stubSleep(t, func(time.Duration) { writeTranscript(t, dir, late) })

	code, out := RunStop(payload(t, path, freshSession(t), "p-1", false))
	assertBlocks(t, code, out)
}

func TestFlushRaceFiresWhenTurnOpenedWithPreamble(t *testing.T) {
	// test_flush_race_fires_when_the_turn_opened_with_a_preamble.
	// A probe asking "does this turn have ANY assistant text" is satisfied by
	// the preamble, breaks out with zero re-reads, and judges the turn on stale
	// text — sailing through on the COMMON turn shape rather than the rare one.
	isolate(t)
	dir := t.TempDir()
	early := []map[string]any{
		userPrompt("fix the mock"),
		assistantText("Let me look at this."),
		assistantTool("Bash", map[string]any{"command": "pytest"}),
		userToolResult(),
		assistantThinking(),
	}
	late := append(append([]map[string]any{}, early...),
		assistantText("I'm stuck. The mock never fires."))
	path := writeTranscript(t, dir, early)
	stubSleep(t, func(time.Duration) { writeTranscript(t, dir, late) })

	code, out := RunStop(payload(t, path, freshSession(t), "p-1", false))
	assertBlocks(t, code, out)
}

func TestFlushRetrySurvivesTornReRead(t *testing.T) {
	// test_flush_retry_survives_a_torn_re_read. An empty re-read is the
	// condition the retry exists to ride out, so it must cost one delay rather
	// than the whole budget.
	isolate(t)
	dir := t.TempDir()
	early := []map[string]any{userPrompt("trigger it"), assistantThinking()}
	late := append(append([]map[string]any{}, early...),
		assistantText("I'm stuck. That read was torn."))
	path := writeTranscript(t, dir, early)

	n := 0
	seen := stubSleep(t, func(time.Duration) {
		n++
		if n == 1 {
			if err := os.WriteFile(path, nil, 0o644); err != nil { // torn / mid-write
				t.Fatal(err)
			}
		} else {
			writeTranscript(t, dir, late)
		}
	})

	code, out := RunStop(payload(t, path, freshSession(t), "p-1", false))
	assertBlocks(t, code, out)
	if len(*seen) != 2 {
		t.Errorf("torn read must cost one delay, not the whole budget; got %d delays", len(*seen))
	}
}

func TestFlushRaceCatchesTextLandingOnFinalRetry(t *testing.T) {
	// test_flush_race_catches_text_landing_on_the_final_retry. Pins the budget
	// as SUFFICIENT, not merely bounded: a future trim of FlushDelays would
	// silently reopen the race while the other tests kept passing.
	isolate(t)
	dir := t.TempDir()
	early := []map[string]any{userPrompt("trigger it"), assistantThinking()}
	late := append(append([]map[string]any{}, early...),
		assistantText("I'm stuck. This one crawled onto disk."))
	path := writeTranscript(t, dir, early)

	n := 0
	stubSleep(t, func(time.Duration) {
		n++
		if n == len(FlushDelays) {
			writeTranscript(t, dir, late)
		}
	})

	code, out := RunStop(payload(t, path, freshSession(t), "p-1", false))
	assertBlocks(t, code, out)
}

func TestFlushRetryIsBoundedAndFailsOpen(t *testing.T) {
	// test_flush_retry_is_bounded_and_fails_open
	isolate(t)
	seen := stubSleep(t, nil)
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("x"), assistantTool("Bash", map[string]any{"command": "ls"})})
	silence(t)(RunStop(payload(t, path, freshSession(t), "p-1", false)))
	if !reflect.DeepEqual(*seen, FlushDelays) {
		t.Errorf("delays = %v, want %v", *seen, FlushDelays)
	}
}

func TestNoRetryWhenTurnAlreadyHasText(t *testing.T) {
	// test_no_retry_when_turn_already_has_text. The common path must pay no
	// latency at all.
	isolate(t)
	seen := stubSleep(t, nil)
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("fix it"), assistantText("Done. Tests pass.")})
	silence(t)(RunStop(payload(t, path, freshSession(t), "p-1", false)))
	if len(*seen) != 0 {
		t.Errorf("expected no sleeps on the common path, got %v", *seen)
	}
}

func TestFlushDelaysBackOffAndStayWithinBudget(t *testing.T) {
	// test_flush_delays_back_off_and_stay_within_budget. The schedule itself is
	// the fix, so assert on it directly.
	var total time.Duration
	for i, d := range FlushDelays {
		total += d
		if i > 0 && d <= FlushDelays[i-1] {
			t.Errorf("waits must strictly increase: %v", FlushDelays)
		}
	}
	if total < 300*time.Millisecond || total > time.Second {
		t.Errorf("total wait %v outside the 300ms-1s budget", total)
	}
}

// --- session start -----------------------------------------------------------

func TestSessionStartEmitsAdditionalContextEnvelope(t *testing.T) {
	// test_session_start_emits_additional_context_envelope
	isolate(t)
	code, out := RunSessionStart("")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	var envelope struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if envelope.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("hookEventName = %q", envelope.HookSpecificOutput.HookEventName)
	}
	if envelope.HookSpecificOutput.AdditionalContext != Doctrine {
		t.Error("additionalContext is not the doctrine")
	}
	if !strings.Contains(strings.ToLower(Doctrine), "oracle") {
		t.Error("doctrine must mention the oracle")
	}
	if n := len(strings.Split(Doctrine, "\n")); n > 8 {
		t.Errorf("doctrine must stay tiny: %d lines, want <= 8", n)
	}
}

func TestKillSwitchSilencesBoth(t *testing.T) {
	// test_env_kill_switch_silences_stop / _session_start
	isolate(t)
	stubSleep(t, nil)
	t.Setenv(config.KillSwitchEnv, "1")
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("x"), assistantText("I'm stuck. No idea.")})
	silence(t)(RunStop(payload(t, path, freshSession(t), "p-1", false)))
	silence(t)(RunSessionStart(""))
}

// --- byte-level parity with the Python implementation ------------------------

func TestOutputBytesMatchPythonGoldens(t *testing.T) {
	// The goldens were produced by the Python hook. Matching them byte for byte
	// is what lets the differential suite treat any difference as behavioral.
	isolate(t)
	stubSleep(t, nil)

	stopGolden, err := os.ReadFile(filepath.Join("testdata", "python_stop_block.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("x"), assistantText("I'm stuck. No idea.")})
	_, out := RunStop(payload(t, path, freshSession(t), "p-1", false))
	if out != string(stopGolden) {
		t.Errorf("stop output differs from Python\n got: %s\nwant: %s", out, stopGolden)
	}

	ssGolden, err := os.ReadFile(filepath.Join("testdata", "python_session_start.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	_, out = RunSessionStart("")
	if out != string(ssGolden) {
		t.Errorf("session-start output differs from Python\n got: %s\nwant: %s", out, ssGolden)
	}
}

func TestOutputIsASCIISafe(t *testing.T) {
	// test_stop_output_is_ascii_safe: emitted JSON must survive any codepage.
	isolate(t)
	stubSleep(t, nil)
	path := writeTranscript(t, t.TempDir(), []map[string]any{
		userPrompt("x"), assistantText("I'm stuck. No idea.")})
	_, out := RunStop(payload(t, path, freshSession(t), "p-1", false))
	if out == "" {
		t.Fatal("expected a block")
	}
	for i := 0; i < len(out); i++ {
		if out[i] >= 0x80 {
			t.Fatalf("non-ASCII byte 0x%02x at %d", out[i], i)
		}
	}
}

// --- cross-package consistency ----------------------------------------------

func TestConfigAndDetectAgreeOnBaselineMarkers(t *testing.T) {
	// The two packages hold independent copies of the marker list so they could
	// be built in parallel. This is the tripwire that keeps them identical.
	isolate(t)
	fromConfig := config.EffectiveMarkers(config.Defaults())
	fromDetect := detect.DefaultMarkerSet()
	if !reflect.DeepEqual(sortedKeys(fromConfig), sortedKeys(fromDetect)) {
		t.Errorf("marker lists have drifted apart\nconfig: %v\ndetect: %v",
			sortedKeys(fromConfig), sortedKeys(fromDetect))
	}
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- payload helper semantics ------------------------------------------------

func TestTruthyPythonStr(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{``, ""}, {`null`, ""}, {`false`, ""}, {`0`, ""}, {`""`, ""},
		{`[]`, ""}, {`{}`, ""}, {`0.0`, ""},
		{`"p-1"`, "p-1"}, {`true`, "True"}, {`5`, "5"}, {`1.5`, "1.5"},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			if got := truthyPythonStr(json.RawMessage(c.raw)); got != c.want {
				t.Errorf("truthyPythonStr(%s) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func TestPythonStrMatchesPythonRendering(t *testing.T) {
	// Expected values were read off CPython's str() for the decoded value.
	// The rendering only picks a state file, so what matters is that two ids
	// Python keeps apart do not collide here. Collisions Python itself has
	// (str(0) and str("0") are both "0") are shared, not divergences.
	cases := []struct{ raw, want string }{
		{``, "unknown"},
		{`null`, "None"},
		{`""`, ""},
		{`"None"`, "None"},
		{`true`, "True"},
		{`false`, "False"},
		{`0`, "0"},
		{`"0"`, "0"},
		{`123`, "123"},
		{`1.5`, "1.5"},
		{`1e3`, "1000.0"},
		{`"abc"`, "abc"},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			if got := pythonStr(json.RawMessage(c.raw), "unknown"); got != c.want {
				t.Errorf("pythonStr(%s) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func TestIntegerAndFloatSessionIDsStayDistinct(t *testing.T) {
	// 1e3 and 1000 are different Python values and must not share a state file.
	if a, b := pythonStr(json.RawMessage(`1e3`), ""), pythonStr(json.RawMessage(`1000`), ""); a == b {
		t.Errorf("1e3 and 1000 both render %q", a)
	}
}
