package transcript

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// --- fixture builders -------------------------------------------------------
//
// Every fixture is written as a real JSONL file and read back through
// LoadEntries, so each test exercises the decoder as well as the function
// under test.

func jstr(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// assistant mirrors the Python _assistant helper: an assistant entry with an
// optional text block and an optional tool_use block, in that order. Pass ""
// for text or tool to omit that block; toolInput is raw JSON.
func assistant(text, tool, toolInput string) string {
	var blocks []string
	if text != "" {
		blocks = append(blocks, `{"type":"text","text":`+jstr(text)+`}`)
	}
	if tool != "" {
		blocks = append(blocks, `{"type":"tool_use","name":`+jstr(tool)+`,"input":`+toolInput+`}`)
	}
	return `{"type":"assistant","message":{"role":"assistant","content":[` +
		strings.Join(blocks, ",") + `]}}`
}

func assistantText(text string) string { return assistant(text, "", "") }

func assistantTool(tool, toolInput string) string { return assistant("", tool, toolInput) }

// userPrompt mirrors the Python _user_prompt helper.
func userPrompt(text string) string {
	return `{"type":"user","message":{"role":"user","content":[{"type":"text","text":` +
		jstr(text) + `}]}}`
}

// userToolResult mirrors the Python _user_tool_result helper.
func userToolResult() string {
	return `{"type":"user","message":{"role":"user","content":` +
		`[{"type":"tool_result","tool_use_id":"x","content":"ok"}]}}`
}

// sidechain marks a fixture line as a subagent entry.
func sidechain(line string) string {
	return strings.TrimSuffix(line, "}") + `,"isSidechain":true}`
}

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.jsonl")
	body := ""
	if len(lines) > 0 {
		body = strings.Join(lines, "\n") + "\n"
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

func load(t *testing.T, lines ...string) []Entry {
	t.Helper()
	return LoadEntries(writeTranscript(t, lines...))
}

func types(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Type)
	}
	return out
}

// --- LoadEntries ------------------------------------------------------------

// Python: test_load_entries_skips_malformed_lines
func TestLoadEntriesSkipsMalformedLines(t *testing.T) {
	entries := load(t, `{"type": "user"}`, `not json at all`, `{"type": "assistant"}`)
	got := types(entries)
	if len(got) != 2 || got[0] != "user" || got[1] != "assistant" {
		t.Fatalf("types = %v, want [user assistant]", got)
	}
}

// Python: test_load_entries_missing_file_returns_empty
func TestLoadEntriesMissingFileReturnsEmpty(t *testing.T) {
	if got := LoadEntries(filepath.Join(t.TempDir(), "definitely", "not", "here.jsonl")); len(got) != 0 {
		t.Fatalf("LoadEntries(missing) = %v, want empty", got)
	}
}

func TestLoadEntriesUnreadablePathReturnsEmpty(t *testing.T) {
	// A directory stands in for any path that cannot be read as a file.
	if got := LoadEntries(t.TempDir()); len(got) != 0 {
		t.Fatalf("LoadEntries(dir) = %v, want empty", got)
	}
}

func TestLoadEntriesEmptyFileReturnsEmpty(t *testing.T) {
	if got := load(t); len(got) != 0 {
		t.Fatalf("LoadEntries(empty) = %v, want empty", got)
	}
}

func TestLoadEntriesSkipsBlankAndNonObjectLines(t *testing.T) {
	entries := load(t,
		`{"type":"user"}`,
		``,
		`   `,
		`[1, 2, 3]`,
		`null`,
		`42`,
		`"a bare string"`,
		`{"type":"assistant"}`,
	)
	got := types(entries)
	if len(got) != 2 || got[0] != "user" || got[1] != "assistant" {
		t.Fatalf("types = %v, want [user assistant]", got)
	}
}

