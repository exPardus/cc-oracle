package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- helpers -----------------------------------------------------------------

func backdate(t *testing.T, path string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s: %v", path, err)
	}
	return false
}

func tmpLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			out = append(out, e.Name())
		}
	}
	return out
}

// stubMarshal injects an encoding failure, restored at test end. This is the
// Go equivalent of the Python test monkeypatching oracle_hook.json.dump.
func stubMarshal(t *testing.T, err error) {
	t.Helper()
	prev := marshal
	marshal = func(any) ([]byte, error) { return nil, err }
	t.Cleanup(func() { marshal = prev })
}

// --- Dir / Path derivation ---------------------------------------------------

func TestDirPrefersOverrideOverDefault(t *testing.T) {
	cases := []struct {
		name       string
		override   string
		defaultDir string
		want       string
	}{
		{"override unset falls back to default", "", `C:\data\oracle-state`, `C:\data\oracle-state`},
		{"override wins", `C:\custom`, `C:\data\oracle-state`, `C:\custom`},
		{"both unset yields empty", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Dir(tc.override, tc.defaultDir); got != tc.want {
				t.Fatalf("Dir(%q, %q) = %q, want %q", tc.override, tc.defaultDir, got, tc.want)
			}
		})
	}
}

// Port of test_state_path_default_unchanged_from_v1: with no state_dir knob
// the record lands directly in the resolved oracle-state dir.
func TestStatePathDefaultUnchangedFromV1(t *testing.T) {
	root := t.TempDir()
	pluginData := filepath.Join(root, "oracle")
	defaultDir := filepath.Join(pluginData, "oracle-state")

	p := Path("some-session", "", defaultDir)

	if got := filepath.Dir(p); got != defaultDir {
		t.Fatalf("parent = %q, want %q", got, defaultDir)
	}
	if got := filepath.Base(filepath.Dir(p)); got != "oracle-state" {
		t.Fatalf("parent name = %q, want oracle-state", got)
	}
	if got := filepath.Dir(filepath.Dir(p)); got != pluginData {
		t.Fatalf("grandparent = %q, want %q", got, pluginData)
	}
}

// Pins the derivation itself: first 16 hex of sha1(session), lowercase, .json.
func TestPathIsFirst16HexOfSHA1(t *testing.T) {
	dir := t.TempDir()
	cases := []struct{ session, want string }{
		{"some-session", "2ddd90ff9b946703.json"}, // sha1 2ddd90ff9b9467033be...
		{"a/b", "3ec69c85a4ff9683.json"},
		{"ab", "da23614e02469a0d.json"},
		{"", "da39a3ee5e6b4b0d.json"}, // sha1 of empty input
	}
	for _, tc := range cases {
		t.Run("session="+tc.session, func(t *testing.T) {
			got := Path(tc.session, "", dir)
			if want := filepath.Join(dir, tc.want); got != want {
				t.Fatalf("Path = %q, want %q", got, want)
			}
			if !stateFileRe.MatchString(filepath.Base(got)) {
				t.Fatalf("base %q does not match the prune allowlist shape", filepath.Base(got))
			}
		})
	}
}

// Port of test_state_paths_distinct_for_colliding_session_ids. Hashing is what
// stops "a/b" and "ab" sharing a file, and stops a separator escaping the dir.
func TestStatePathsDistinctForCollidingSessionIDs(t *testing.T) {
	dir := t.TempDir()
	a := Path("a/b", "", dir)
	b := Path("ab", "", dir)
	if a == b {
		t.Fatalf("session ids a/b and ab collided on %q", a)
	}
	for _, p := range []string{a, b, Path(`..\..\escape`, "", dir), Path("../../escape", "", dir)} {
		if got := filepath.Dir(p); got != dir {
			t.Fatalf("path %q escaped the state dir (parent %q, want %q)", p, got, dir)
		}
	}
}