func TestLoadEntriesHandlesCRLFAndBareCR(t *testing.T) {
	// Python reads in text mode with universal newlines, so \r\n and a lone \r
	// both terminate a record.
	p := filepath.Join(t.TempDir(), "t.jsonl")
	body := `{"type":"user"}` + "\r\n" + `{"type":"assistant"}` + "\r" + `{"type":"user"}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := types(LoadEntries(p))
	if len(got) != 3 {
		t.Fatalf("types = %v, want 3 entries", got)
	}
}

// Python: test_load_entries_skips_sidechain_entries
func TestLoadEntriesSkipsSidechainEntries(t *testing.T) {
	entries := load(t,
		userPrompt("fix"),
		sidechain(assistantText("I'm stuck.")),
		assistantText("done."),
	)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 (sidechain dropped)", len(entries))
	}
	if got := LastAssistantText(entries); got != "done." {
		t.Fatalf("LastAssistantText = %q, want %q", got, "done.")
	}
}

func TestLoadEntriesSidechainTruthiness(t *testing.T) {
	// isSidechain is a Python truthiness test, not a bool decode.
	cases := []struct {
		name    string
		value   string
		skipped bool
	}{
		{"true", `true`, true},
		{"false", `false`, false},
		{"null", `null`, false},
		{"zero", `0`, false},
		{"one", `1`, true},
		{"empty string", `""`, false},
		{"string false", `"false"`, true}, // truthy non-empty string
		{"string 0", `"0"`, true},
		{"empty list", `[]`, false},
		{"nonempty list", `[0]`, true},
		{"empty object", `{}`, false},
		{"nonempty object", `{"a":0}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := `{"type":"assistant","isSidechain":` + tc.value + `}`
			entries := load(t, line)
			skipped := len(entries) == 0
			if skipped != tc.skipped {
				t.Fatalf("isSidechain=%s skipped=%v, want %v", tc.value, skipped, tc.skipped)
			}
		})
	}
}

func TestLoadEntriesSidechainAbsentIsKept(t *testing.T) {
	if entries := load(t, `{"type":"assistant"}`); len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
}

// Python: test_load_entries_tolerates_invalid_utf8
func TestLoadEntriesToleratesInvalidUTF8(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	body := []byte(userPrompt("fix") + "\n")
	body = append(body, []byte("\xff\xfe garbage line\n")...)
	body = append(body, []byte(assistantText("done.")+"\n")...)
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LastAssistantText(LoadEntries(p)); got != "done." {
		t.Fatalf("LastAssistantText = %q, want %q", got, "done.")
	}
}

func TestLoadEntriesInvalidByteCostsOneReplacementChar(t *testing.T) {
	// Python's errors="replace" turns a stray \xff into exactly one U+FFFD;
	// the surrounding record still parses and the text is still scannable.
	p := filepath.Join(t.TempDir(), "t.jsonl")
	body := []byte(`{"type":"assistant","message":{"role":"assistant",` +
		`"content":[{"type":"text","text":"a` + "\xff" + `b"}]}}` + "\n")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := LastAssistantText(LoadEntries(p)), "a�b"; got != want {
		t.Fatalf("LastAssistantText = %q, want %q", got, want)
	}
}

// --- LastAssistantText ------------------------------------------------------

// Python: test_last_assistant_text_takes_final_text_message
func TestLastAssistantTextTakesFinalTextMessage(t *testing.T) {
	entries := load(t,
		assistantText("first"),
		userToolResult(),
		assistantText("I'm stuck on the failing mock."),
	)
	if got, want := LastAssistantText(entries), "I'm stuck on the failing mock."; got != want {
		t.Fatalf("LastAssistantText = %q, want %q", got, want)
	}
}

// Python: test_last_assistant_text_skips_tool_use_only_entries
func TestLastAssistantTextSkipsToolUseOnlyEntries(t *testing.T) {
	entries := load(t,
		assistantText("real text"),
		assistantTool("Bash", `{"command":"ls"}`),
	)
	if got, want := LastAssistantText(entries), "real text"; got != want {
		t.Fatalf("LastAssistantText = %q, want %q", got, want)
	}
}

// Python: test_last_assistant_text_ignores_user_messages
func TestLastAssistantTextIgnoresUserMessages(t *testing.T) {
	// User saying "I'm not sure" must never be what gets scanned.
	entries := load(t,
		assistantText("done."),
		userPrompt("I'm not sure what I want here"),
	)
	if got, want := LastAssistantText(entries), "done."; got != want {
		t.Fatalf("LastAssistantText = %q, want %q", got, want)
	}
}

// Python: test_last_assistant_text_empty_transcript
func TestLastAssistantTextEmptyTranscript(t *testing.T) {
	if got := LastAssistantText(nil); got != "" {
		t.Fatalf("LastAssistantText(nil) = %q, want %q", got, "")
	}
	if got := LastAssistantText([]Entry{}); got != "" {
		t.Fatalf("LastAssistantText(empty) = %q, want %q", got, "")
	}
}

// Python: test_last_assistant_text_handles_string_content
func TestLastAssistantTextHandlesStringContent(t *testing.T) {
	entries := load(t,
		`{"type":"assistant","message":{"role":"assistant","content":"I'm stuck on the mock."}}`,
	)
	if got, want := LastAssistantText(entries), "I'm stuck on the mock."; got != want {
		t.Fatalf("LastAssistantText = %q, want %q", got, want)
	}
}