func TestPathCreatesStateDirectory(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "oracle-state")
	p := Path("s", "", nested)
	info, err := os.Stat(nested)
	if err != nil {
		t.Fatalf("state dir was not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%q is not a directory", nested)
	}
	if exists(t, p) {
		t.Fatalf("Path must not create the record file itself")
	}
}

// Port of test_state_dir_knob_relocates_state_files: the knob relocates the
// record, and the per-turn guard still works through the custom location.
func TestStateDirKnobRelocatesStateFiles(t *testing.T) {
	root := t.TempDir()
	defaultDir := filepath.Join(root, "oracle", "oracle-state")
	custom := filepath.Join(root, "custom-state")

	RecordBlock("sess-1", "p-1", custom, defaultDir)

	if got := filepath.Dir(Path("sess-1", custom, defaultDir)); got != custom {
		t.Fatalf("state dir = %q, want %q", got, custom)
	}
	if !exists(t, Path("sess-1", custom, defaultDir)) {
		t.Fatalf("no state record written under the custom state_dir")
	}
	if exists(t, defaultDir) {
		t.Fatalf("default state dir %q was created despite the state_dir knob", defaultDir)
	}
	if !AlreadyBlocked("sess-1", "p-1", custom, defaultDir) {
		t.Fatalf("per-turn guard broken through the custom location")
	}
}

// --- AlreadyBlocked ----------------------------------------------------------

func TestAlreadyBlockedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	RecordBlock("sess", "p-1", "", dir)

	if !AlreadyBlocked("sess", "p-1", "", dir) {
		t.Fatalf("recorded prompt not reported as blocked")
	}
	if AlreadyBlocked("sess", "p-2", "", dir) {
		t.Fatalf("a different prompt id must not read as blocked")
	}
	if AlreadyBlocked("other-sess", "p-1", "", dir) {
		t.Fatalf("a different session must not read as blocked")
	}
}

func TestRecordBlockOverwritesPreviousRecord(t *testing.T) {
	dir := t.TempDir()
	RecordBlock("sess", "p-1", "", dir)
	RecordBlock("sess", "p-2", "", dir)

	if AlreadyBlocked("sess", "p-1", "", dir) {
		t.Fatalf("superseded prompt still reads as blocked")
	}
	if !AlreadyBlocked("sess", "p-2", "", dir) {
		t.Fatalf("latest prompt does not read as blocked")
	}
	if left := tmpLeftovers(t, dir); len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}
}

// Every read or parse failure is fail-open: false, i.e. still eligible to block.
func TestAlreadyBlockedFailsOpen(t *testing.T) {
	cases := []struct {
		name     string
		content  string // "" means: write no file at all
		write    bool
		promptID string
	}{
		{name: "missing file", promptID: "p-1"},
		{name: "empty file", content: "", write: true, promptID: "p-1"},
		{name: "malformed json", content: "{not json", write: true, promptID: "p-1"},
		{name: "truncated record", content: `{"blocked_prompt": "p-`, write: true, promptID: "p-1"},
		{name: "json array not object", content: `["p-1"]`, write: true, promptID: "p-1"},
		{name: "json string not object", content: `"p-1"`, write: true, promptID: "p-1"},
		{name: "key absent", content: `{"other": "p-1"}`, write: true, promptID: "p-1"},
		{name: "null value vs empty prompt", content: `{"blocked_prompt": null}`, write: true, promptID: ""},
		{name: "numeric value", content: `{"blocked_prompt": 1}`, write: true, promptID: "1"},
		{name: "wrong prompt", content: `{"blocked_prompt": "p-9"}`, write: true, promptID: "p-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.write {
				writeFile(t, Path("sess", "", dir), tc.content)
			}
			if AlreadyBlocked("sess", tc.promptID, "", dir) {
				t.Fatalf("expected fail-open false")
			}
		})
	}
}