func TestLastAssistantTextJoinsBlocksAndTrims(t *testing.T) {
	entries := load(t,
		`{"type":"assistant","message":{"role":"assistant","content":[`+
			`{"type":"text","text":"  first line"},`+
			`{"type":"thinking","thinking":"ignored"},`+
			`{"type":"text","text":""},`+
			`{"type":"text","text":"second line  "}]}}`,
	)
	if got, want := LastAssistantText(entries), "first line\nsecond line"; got != want {
		t.Fatalf("LastAssistantText = %q, want %q", got, want)
	}
}

func TestLastAssistantTextSkipsWhitespaceOnlyEntries(t *testing.T) {
	entries := load(t,
		assistantText("real text"),
		assistantText("   \n  "),
	)
	if got, want := LastAssistantText(entries), "real text"; got != want {
		t.Fatalf("LastAssistantText = %q, want %q", got, want)
	}
}

func TestLastAssistantTextIgnoresHookInjectedContext(t *testing.T) {
	// Hook-injected context arrives as a non-assistant entry and must never
	// be what the nudge decision reads.
	entries := load(t,
		assistantText("done."),
		`{"type":"system","message":{"role":"system","content":"I'm stuck."}}`,
	)
	if got, want := LastAssistantText(entries), "done."; got != want {
		t.Fatalf("LastAssistantText = %q, want %q", got, want)
	}
}

func TestLastAssistantTextEmptyStringContentFallsThrough(t *testing.T) {
	entries := load(t,
		assistantText("earlier text"),
		`{"type":"assistant","message":{"role":"assistant","content":"   "}}`,
	)
	if got, want := LastAssistantText(entries), "earlier text"; got != want {
		t.Fatalf("LastAssistantText = %q, want %q", got, want)
	}
}

// --- TurnStart --------------------------------------------------------------

func TestTurnStart(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  int
	}{
		{"empty", nil, 0},
		{"no user prompt at all", []string{assistantText("hi"), assistantText("there")}, 0},
		{"single prompt at head", []string{userPrompt("go"), assistantText("ok")}, 0},
		{
			"last of several prompts",
			[]string{userPrompt("first"), assistantText("ok"), userPrompt("second"), assistantText("ok")},
			2,
		},
		{
			"tool results do not move the boundary",
			[]string{userPrompt("first"), assistantText("ok"), userToolResult(), assistantText("ok")},
			0,
		},
		{
			"string-content user prompt counts",
			[]string{
				assistantText("ok"),
				`{"type":"user","message":{"role":"user","content":"do the thing"}}`,
			},
			1,
		},
		{
			"whitespace-only string prompt does not count",
			[]string{
				userPrompt("real"),
				assistantText("ok"),
				`{"type":"user","message":{"role":"user","content":"   "}}`,
			},
			0,
		},
		{
			"mixed text and tool_result blocks do not count",
			[]string{
				userPrompt("real"),
				`{"type":"user","message":{"role":"user","content":[` +
					`{"type":"text","text":"here"},` +
					`{"type":"tool_result","tool_use_id":"x","content":"ok"}]}}`,
			},
			0,
		},
		{
			"user entry with no content does not count",
			[]string{userPrompt("real"), `{"type":"user"}`},
			0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TurnStart(load(t, tc.lines...)); got != tc.want {
				t.Fatalf("TurnStart = %d, want %d", got, tc.want)
			}
		})
	}
}

// --- TurnTailFlushed --------------------------------------------------------