// BlockState's second result reproduces the control flow of Python's
// _already_blocked: a decoded value that is not a dict raises AttributeError,
// which except (OSError, ValueError) does NOT catch, so run_stop's catch-all
// swallows it and the hook exits silently WITHOUT blocking. ok=false carries
// that "abort the turn silently" signal; a missing or malformed file does not,
// because Python catches OSError/ValueError and simply proceeds.
func TestBlockStateDistinguishesCorruptFromUnusable(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		write       bool
		promptID    string
		wantBlocked bool
		wantOK      bool
	}{
		// --- the six shapes the port must reproduce exactly ---
		{name: "object matching prompt", content: `{"blocked_prompt": "p-1"}`, write: true, promptID: "p-1", wantBlocked: true, wantOK: true},
		{name: "object with other prompt", content: `{"blocked_prompt": "p-9"}`, write: true, promptID: "p-1", wantBlocked: false, wantOK: true},
		{name: "json array is corrupt", content: `["p-1"]`, write: true, promptID: "p-1", wantBlocked: false, wantOK: false},
		{name: "json string is corrupt", content: `"p-1"`, write: true, promptID: "p-1", wantBlocked: false, wantOK: false},
		{name: "json number is corrupt", content: `42`, write: true, promptID: "p-1", wantBlocked: false, wantOK: false},
		{name: "json true is corrupt", content: `true`, write: true, promptID: "p-1", wantBlocked: false, wantOK: false},
		{name: "json false is corrupt", content: `false`, write: true, promptID: "p-1", wantBlocked: false, wantOK: false},
		{name: "json null is corrupt", content: `null`, write: true, promptID: "p-1", wantBlocked: false, wantOK: false},
		{name: "malformed json is usable", content: `{not json`, write: true, promptID: "p-1", wantBlocked: false, wantOK: true},
		{name: "truncated record is usable", content: `{"blocked_prompt": "p-`, write: true, promptID: "p-1", wantBlocked: false, wantOK: true},
		{name: "empty file is usable", content: ``, write: true, promptID: "p-1", wantBlocked: false, wantOK: true},
		{name: "missing file is usable", promptID: "p-1", wantBlocked: false, wantOK: true},

		// --- object shapes: usable, never blocked ---
		{name: "empty object", content: `{}`, write: true, promptID: "p-1", wantBlocked: false, wantOK: true},
		{name: "key absent", content: `{"other": "p-1"}`, write: true, promptID: "p-1", wantBlocked: false, wantOK: true},
		{name: "null value vs empty prompt", content: `{"blocked_prompt": null}`, write: true, promptID: "", wantBlocked: false, wantOK: true},
		{name: "numeric value", content: `{"blocked_prompt": 1}`, write: true, promptID: "1", wantBlocked: false, wantOK: true},
		{name: "nested object value", content: `{"blocked_prompt": {"p": 1}}`, write: true, promptID: "p-1", wantBlocked: false, wantOK: true},
		{name: "empty prompt matches empty record", content: `{"blocked_prompt": ""}`, write: true, promptID: "", wantBlocked: true, wantOK: true},
		{name: "leading whitespace object", content: "\n  {\"blocked_prompt\": \"p-1\"}\n", write: true, promptID: "p-1", wantBlocked: true, wantOK: true},

		// Python's json accepts these bare literals and decodes each to a
		// float, so .get() raises exactly as it does for a list. Go's decoder
		// rejects them, so without the explicit case they would be misfiled as
		// merely malformed and let the turn block.
		{name: "bare NaN is corrupt", content: `NaN`, write: true, promptID: "p-1", wantBlocked: false, wantOK: false},
		{name: "bare Infinity is corrupt", content: `Infinity`, write: true, promptID: "p-1", wantBlocked: false, wantOK: false},
		{name: "bare -Infinity is corrupt", content: `-Infinity`, write: true, promptID: "p-1", wantBlocked: false, wantOK: false},
		{name: "padded NaN is corrupt", content: "  NaN\n", write: true, promptID: "p-1", wantBlocked: false, wantOK: false},
		{name: "NaNsense is only malformed", content: `NaNsense`, write: true, promptID: "p-1", wantBlocked: false, wantOK: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.write {
				writeFile(t, Path("sess", "", dir), tc.content)
			}
			blocked, ok := BlockState("sess", tc.promptID, "", dir)
			if blocked != tc.wantBlocked || ok != tc.wantOK {
				t.Fatalf("BlockState(%q) = (%v, %v), want (%v, %v)",
					tc.content, blocked, ok, tc.wantBlocked, tc.wantOK)
			}
			// AlreadyBlocked must stay a pure collapse of the first result.
			if got := AlreadyBlocked("sess", tc.promptID, "", dir); got != blocked {
				t.Fatalf("AlreadyBlocked = %v, want %v (BlockState's first result)", got, blocked)
			}
		})
	}
}

func TestBlockStateRoundTripAfterRecordBlock(t *testing.T) {
	dir := t.TempDir()
	RecordBlock("sess", "p-1", "", dir)

	blocked, ok := BlockState("sess", "p-1", "", dir)
	if !blocked || !ok {
		t.Fatalf("BlockState after RecordBlock = (%v, %v), want (true, true)", blocked, ok)
	}
	// Records we write ourselves are never corrupt.
	if _, ok := BlockState("sess", "p-2", "", dir); !ok {
		t.Fatalf("our own record read back as corrupt")
	}
}

func TestRecordBlockWritesPythonCompatibleJSON(t *testing.T) {
	dir := t.TempDir()
	RecordBlock("sess", "p-1", "", dir)

	data, err := os.ReadFile(Path("sess", "", dir))
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("record is not valid JSON (%q): %v", data, err)
	}
	if len(obj) != 1 || obj["blocked_prompt"] != "p-1" {
		t.Fatalf("record = %q, want a single blocked_prompt key", data)
	}
}

// --- atomicity ---------------------------------------------------------------

// Port of test_interrupted_state_write_preserves_previous_record: a crash
// mid-write must not clobber the existing record, because a truncated file
// would let the same prompt be blocked twice.
func TestInterruptedStateWritePreservesPreviousRecord(t *testing.T) {
	dir := t.TempDir()
	RecordBlock("sess", "p-1", "", dir)

	stubMarshal(t, errors.New("disk full"))
	RecordBlock("sess", "p-2", "", dir)

	if !AlreadyBlocked("sess", "p-1", "", dir) {
		t.Fatalf("previous record was lost by a failed write")
	}
	if AlreadyBlocked("sess", "p-2", "", dir) {
		t.Fatalf("failed write must not record the new prompt")
	}
	if left := tmpLeftovers(t, dir); len(left) != 0 {
		t.Fatalf("failed write left temp files behind: %v", left)
	}
}

func TestFailedFirstWriteLeavesNoRecordAndNoTemp(t *testing.T) {
	dir := t.TempDir()
	stubMarshal(t, errors.New("disk full"))

	RecordBlock("sess", "p-1", "", dir)

	if exists(t, Path("sess", "", dir)) {
		t.Fatalf("failed write created a record file")
	}
	if left := tmpLeftovers(t, dir); len(left) != 0 {
		t.Fatalf("failed write left temp files behind: %v", left)
	}
}

// The hook must never wedge a session: an unusable state directory is silent.
func TestRecordBlockSurvivesUnusableDirectory(t *testing.T) {
	root := t.TempDir()
	blocker := writeFile(t, filepath.Join(root, "blocker"), "not a directory")
	unusable := filepath.Join(blocker, "oracle-state")

	RecordBlock("sess", "p-1", "", unusable) // must not panic
	if AlreadyBlocked("sess", "p-1", "", unusable) {
		t.Fatalf("expected fail-open false for an unusable state dir")
	}
}

// --- pruning -----------------------------------------------------------------