func TestTurnTailFlushed(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  bool
	}{
		{"empty", nil, false},
		{"no assistant entries", []string{userPrompt("go"), userToolResult()}, false},
		{"tail carries text", []string{userPrompt("go"), assistantText("I'm stuck.")}, true},
		{
			// The incident this probe exists for: a preamble satisfies an
			// any-text probe while the turn's real final message is still in
			// flight. The tail is tool_use only, so the tail is NOT flushed.
			"preamble then tool-only tail",
			[]string{
				userPrompt("go"),
				assistantText("Let me look at this."),
				assistantTool("Bash", `{"command":"ls"}`),
			},
			false,
		},
		{
			"tail holds thinking only",
			[]string{
				userPrompt("go"),
				assistantText("Let me look at this."),
				`{"type":"assistant","message":{"role":"assistant","content":` +
					`[{"type":"thinking","thinking":"hmm"}]}}`,
			},
			false,
		},
		{
			"tail holds text plus tool_use",
			[]string{userPrompt("go"), assistant("Here is the answer.", "Bash", `{"command":"ls"}`)},
			true,
		},
		{
			"tool results after the tail do not hide it",
			[]string{userPrompt("go"), assistantText("done."), userToolResult()},
			true,
		},
		{
			"tail text is whitespace only",
			[]string{userPrompt("go"), assistantText("   ")},
			false,
		},
		{
			"tail string content",
			[]string{userPrompt("go"), `{"type":"assistant","message":{"role":"assistant","content":"done."}}`},
			true,
		},
		{
			"tail empty string content",
			[]string{
				assistantText("earlier"),
				`{"type":"assistant","message":{"role":"assistant","content":""}}`,
			},
			false,
		},
		{
			"tail with empty block list",
			[]string{assistantText("earlier"), `{"type":"assistant","message":{"role":"assistant","content":[]}}`},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TurnTailFlushed(load(t, tc.lines...)); got != tc.want {
				t.Fatalf("TurnTailFlushed = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- OracleConsultedThisTurn ------------------------------------------------

// Python: test_consulted_true_when_oracle_task_after_last_user_prompt
func TestConsultedTrueWhenOracleTaskAfterLastUserPrompt(t *testing.T) {
	entries := load(t,
		userPrompt("fix the bug"),
		assistant("consulting", "Task", `{"subagent_type":"oracle","prompt":"brief"}`),
		userToolResult(),
		assistantText("implementing the plan"),
	)
	if !OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = false, want true")
	}
}

// Python: test_consulted_matches_plugin_scoped_name
func TestConsultedMatchesPluginScopedName(t *testing.T) {
	entries := load(t,
		userPrompt("fix the bug"),
		assistantTool("Task", `{"subagent_type":"oracle:oracle"}`),
	)
	if !OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = false, want true")
	}
}

// Python: test_consulted_rejects_substring_lookalikes
func TestConsultedRejectsSubstringLookalikes(t *testing.T) {
	// exact-name rule: an unrelated agent containing "oracle" must NOT count
	entries := load(t,
		userPrompt("fix the bug"),
		assistantTool("Task", `{"subagent_type":"my-oracledb-helper"}`),
	)
	if OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = true, want false")
	}
}

// Python: test_consulted_false_when_consult_was_previous_turn
func TestConsultedFalseWhenConsultWasPreviousTurn(t *testing.T) {
	entries := load(t,
		userPrompt("first ask"),
		assistantTool("Task", `{"subagent_type":"oracle"}`),
		userPrompt("second ask"),
		assistantText("I'm stuck."),
	)
	if OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = true, want false")
	}
}

// Python: test_consulted_false_for_other_agents
func TestConsultedFalseForOtherAgents(t *testing.T) {
	entries := load(t,
		userPrompt("fix"),
		assistantTool("Task", `{"subagent_type":"general-purpose"}`),
	)
	if OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = true, want false")
	}
}

// Python: test_consulted_via_agent_tool_name
func TestConsultedViaAgentToolName(t *testing.T) {
	// Newer harnesses dispatch subagents through a tool named "Agent", not "Task".
	entries := load(t,
		userPrompt("fix the bug"),
		assistantTool("Agent", `{"subagent_type":"oracle","prompt":"brief"}`),
	)
	if !OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = false, want true")
	}
}

// Python: test_tool_results_do_not_count_as_user_prompts
func TestToolResultsDoNotCountAsUserPrompts(t *testing.T) {
	// tool_result user entries must not reset the turn boundary
	entries := load(t,
		userPrompt("fix"),
		assistantTool("Task", `{"subagent_type":"oracle"}`),
		userToolResult(),
		assistantText("still this turn"),
	)
	if !OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = false, want true")
	}
}

func TestConsultedSubagentNameRules(t *testing.T) {
	cases := []struct {
		name         string
		subagentType string // raw JSON
		want         bool
	}{
		{"exact oracle", `"oracle"`, true},
		{"uppercase", `"ORACLE"`, true},
		{"mixed case scoped", `"cc-Oracle:Oracle"`, true},
		{"plugin scoped", `"oracle:oracle"`, true},
		{"substring lookalike", `"my-oracledb-helper"`, false},
		{"suffix without colon", `"myoracle"`, false},
		{"prefix", `"oracle-helper"`, false},
		{"trailing space", `"oracle "`, false},
		{"colon oracle prefix", `"oracle:general"`, false},
		{"empty", `""`, false},
		{"number", `5`, false},
		{"bool", `true`, false},
		{"null", `null`, false},
		{"list", `["oracle"]`, false},
		{"object", `{"a":"oracle"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := load(t,
				userPrompt("fix"),
				assistantTool("Task", `{"subagent_type":`+tc.subagentType+`}`),
			)
			if got := OracleConsultedThisTurn(entries); got != tc.want {
				t.Fatalf("OracleConsultedThisTurn = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConsultedIgnoresNonDispatchTools(t *testing.T) {
	entries := load(t,
		userPrompt("fix"),
		assistantTool("Bash", `{"subagent_type":"oracle"}`),
	)
	if OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = true, want false")
	}
}

func TestConsultedIgnoresUserEntries(t *testing.T) {
	// A tool_use block on a non-assistant entry is not the main thread
	// dispatching anything.
	entries := load(t,
		userPrompt("fix"),
		`{"type":"user","message":{"role":"user","content":[`+
			`{"type":"tool_use","name":"Task","input":{"subagent_type":"oracle"}}]}}`,
	)
	if OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = true, want false")
	}
}

func TestConsultedIgnoresSidechainDispatch(t *testing.T) {
	// A subagent's own Task call lives in the same file and is not ours.
	entries := load(t,
		userPrompt("fix"),
		sidechain(assistantTool("Task", `{"subagent_type":"oracle"}`)),
	)
	if OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = true, want false")
	}
}

func TestConsultedEmptyEntries(t *testing.T) {
	if OracleConsultedThisTurn(nil) {
		t.Fatal("OracleConsultedThisTurn(nil) = true, want false")
	}
}

// --- decode tolerance -------------------------------------------------------
//
// New code relative to Python: these shapes must degrade the way a .get()
// chain would rather than dropping the record or panicking.

func TestDecodeToleranceDoesNotDropEntries(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"message missing entirely", `{"type":"assistant"}`},
		{"message is null", `{"type":"assistant","message":null}`},
		{"message is a string", `{"type":"assistant","message":"oops"}`},
		{"message is a number", `{"type":"assistant","message":7}`},
		{"content missing", `{"type":"assistant","message":{"role":"assistant"}}`},
		{"content is null", `{"type":"assistant","message":{"role":"assistant","content":null}}`},
		{"content is a number", `{"type":"assistant","message":{"role":"assistant","content":7}}`},
		{"content is an object", `{"type":"assistant","message":{"role":"assistant","content":{"a":1}}}`},
		{"content array holds scalars", `{"type":"assistant","message":{"role":"assistant","content":["x",1,null]}}`},
		{"block type is a number", `{"type":"assistant","message":{"role":"assistant","content":[{"type":5,"text":"hi"}]}}`},
		{"block name is a list", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":["Task"]}]}}`},
		{"tool input is a string", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Task","input":"oracle"}]}}`},
		{"tool input is a list", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Task","input":["oracle"]}]}}`},
		{"tool input missing", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Task"}]}}`},
		{"tool input is null", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Task","input":null}]}}`},
		{"type is a number", `{"type":9,"message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`},
		{"type missing", `{"message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The record survives the decode...
			entries := load(t, tc.line)
			if len(entries) != 1 {
				t.Fatalf("len(entries) = %d, want 1 (record must not be dropped)", len(entries))
			}
			// ...and every consumer tolerates it.
			LastAssistantText(entries)
			TurnStart(entries)
			TurnTailFlushed(entries)
			OracleConsultedThisTurn(entries)
		})
	}
}

func TestDecodeToleranceDegradesLikePythonGet(t *testing.T) {
	cases := []struct {
		name         string
		line         string
		wantText     string
		wantFlushed  bool
		wantConsult  bool
		wantTurnZero bool
	}{
		{
			name:     "message missing yields no text",
			line:     `{"type":"assistant"}`,
			wantText: "",
		},
		{
			name:     "content null yields no text",
			line:     `{"type":"assistant","message":{"role":"assistant","content":null}}`,
			wantText: "",
		},
		{
			name:     "content array of scalars yields no blocks",
			line:     `{"type":"assistant","message":{"role":"assistant","content":["I'm stuck",null,3]}}`,
			wantText: "",
		},
		{
			name:     "block with non-string type is not a text block",
			line:     `{"type":"assistant","message":{"role":"assistant","content":[{"type":5,"text":"I'm stuck"}]}}`,
			wantText: "",
		},
		{
			name:     "block with non-string text yields empty text",
			line:     `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":5}]}}`,
			wantText: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := load(t, tc.line)
			if got := LastAssistantText(entries); got != tc.wantText {
				t.Fatalf("LastAssistantText = %q, want %q", got, tc.wantText)
			}
			if got := TurnTailFlushed(entries); got != tc.wantFlushed {
				t.Fatalf("TurnTailFlushed = %v, want %v", got, tc.wantFlushed)
			}
			if got := OracleConsultedThisTurn(entries); got != tc.wantConsult {
				t.Fatalf("OracleConsultedThisTurn = %v, want %v", got, tc.wantConsult)
			}
		})
	}
}

func TestDecodeToleranceGoodBlocksSurviveBadNeighbours(t *testing.T) {
	// A malformed sibling block must not cost the record its usable blocks.
	entries := load(t,
		userPrompt("fix"),
		`{"type":"assistant","message":{"role":"assistant","content":[`+
			`"a bare string",`+
			`null,`+
			`{"type":5},`+
			`{"type":"tool_use","name":"Task","input":{"subagent_type":"oracle"}},`+
			`{"type":"text","text":"consulting"}]}}`,
	)
	if !OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = false, want true")
	}
	if got, want := LastAssistantText(entries), "consulting"; got != want {
		t.Fatalf("LastAssistantText = %q, want %q", got, want)
	}
}

func TestEntryStructIsUsableDirectly(t *testing.T) {
	// The package's consumers build fixtures as struct literals; keep the
	// exported shape sufficient for that.
	entries := []Entry{
		{Type: "user", Content: Content{Blocks: []Block{{Type: "text", Text: "fix"}}}},
		{Type: "assistant", Content: Content{Blocks: []Block{
			{Type: "tool_use", Name: "Agent", Input: json.RawMessage(`{"subagent_type":"cc-oracle:oracle"}`)},
			{Type: "text", Text: "consulting"},
		}}},
		{Type: "assistant", Content: Content{IsString: true, Str: "done."}},
	}
	if got := TurnStart(entries); got != 0 {
		t.Fatalf("TurnStart = %d, want 0", got)
	}
	if !OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = false, want true")
	}
	if !TurnTailFlushed(entries) {
		t.Fatal("TurnTailFlushed = false, want true")
	}
	if got, want := LastAssistantText(entries), "done."; got != want {
		t.Fatalf("LastAssistantText = %q, want %q", got, want)
	}
}

// --- pythonTruthy -----------------------------------------------------------

func TestPythonTruthy(t *testing.T) {
	cases := []struct {
		raw  string // "" means the key was absent
		want bool
	}{
		{``, false}, // absent
		{`null`, false},
		{`false`, false},
		{`true`, true},
		{`0`, false},
		{`-0`, false},
		{`0.0`, false},
		{`0e5`, false},
		{`1`, true},
		{`-1`, true},
		{`0.5`, true},
		{`1e400`, true},   // overflows to +Inf, still truthy
		{`1e-400`, false}, // underflows to 0.0, as Python's float does
		{`""`, false},
		{`"false"`, true}, // the string "false" is a non-empty string
		{`"0"`, true},
		{`" "`, true},
		{`"x"`, true},
		{`[]`, false},
		{`[0]`, true},
		{`[[]]`, true},
		{`{}`, false},
		{`{"a":0}`, true},
	}
	for _, tc := range cases {
		name := tc.raw
		if name == "" {
			name = "absent"
		}
		t.Run(name, func(t *testing.T) {
			var raw json.RawMessage
			if tc.raw != "" {
				raw = json.RawMessage(tc.raw)
			}
			if got := pythonTruthy(raw); got != tc.want {
				t.Fatalf("pythonTruthy(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// --- decodeUTF8Replace ------------------------------------------------------

// Expectations below were taken from CPython:
//
//	bytes.decode("utf-8", "replace")
//
// CPython substitutes one U+FFFD per *maximal subpart*, so a lone bad byte
// costs one replacement char and a truncated-but-valid multi-byte prefix also
// costs one. Neither string(b) nor strings.ToValidUTF8 reproduces this.
func TestDecodeUTF8Replace(t *testing.T) {
	const R = "�"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"ascii", "hello", "hello"},
		{"valid multibyte", "café 日本 \U0001F600", "café 日本 \U0001F600"},
		{"stray ff", "\xff", R},
		{"ff fe", "\xff\xfe", R + R},
		{"ascii around ff", "a\xffb", "a" + R + "b"},
		{"lone continuation bytes", "\x80\x80", R + R},
		{"truncated 2-byte at end", "\xc2", R},
		{"c2 then ascii", "\xc2A", R + "A"},
		{"truncated 3-byte at end", "\xe2\x82", R},
		{"truncated 3-byte then ascii", "\xe2\x82abc", R + "abc"},
		{"e2 82 then ascii", "\xe2\x82A", R + "A"},
		{"overlong 3-byte", "\xe0\x80\x80", R + R + R},
		{"surrogate", "\xed\xa0\x80", R + R + R},
		{"truncated 4-byte at end", "\xf0\x9f\x98", R},
		{"beyond U+10FFFF", "\xf4\x90\x80\x80", R + R + R + R},
		{"invalid start f5", "\xf5", R},
		{"garbage line", "\xff\xfe garbage line", R + R + " garbage line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(decodeUTF8Replace([]byte(tc.in))); got != tc.want {
				t.Fatalf("decodeUTF8Replace(% x) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDecodeUTF8ReplaceValidInputIsUntouched(t *testing.T) {
	// Round-trip guard: the fast path must not alter valid text, including
	// text that already contains a genuine U+FFFD.
	in := "valid � text \U0001F600"
	if got := string(decodeUTF8Replace([]byte(in))); got != in {
		t.Fatalf("decodeUTF8Replace = %q, want %q", got, in)
	}
}

// --- LoadTurn ---------------------------------------------------------------
//
// LoadTurn is an optimization of LoadEntries(p)[TurnStart(...):] and is tested
// only against it. LoadEntries stays the honest reference implementation: it
// parses every line, so it cannot drift in the same direction as the thing it
// is checking.

// assertLoadTurnEquivalent is the whole contract, asserted on a real file.
func assertLoadTurnEquivalent(t *testing.T, path string) {
	t.Helper()
	all := LoadEntries(path)
	want := all[TurnStart(all):]
	got := LoadTurn(path)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadTurn mismatch\n got (%d): %+v\nwant (%d): %+v",
			len(got), got, len(want), want)
	}
}

func TestLoadTurnEquivalentToSlicedLoadEntries(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
	}{
		{"empty file", nil},
		{
			"only entry is a user prompt",
			[]string{userPrompt("just this")},
		},
		{
			"single turn",
			[]string{
				userPrompt("fix the bug"),
				assistant("consulting", "Task", `{"subagent_type":"oracle","prompt":"brief"}`),
				userToolResult(),
				assistantText("implementing the plan"),
			},
		},
		{
			"several turns",
			[]string{
				userPrompt("first ask"),
				assistantTool("Task", `{"subagent_type":"oracle"}`),
				assistantText("done one"),
				userPrompt("second ask"),
				assistantText("working"),
				userToolResult(),
				assistantText("I'm stuck."),
			},
		},
		{
			// Degenerate: TurnStart is 0, so the turn is the whole file and
			// LoadTurn must parse all of it.
			"no user prompt anywhere",
			[]string{
				assistantText("hi"),
				userToolResult(),
				assistantTool("Bash", `{"command":"ls"}`),
				assistantText("bye"),
			},
		},
		{
			"all sidechain",
			[]string{
				sidechain(userPrompt("subagent ask")),
				sidechain(assistantText("subagent answer")),
			},
		},
		{
			// The boundary-defining prompt is a sidechain entry, so it is not
			// the boundary at all — both paths must agree on that.
			"sidechain user prompt is not a boundary",
			[]string{
				userPrompt("real ask"),
				assistantText("working"),
				sidechain(userPrompt("subagent ask")),
				assistantText("still the same turn"),
			},
		},
		{
			"sidechain interleaved inside the turn",
			[]string{
				userPrompt("real ask"),
				sidechain(assistantText("subagent noise")),
				assistantText("done."),
			},
		},
		{
			"blank and malformed lines interleaved",
			[]string{
				assistantText("before"),
				``,
				`not json at all`,
				userPrompt("the ask"),
				`   `,
				`[1,2,3]`,
				assistantText("after"),
			},
		},
		{
			"boundary is the last line",
			[]string{
				assistantText("previous turn tail"),
				userPrompt("brand new ask"),
			},
		},
		{
			"trailing tool results after the boundary",
			[]string{
				userPrompt("first"),
				assistantText("ok"),
				userPrompt("second"),
				userToolResult(),
				userToolResult(),
			},
		},
		{
			"string-content user prompt is a boundary",
			[]string{
				assistantText("earlier"),
				`{"type":"user","message":{"role":"user","content":"do the thing"}}`,
				assistantText("later"),
			},
		},
		{
			"whitespace-only user prompt is not a boundary",
			[]string{
				userPrompt("real ask"),
				assistantText("working"),
				`{"type":"user","message":{"role":"user","content":"   "}}`,
				assistantText("tail"),
			},
		},
		{
			"mixed text and tool_result user entry is not a boundary",
			[]string{
				userPrompt("real ask"),
				`{"type":"user","message":{"role":"user","content":[` +
					`{"type":"text","text":"here"},` +
					`{"type":"tool_result","tool_use_id":"x","content":"ok"}]}}`,
				assistantText("tail"),
			},
		},
		{
			"non-object and scalar lines only",
			[]string{`null`, `42`, `[1,2,3]`, `"bare"`},
		},
		{
			"odd shapes around the boundary",
			[]string{
				`{"type":"assistant","message":"oops"}`,
				userPrompt("the ask"),
				`{"type":"assistant","message":null}`,
				`{"type":"assistant","message":{"role":"assistant","content":7}}`,
				assistantText("tail"),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertLoadTurnEquivalent(t, writeTranscript(t, tc.lines...))
		})
	}
}

func TestLoadTurnMissingFileReturnsEmpty(t *testing.T) {
	if got := LoadTurn(filepath.Join(t.TempDir(), "nope.jsonl")); len(got) != 0 {
		t.Fatalf("LoadTurn(missing) = %v, want empty", got)
	}
	if got := LoadTurn(t.TempDir()); len(got) != 0 {
		t.Fatalf("LoadTurn(dir) = %v, want empty", got)
	}
}

func TestLoadTurnEquivalentOnCRLFAndInvalidUTF8(t *testing.T) {
	t.Run("crlf and bare cr", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "t.jsonl")
		body := assistantText("previous") + "\r\n" +
			userPrompt("the ask") + "\r" +
			assistantText("tail") + "\n"
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		assertLoadTurnEquivalent(t, p)
	})
	t.Run("invalid utf8", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "t.jsonl")
		body := []byte(assistantText("previous") + "\n")
		body = append(body, []byte("\xff\xfe garbage line\n")...)
		body = append(body, []byte(userPrompt("the ask")+"\n")...)
		body = append(body, []byte(assistantText("tail")+"\n")...)
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
		assertLoadTurnEquivalent(t, p)
	})
}

func TestLoadTurnReturnsTheBoundaryEntry(t *testing.T) {
	// The boundary prompt is included, not dropped — the hook's marker scan
	// and consult scan both expect the turn to start at it.
	entries := LoadTurn(writeTranscript(t,
		userPrompt("first ask"),
		assistantText("done one"),
		userPrompt("second ask"),
		assistantTool("Task", `{"subagent_type":"oracle"}`),
		assistantText("I'm stuck."),
	))
	if len(entries) != 3 {
		t.Fatalf("len = %d, want 3", len(entries))
	}
	if !entries[0].Content.IsString && len(entries[0].Content.Blocks) > 0 {
		if got := entries[0].Content.Blocks[0].Text; got != "second ask" {
			t.Fatalf("boundary entry text = %q, want %q", got, "second ask")
		}
	}
	if !OracleConsultedThisTurn(entries) {
		t.Fatal("OracleConsultedThisTurn = false, want true")
	}
}

// --- benchmarks -------------------------------------------------------------

// bigUserToolResult is a tool_result entry with a realistic multi-kilobyte
// payload — the bulk of a real transcript's bytes.
func bigUserToolResult(payload string) string {
	return `{"type":"user","message":{"role":"user","content":[` +
		`{"type":"tool_result","tool_use_id":"x","content":` + jstr(payload) + `}]}}`
}

// buildLargeTranscript writes a synthetic transcript shaped like a real one:
// ~3.7MB over ~1500 records, of which only the last ~22 belong to the current
// turn. Generated per run — nothing this large belongs in the repo.
func buildLargeTranscript(tb testing.TB) string {
	tb.Helper()
	p := filepath.Join(tb.TempDir(), "big.jsonl")
	filler := strings.Repeat("abcdefghij ", 440) // ~4.8KB per heavy record
	var buf bytes.Buffer
	const priorTurns = 370
	for i := 0; i < priorTurns; i++ {
		buf.WriteString(userPrompt("earlier ask "+strconv.Itoa(i)) + "\n")
		buf.WriteString(assistantText("Working on it. "+filler) + "\n")
		buf.WriteString(assistantTool("Bash", `{"command":"ls -la"}`) + "\n")
		buf.WriteString(bigUserToolResult(filler) + "\n")
	}
	// The current turn: a boundary prompt and a few dozen entries after it,
	// none of which is a real user prompt.
	buf.WriteString(userPrompt("the final ask") + "\n")
	for i := 0; i < 10; i++ {
		buf.WriteString(assistantText("Step "+strconv.Itoa(i)+". "+filler) + "\n")
		buf.WriteString(bigUserToolResult(filler) + "\n")
	}
	buf.WriteString(assistantText("I'm stuck on the failing mock.") + "\n")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		tb.Fatalf("write large fixture: %v", err)
	}
	return p
}

// TestLoadTurnEquivalentOnLargeTranscript runs the equivalence contract over
// the benchmark's own fixture, so the shape the benchmark measures is the
// shape that is proven correct.
func TestLoadTurnEquivalentOnLargeTranscript(t *testing.T) {
	p := buildLargeTranscript(t)
	assertLoadTurnEquivalent(t, p)
	if got, want := len(LoadTurn(p)), 22; got != want {
		t.Fatalf("turn length = %d, want %d", got, want)
	}
	if got, want := len(LoadEntries(p)), 370*4+22; got != want {
		t.Fatalf("total entries = %d, want %d", got, want)
	}
	if got, want := LastAssistantText(LoadTurn(p)), "I'm stuck on the failing mock."; got != want {
		t.Fatalf("LastAssistantText = %q, want %q", got, want)
	}
}

var sinkEntries []Entry

func BenchmarkLoadEntriesLarge(b *testing.B) {
	p := buildLargeTranscript(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		all := LoadEntries(p)
		sinkEntries = all[TurnStart(all):]
	}
}

func BenchmarkLoadTurnLarge(b *testing.B) {
	p := buildLargeTranscript(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkEntries = LoadTurn(p)
	}
}