// Port of test_prune_only_touches_own_state_files. CRIT: with the user-facing
// state_dir knob the resolved dir can be a user's own folder, so the age sweep
// must only ever delete files WE created.
func TestPruneOnlyTouchesOwnStateFiles(t *testing.T) {
	root := t.TempDir()
	custom := filepath.Join(root, "documents")
	if err := os.MkdirAll(custom, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defaultDir := filepath.Join(root, "oracle", "oracle-state")

	precious := writeFile(t, filepath.Join(custom, "thesis-final.docx"), "years of work")
	foreignJSON := writeFile(t, filepath.Join(custom, "notes.json"), "{}") // .json, not our shape
	staleState := writeFile(t, filepath.Join(custom, strings.Repeat("a", 16)+".json"), `{"blocked_prompt": "old"}`)
	staleTmp := writeFile(t, filepath.Join(custom, "tmp1a2b3c.tmp"), "")

	for _, f := range []string{precious, foreignJSON, staleState, staleTmp} {
		backdate(t, f, 45*24*time.Hour)
	}

	RecordBlock("sess-fresh", "p-1", custom, defaultDir) // triggers the sweep

	if !exists(t, precious) {
		t.Fatalf("the sweep deleted a user file (%s)", precious)
	}
	if !exists(t, foreignJSON) {
		t.Fatalf("the sweep deleted a foreign .json (%s)", foreignJSON)
	}
	if exists(t, staleState) {
		t.Fatalf("stale state record survived the sweep")
	}
	if exists(t, staleTmp) {
		t.Fatalf("stale temp leftover survived the sweep")
	}
}

// Port of test_prune_never_deletes_config: config.json shares the directory
// and is exempt regardless of age.
func TestPruneNeverDeletesConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, filepath.Join(dir, "config.json"), `{"doctrine": true}`)
	backdate(t, cfg, 40*24*time.Hour)

	RecordBlock("sess", "p-1", "", dir)

	if !exists(t, cfg) {
		t.Fatalf("the 30-day sweep deleted config.json")
	}
}

// Port of test_stale_state_files_pruned_on_write.
func TestStaleStateFilesPrunedOnWrite(t *testing.T) {
	dir := t.TempDir()
	RecordBlock("old-session", "p-1", "", dir)
	RecordBlock("fresh-session", "p-1", "", dir)

	old := Path("old-session", "", dir)
	backdate(t, old, 40*24*time.Hour)

	RecordBlock("new-session", "p-1", "", dir)

	if exists(t, old) {
		t.Fatalf("stale state file survived the sweep")
	}
	if !exists(t, Path("fresh-session", "", dir)) {
		t.Fatalf("fresh state file was pruned")
	}
	if !exists(t, Path("new-session", "", dir)) {
		t.Fatalf("the record being written was not created")
	}
}

func TestPruneRespectsThirtyDayBoundary(t *testing.T) {
	cases := []struct {
		name     string
		age      time.Duration
		survives bool
	}{
		{"one day old", 24 * time.Hour, true},
		{"29 days old", 29 * 24 * time.Hour, true},
		{"31 days old", 31 * 24 * time.Hour, false},
		{"a year old", 365 * 24 * time.Hour, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			victim := writeFile(t, filepath.Join(dir, strings.Repeat("b", 16)+".json"), "{}")
			backdate(t, victim, tc.age)

			RecordBlock("sess", "p-1", "", dir)

			if got := exists(t, victim); got != tc.survives {
				t.Fatalf("age %v: survives = %v, want %v", tc.age, got, tc.survives)
			}
		})
	}
}

// The allowlist decides what may ever be deleted; this pins its exact shape.
func TestPruneAllowlistShapes(t *testing.T) {
	cases := []struct {
		name    string
		allowed bool
	}{
		{"0123456789abcdef.json", true},  // our state record
		{"aaaaaaaaaaaaaaaa.json", true},  // our state record
		{"tmp1a2b3c.tmp", true},          // Python mkstemp leftover
		{"tmp_ab12_x.tmp", true},         // Python mkstemp leftover (underscores)
		{"tmp1234567890.tmp", true},      // Go os.CreateTemp(dir, "tmp*.tmp") leftover
		{"tmp.tmp", true},                // degenerate but ours
		{"0123456789ABCDEF.json", false}, // uppercase hex is not what we emit
		{"0123456789abcde.json", false},  // 15 chars
		{"0123456789abcdef0.json", false},
		{"0123456789abcdef.txt", false},
		{"notes.json", false},
		{"config.json", false},
		{"thesis-final.docx", false},
		{"1234567890.tmp", false}, // Go's DEFAULT "*.tmp" shape — see tmpPattern
		{"session.tmp", false},
		{"tmp-1a2b.tmp", false}, // hyphen is outside mkstemp's alphabet
		{"xtmp123.tmp", false},
		{"0123456789abcdef.json.bak", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stateFileRe.MatchString(tc.name) || tmpFileRe.MatchString(tc.name)
			if got != tc.allowed {
				t.Fatalf("allowlist(%q) = %v, want %v", tc.name, got, tc.allowed)
			}
		})
	}
}

// --- the Go/Python temp-name divergence this package exists to bridge --------

// Go's os.CreateTemp fills "*" with a decimal run, so the obvious "*.tmp"
// pattern yields "1234567890.tmp", which the Python-era allowlist does not
// match — every failed write would orphan a temp file forever. Assert that the
// pattern production code actually uses produces names the allowlist accepts,
// and that the naive pattern would not have.
func TestGoTempFileNamesMatchPruneAllowlist(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 64; i++ {
		f, err := os.CreateTemp(dir, tmpPattern)
		if err != nil {
			t.Fatalf("createtemp: %v", err)
		}
		name := filepath.Base(f.Name())
		_ = f.Close()
		if !tmpFileRe.MatchString(name) {
			t.Fatalf("os.CreateTemp(dir, %q) produced %q, which the prune allowlist rejects", tmpPattern, name)
		}
	}

	naive := t.TempDir()
	rejected := 0
	for i := 0; i < 16; i++ {
		f, err := os.CreateTemp(naive, "*.tmp")
		if err != nil {
			t.Fatalf("createtemp: %v", err)
		}
		name := filepath.Base(f.Name())
		_ = f.Close()
		if !tmpFileRe.MatchString(name) {
			rejected++
		}
	}
	if rejected != 16 {
		t.Fatalf(`the naive "*.tmp" pattern was matched by the allowlist %d/16 times; `+
			`the tmpPattern guard is no longer load-bearing and this test needs revisiting`, 16-rejected)
	}
}

// End-to-end counterpart: a Go-shaped temp leftover really is swept.
func TestGoTempLeftoverIsPruned(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, tmpPattern)
	if err != nil {
		t.Fatalf("createtemp: %v", err)
	}
	leftover := f.Name()
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	backdate(t, leftover, 45*24*time.Hour)

	RecordBlock("sess", "p-1", "", dir)

	if exists(t, leftover) {
		t.Fatalf("Go-created temp leftover %q survived the sweep", filepath.Base(leftover))
	}
}

// A fresh temp file from a concurrent write must not be swept.
func TestFreshTempLeftoverSurvives(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, tmpPattern)
	if err != nil {
		t.Fatalf("createtemp: %v", err)
	}
	leftover := f.Name()
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	RecordBlock("sess", "p-1", "", dir)

	if !exists(t, leftover) {
		t.Fatalf("a fresh temp file was swept")
	}
}

// A directory whose name matches the allowlist must never be removed.
func TestPruneIgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	decoy := filepath.Join(dir, strings.Repeat("c", 16)+".json")
	if err := os.Mkdir(decoy, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	backdate(t, decoy, 60*24*time.Hour)

	RecordBlock("sess", "p-1", "", dir)

	if !exists(t, decoy) {
		t.Fatalf("the sweep removed a directory")
	}
}

func TestPruneOnMissingDirectoryIsSilent(t *testing.T) {
	pruneStale(filepath.Join(t.TempDir(), "does-not-exist")) // must not panic
}
