package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	oracle "github.com/exPardus/cc-oracle"
)

// wantBaseline is an independent transcription of MARKERS in
// hooks/oracle_hook.py (lines 22-51). It exists to pin the package's own copy:
// if the two ever disagree, TestBaselineMarkers fails instead of detection
// silently changing.
var wantBaseline = []string{
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
	"hit a brick wall",
	"at a dead end",
	"i'm stumped",
	"i'm at a loss",
	"out of ideas",
	"going in circles",
	"can't work out",
	"no idea how",
	"not making progress",
	"keep getting the same",
	"still can't",
	"not getting anywhere",
}

// unsetEnv removes key for the duration of the test. t.Setenv is called first
// purely to register the restore-on-cleanup that testing provides; there is no
// t.Unsetenv in the language floor's testing package.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
}

// isolate points CLAUDE_PLUGIN_DATA at a fresh directory whose basename
// identifies THIS plugin, and clears the kill switch. Returns the base dir.
// Mirrors _isolate in tests/test_config.py.
func isolate(t *testing.T) string {
	t.Helper()
	base := filepath.Join(t.TempDir(), oracle.PluginName)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	t.Setenv(pluginDataEnv, base)
	unsetEnv(t, KillSwitchEnv)
	return base
}

// writeConfig writes cfg as JSON to <base>/oracle-state/config.json. The path
// is built independently of ConfigPath so a bug there cannot hide itself.
func writeConfig(t *testing.T, base string, cfg any) {
	t.Helper()
	dir := filepath.Join(base, dataDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	writeConfigRaw(t, base, string(raw))
}

// writeConfigRaw writes literal bytes, for the malformed-document cases.
func writeConfigRaw(t *testing.T, base, body string) {
	t.Helper()
	dir := filepath.Join(base, dataDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// assertSet compares an effective-marker set against an expected list.
func assertSet(t *testing.T, got map[string]struct{}, want []string) {
	t.Helper()
	gotKeys := sortedKeys(got)
	wantKeys := append([]string(nil), want...)
	sort.Strings(wantKeys)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Errorf("marker set mismatch\n got: %v\nwant: %v", gotKeys, wantKeys)
	}
}

func assertHas(t *testing.T, set map[string]struct{}, marker string) {
	t.Helper()
	if _, ok := set[marker]; !ok {
		t.Errorf("expected marker %q in set %v", marker, sortedKeys(set))
	}
}

func assertLacks(t *testing.T, set map[string]struct{}, marker string) {
	t.Helper()
	if _, ok := set[marker]; ok {
		t.Errorf("did not expect marker %q in set %v", marker, sortedKeys(set))
	}
}

// --- baseline marker list -----------------------------------------------------

func TestBaselineMarkers(t *testing.T) {
	if len(baselineMarkers) != 24 {
		t.Fatalf("baseline has %d markers, want 24", len(baselineMarkers))
	}
	if !reflect.DeepEqual(baselineMarkers, wantBaseline) {
		t.Fatalf("baseline drifted from the Python MARKERS tuple\n got: %q\nwant: %q",
			baselineMarkers, wantBaseline)
	}
	for _, m := range baselineMarkers {
		if got := normalizeMarker(m); got != m {
			t.Errorf("baseline marker %q is not already normalized (got %q)", m, got)
		}
	}
}

// --- config location ----------------------------------------------------------

func TestConfigPathUsesStateDirLogic(t *testing.T) {
	base := isolate(t)
	p := ConfigPath()
	if got := filepath.Base(p); got != "config.json" {
		t.Errorf("basename = %q, want config.json", got)
	}
	parent := filepath.Dir(p)
	if got := filepath.Base(parent); got != dataDirName {
		t.Errorf("parent dir = %q, want %q", got, dataDirName)
	}
	if got := filepath.Dir(parent); got != filepath.Clean(base) {
		t.Errorf("grandparent = %q, want %q", got, base)
	}
}

func TestOracleDataDirIsConfigParent(t *testing.T) {
	isolate(t)
	if got, want := filepath.Dir(ConfigPath()), OracleDataDir(); got != want {
		t.Errorf("ConfigPath parent = %q, want OracleDataDir %q", got, want)
	}
}

func TestOwnPluginDataAcceptsEveryOwnedForm(t *testing.T) {
	// All three allowlisted shapes must be accepted: bare plugin name, and the
	// hyphen/at marketplace-scoped forms.
	if len(oracle.OwnDataDirNames) == 0 {
		t.Fatal("OwnDataDirNames is empty")
	}
	for name := range oracle.OwnDataDirNames {
		t.Run(name, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), name)
			t.Setenv(pluginDataEnv, base)
			if got := OwnPluginData(); got != base {
				t.Errorf("OwnPluginData() = %q, want %q", got, base)
			}
			want := filepath.Join(base, dataDirName, "config.json")
			if got := ConfigPath(); got != want {
				t.Errorf("ConfigPath() = %q, want %q", got, want)
			}
		})
	}
}

func TestOwnPluginDataBasenameNormalization(t *testing.T) {
	// Mirrors Python's basename(normpath(env)): trailing separators and "."
	// segments must not defeat the allowlist.
	root := t.TempDir()
	owned := filepath.Join(root, oracle.PluginName)
	sep := string(os.PathSeparator)

	cases := []struct {
		name string
		env  string
	}{
		{"plain", owned},
		{"trailing os separator", owned + sep},
		{"trailing double separator", owned + sep + sep},
		{"trailing slash", owned + "/"},
		{"dot segment", owned + sep + "."},
		{"redundant dot segment", filepath.Join(root, ".", oracle.PluginName)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(pluginDataEnv, tc.env)
			if got := OwnPluginData(); got != tc.env {
				t.Errorf("OwnPluginData() = %q, want the env value %q", got, tc.env)
			}
			want := filepath.Join(owned, dataDirName, "config.json")
			if got := ConfigPath(); got != want {
				t.Errorf("ConfigPath() = %q, want %q", got, want)
			}
		})
	}
}

func TestOwnPluginDataRejectsForeignDirs(t *testing.T) {
	// The allowlist is exact forms only. A prefix test would accept
	// "oracle-db-tools", which belongs to somebody else.
	root := t.TempDir()
	cases := []string{
		"codex-openai-codex",
		oracle.PluginName + "-db-tools",
		oracle.PluginName + "-",
		oracle.PluginName + "s",
		strings.ToUpper(oracle.PluginName),
		"not-" + oracle.PluginName,
		oracle.PluginName + "@some-other-marketplace",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(pluginDataEnv, filepath.Join(root, name))
			if got := OwnPluginData(); got != "" {
				t.Errorf("OwnPluginData() = %q for foreign dir %q, want \"\"", got, name)
			}
		})
	}
}

func TestOwnPluginDataUnsetIsEmpty(t *testing.T) {
	unsetEnv(t, pluginDataEnv)
	if got := OwnPluginData(); got != "" {
		t.Errorf("OwnPluginData() = %q with env unset, want \"\"", got)
	}
}

func TestForeignEnvDoesNotRedirectConfig(t *testing.T) {
	// A leaked env var must not let another plugin's dir supply our config.
	foreign := filepath.Join(t.TempDir(), "codex-openai-codex")
	dir := filepath.Join(foreign, dataDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"stop_hook": false}`), 0o644); err != nil {
		t.Fatalf("write foreign config: %v", err)
	}
	t.Setenv(pluginDataEnv, foreign)
	unsetEnv(t, KillSwitchEnv)

	p := ConfigPath()
	if strings.HasPrefix(p, filepath.Clean(foreign)+string(os.PathSeparator)) {
		t.Errorf("config path %q lives under the foreign dir %q", p, foreign)
	}
	if cfg := Load(); !cfg.StopHook {
		t.Error("foreign config was read: stop_hook came back false")
	}
}

func TestConfigPathFallsBackToTempDir(t *testing.T) {
	unsetEnv(t, pluginDataEnv)
	p := ConfigPath()
	if got := filepath.Base(p); got != "config.json" {
		t.Errorf("basename = %q, want config.json", got)
	}
	parent := filepath.Dir(p)
	if got := filepath.Base(parent); got != dataDirName {
		t.Errorf("parent dir = %q, want %q", got, dataDirName)
	}
	if got, want := filepath.Dir(parent), filepath.Clean(os.TempDir()); got != want {
		t.Errorf("grandparent = %q, want OS temp dir %q", got, want)
	}
}

// --- Load / EffectiveMarkers --------------------------------------------------

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if !cfg.StopHook {
		t.Error("StopHook default = false, want true")
	}
	if !cfg.Doctrine {
		t.Error("Doctrine default = false, want true")
	}
	if cfg.StateDir != "" {
		t.Errorf("StateDir default = %q, want \"\"", cfg.StateDir)
	}
	if len(cfg.MarkersAdd) != 0 || len(cfg.MarkersRemove) != 0 {
		t.Errorf("marker knobs default non-empty: add=%v remove=%v", cfg.MarkersAdd, cfg.MarkersRemove)
	}
}

func TestDefaultsWhenNoConfig(t *testing.T) {
	isolate(t)
	cfg := Load()
	if !cfg.StopHook {
		t.Error("stop_hook = false, want true")
	}
	if !cfg.Doctrine {
		t.Error("doctrine = false, want true")
	}
	if cfg.StateDir != "" {
		t.Errorf("state_dir = %q, want \"\"", cfg.StateDir)
	}
	assertSet(t, EffectiveMarkers(cfg), wantBaseline)
}

func TestConfigAddsAndRemovesMarkers(t *testing.T) {
	base := isolate(t)
	writeConfig(t, base, map[string]any{
		"markers": map[string]any{
			"add":    []any{"No Clue  How"},
			"remove": []any{"I'M CONFUSED"},
		},
	})
	marks := EffectiveMarkers(Load())
	assertHas(t, marks, "no clue how")    // normalized: lowercase, collapsed whitespace
	assertLacks(t, marks, "i'm confused") // remove is case-insensitive
	assertHas(t, marks, "i'm stuck")      // rest of the builtins intact
}

func TestMalformedConfigIgnored(t *testing.T) {
	base := isolate(t)
	writeConfigRaw(t, base, "{not json")
	cfg := Load()
	if !cfg.StopHook {
		t.Error("stop_hook = false after malformed config, want true")
	}
	if !cfg.Doctrine {
		t.Error("doctrine = false after malformed config, want true")
	}
	assertSet(t, EffectiveMarkers(cfg), wantBaseline)
}

func TestNonObjectConfigIgnored(t *testing.T) {
	// A well-formed document that is not an object must fall back to defaults,
	// matching Python's isinstance(raw, dict) gate.
	for _, body := range []string{`[1, 2, 3]`, `"stop_hook"`, `42`, `null`, `true`, ``} {
		t.Run(body, func(t *testing.T) {
			base := isolate(t)
			writeConfigRaw(t, base, body)
			cfg := Load()
			if !cfg.StopHook || !cfg.Doctrine || cfg.StateDir != "" {
				t.Errorf("non-object config %q changed the defaults: %+v", body, cfg)
			}
			assertSet(t, EffectiveMarkers(cfg), wantBaseline)
		})
	}
}

func TestUnreadableConfigIgnored(t *testing.T) {
	// config.json as a directory: os.ReadFile fails, which is Python's OSError
	// branch. Defaults must stand.
	base := isolate(t)
	if err := os.MkdirAll(filepath.Join(base, dataDirName, "config.json"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := Load()
	if !cfg.StopHook || !cfg.Doctrine {
		t.Errorf("unreadable config changed the defaults: %+v", cfg)
	}
}

func TestWrongTypesIgnoredPerKey(t *testing.T) {
	base := isolate(t)
	writeConfig(t, base, map[string]any{
		"stop_hook": "no", // not a bool -> ignored
		"doctrine":  0,    // not a bool -> ignored
		"state_dir": 42,   // not a str  -> ignored
		"markers":   map[string]any{"add": []any{"ok marker", 42}, "remove": "i'm stuck"},
	})
	cfg := Load()
	if !cfg.StopHook {
		t.Error("stop_hook = false, want true (\"no\" is not a bool)")
	}
	if !cfg.Doctrine {
		t.Error("doctrine = false, want true (0 is not a bool)")
	}
	if cfg.StateDir != "" {
		t.Errorf("state_dir = %q, want \"\" (42 is not a string)", cfg.StateDir)
	}
	if !reflect.DeepEqual(cfg.MarkersAdd, []string{"ok marker"}) {
		t.Errorf("markers.add = %q, want [\"ok marker\"] (the int is dropped)", cfg.MarkersAdd)
	}
	if len(cfg.MarkersRemove) != 0 {
		t.Errorf("markers.remove = %q, want empty (a string is not a list)", cfg.MarkersRemove)
	}
	marks := EffectiveMarkers(cfg)
	assertHas(t, marks, "ok marker")
	assertHas(t, marks, "i'm stuck") // bad remove shape -> no removal
}

func TestNullValuesLeaveDefaults(t *testing.T) {
	// Go-specific trap: json.Unmarshal of `null` into a bool succeeds silently
	// and would flip a default if the value were assigned unconditionally.
	base := isolate(t)
	writeConfigRaw(t, base,
		`{"stop_hook": null, "doctrine": null, "state_dir": null, "markers": null}`)
	cfg := Load()
	if !cfg.StopHook {
		t.Error("stop_hook = false after null, want true")
	}
	if !cfg.Doctrine {
		t.Error("doctrine = false after null, want true")
	}
	if cfg.StateDir != "" {
		t.Errorf("state_dir = %q after null, want \"\"", cfg.StateDir)
	}
	assertSet(t, EffectiveMarkers(cfg), wantBaseline)
}

func TestGoodKeysApplyAlongsideBadOnes(t *testing.T) {
	base := isolate(t)
	writeConfig(t, base, map[string]any{
		"stop_hook": false,      // good
		"doctrine":  "nope",     // bad -> default true stands
		"state_dir": "  ",       // blank -> ignored
		"markers":   []any{"x"}, // not an object -> ignored
	})
	cfg := Load()
	if cfg.StopHook {
		t.Error("stop_hook = true, want false (well-typed key must apply)")
	}
	if !cfg.Doctrine {
		t.Error("doctrine = false, want true (bad sibling key must be ignored)")
	}
	if cfg.StateDir != "" {
		t.Errorf("state_dir = %q, want \"\" (blank string is ignored)", cfg.StateDir)
	}
	assertSet(t, EffectiveMarkers(cfg), wantBaseline)
}

func TestStateDirKnobLoaded(t *testing.T) {
	base := isolate(t)
	custom := filepath.Join(base, "custom-state")
	writeConfig(t, base, map[string]any{"state_dir": custom})
	if got := Load().StateDir; got != custom {
		t.Errorf("state_dir = %q, want %q (stored verbatim, not trimmed)", got, custom)
	}
}

func TestStateDirBlankIgnored(t *testing.T) {
	base := isolate(t)
	writeConfig(t, base, map[string]any{"state_dir": "   \t "})
	if got := Load().StateDir; got != "" {
		t.Errorf("state_dir = %q, want \"\" for a whitespace-only value", got)
	}
}

func TestBoolKnobsAcceptFalse(t *testing.T) {
	base := isolate(t)
	writeConfig(t, base, map[string]any{"stop_hook": false, "doctrine": false})
	cfg := Load()
	if cfg.StopHook {
		t.Error("stop_hook = true, want false")
	}
	if cfg.Doctrine {
		t.Error("doctrine = true, want false")
	}
}

func TestRemoveUnknownMarkerIsNoop(t *testing.T) {
	base := isolate(t)
	writeConfig(t, base, map[string]any{
		"markers": map[string]any{"remove": []any{"never was a marker"}},
	})
	assertSet(t, EffectiveMarkers(Load()), wantBaseline)
}

func TestEmptyMarkerListsLeaveDefaults(t *testing.T) {
	// An add/remove list that cleans to nothing leaves the default in place.
	cases := map[string]any{
		"both empty":      map[string]any{"add": []any{}, "remove": []any{}},
		"only blanks":     map[string]any{"add": []any{"", "   "}, "remove": []any{" \t "}},
		"only non-string": map[string]any{"add": []any{1, true, nil}, "remove": []any{map[string]any{}}},
	}
	for name, markers := range cases {
		t.Run(name, func(t *testing.T) {
			base := isolate(t)
			writeConfig(t, base, map[string]any{"markers": markers})
			cfg := Load()
			if len(cfg.MarkersAdd) != 0 || len(cfg.MarkersRemove) != 0 {
				t.Errorf("expected empty knobs, got add=%q remove=%q", cfg.MarkersAdd, cfg.MarkersRemove)
			}
			assertSet(t, EffectiveMarkers(cfg), wantBaseline)
		})
	}
}

func TestRemovedFamilyKeyDropsFamily(t *testing.T) {
	// Families are baseline entries and individually removable like any core
	// marker — the knob mutates the POST-variant-family set.
	base := isolate(t)
	writeConfig(t, base, map[string]any{
		"markers": map[string]any{"remove": []any{"Hit A Brick Wall"}},
	})
	marks := EffectiveMarkers(Load())
	assertLacks(t, marks, "hit a brick wall")
	assertHas(t, marks, "at a dead end")
	if len(marks) != len(wantBaseline)-1 {
		t.Errorf("set size = %d, want %d", len(marks), len(wantBaseline)-1)
	}
}

func TestRemoveThenAddOrdering(t *testing.T) {
	// Python applies remove first, then add: a marker in both lists survives.
	base := isolate(t)
	writeConfig(t, base, map[string]any{
		"markers": map[string]any{"add": []any{"i'm stuck"}, "remove": []any{"i'm stuck"}},
	})
	assertHas(t, EffectiveMarkers(Load()), "i'm stuck")
}

func TestEffectiveMarkersIsIndependentPerCall(t *testing.T) {
	// Mutating a returned set must not poison the baseline for the next call.
	cfg := Defaults()
	first := EffectiveMarkers(cfg)
	delete(first, "i'm stuck")
	assertHas(t, EffectiveMarkers(cfg), "i'm stuck")
}

// --- normalization ------------------------------------------------------------

func TestNormalizeMarker(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"already normal", "i'm stuck", "i'm stuck"},
		{"trim and lowercase", "  I'm Stuck  ", "i'm stuck"},
		{"collapse double space", "No Clue  How", "no clue how"},
		{"tab", "a\tb", "a b"},
		{"newline", "a\nb", "a b"},
		{"crlf tab run", "a\r\n\tb", "a b"},
		{"vertical tab", "a\vb", "a b"},
		{"form feed", "a\fb", "a b"},
		{"empty", "", ""},
		{"blank", "   ", ""},
		{"mixed case and spacing", "MiXeD   CASE\tMarker", "mixed case marker"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeMarker(tc.in); got != tc.want {
				t.Errorf("normalizeMarker(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNormalizeMarkerUnicodeWhitespace covers the separators Go's ASCII-only
// `\s` would miss. Python's `\s` on str patterns is Unicode-aware, so the Go
// pattern spells the classes out; each rune here is a regression against
// silently reverting to a bare `\s+`. Runes are given numerically so the source
// stays ASCII and the intent is readable.
func TestNormalizeMarkerUnicodeWhitespace(t *testing.T) {
	cases := []struct {
		name string
		sep  rune
	}{
		{"nbsp", 0x00A0},
		{"next line", 0x0085},
		{"ogham space mark", 0x1680},
		{"en quad", 0x2000},
		{"em space", 0x2003},
		{"thin space", 0x2009},
		{"hair space", 0x200A},
		{"line separator", 0x2028},
		{"paragraph separator", 0x2029},
		{"narrow nbsp", 0x202F},
		{"medium mathematical space", 0x205F},
		{"ideographic space", 0x3000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sep := string(tc.sep)
			// Single separator, a run of them, and a mixed ASCII/Unicode run all
			// collapse to one plain space; leading and trailing ones are trimmed.
			checks := []struct{ in, want string }{
				{"a" + sep + "b", "a b"},
				{"a" + sep + sep + "b", "a b"},
				{"a" + sep + " \t" + sep + "b", "a b"},
				{sep + "No Clue" + sep + sep + "How" + sep, "no clue how"},
				{sep, ""},
			}
			for _, c := range checks {
				if got := normalizeMarker(c.in); got != c.want {
					t.Errorf("normalizeMarker(%q) = %q, want %q", c.in, got, c.want)
				}
			}
		})
	}
}

func TestConfigMarkersAreNormalizedOnLoad(t *testing.T) {
	base := isolate(t)
	writeConfig(t, base, map[string]any{
		"markers": map[string]any{
			"add":    []any{"  Totally   BAFFLED  "},
			"remove": []any{"  I'M   STUCK  "},
		},
	})
	cfg := Load()
	if !reflect.DeepEqual(cfg.MarkersAdd, []string{"totally baffled"}) {
		t.Errorf("markers.add = %q, want [\"totally baffled\"]", cfg.MarkersAdd)
	}
	if !reflect.DeepEqual(cfg.MarkersRemove, []string{"i'm stuck"}) {
		t.Errorf("markers.remove = %q, want [\"i'm stuck\"]", cfg.MarkersRemove)
	}
	marks := EffectiveMarkers(cfg)
	assertHas(t, marks, "totally baffled")
	assertLacks(t, marks, "i'm stuck") // removal works despite the odd spacing
}

// --- kill switch --------------------------------------------------------------

func TestDisabledByEnv(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"1", true},
		{"true", true},
		{"yes", true},
		{"TRUE", true},
		{"YES", true},
		{"True", true},
		{"  1  ", true},
		{"\tyes\n", true},
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"off", false},
		{"2", false},
		{"truthy", false},
		{"1 1", false},
	}
	for _, tc := range cases {
		t.Run("val="+tc.val, func(t *testing.T) {
			t.Setenv(KillSwitchEnv, tc.val)
			if got := DisabledByEnv(); got != tc.want {
				t.Errorf("DisabledByEnv() with %q = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}

func TestDisabledByEnvUnset(t *testing.T) {
	unsetEnv(t, KillSwitchEnv)
	if DisabledByEnv() {
		t.Error("DisabledByEnv() = true with the variable unset, want false")
	}
}

// badBytes splices raw bytes between two JSON fragments.
//
// The offending bytes are given numerically rather than as string escapes: an
// escape sequence written here is liable to be rewritten into a real character
// by a formatter or an editor, which would silently turn these fixtures into
// valid UTF-8 and make the tests pass without exercising anything.
func badBytes(prefix string, bad []byte, suffix string) []byte {
	out := append([]byte(prefix), bad...)
	return append(out, suffix...)
}

// TestNonUTF8ConfigIsDiscarded pins parity with Python's strict decode.
//
// Python reads config.json with read_text(encoding="utf-8"): a single non-UTF-8
// byte raises UnicodeDecodeError and the entire config is discarded. Go's json
// decoder would instead substitute U+FFFD inside strings and honour the file.
// A config saved as cp1252 (Notepad's "ANSI") is an ordinary Windows mistake,
// and either implementation may win the dispatch chain, so the two must agree
// on whether such a file applies at all.
func TestNonUTF8ConfigIsDiscarded(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{"cp1252 accented state_dir", badBytes(
			`{"stop_hook": false, "state_dir": "/opt/jos`, []byte{0xe9}, `/oracle"}`)},
		{"cp1252 in an unused key", badBytes(
			`{"doctrine": false, "note": "caf`, []byte{0xe9}, `"}`)},
		{"lone continuation byte", badBytes(
			`{"stop_hook": false, "n": "`, []byte{0x80}, `"}`)},
		{"truncated multibyte", badBytes(
			`{"stop_hook": false, "n": "`, []byte{0xe2, 0x82}, `"}`)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if utf8.Valid(c.body) {
				t.Fatal("fixture is valid UTF-8; it does not exercise the branch")
			}
			dir := isolateConfigDir(t)
			if err := os.WriteFile(filepath.Join(dir, "config.json"), c.body, 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := Load()
			if !cfg.StopHook || !cfg.Doctrine {
				t.Errorf("invalid UTF-8 must discard the whole config; got %+v", cfg)
			}
			if cfg.StateDir != "" {
				t.Errorf("StateDir = %q, want empty", cfg.StateDir)
			}
		})
	}
}

// TestValidUTF8ConfigStillApplies is the control: the same shape as valid UTF-8
// must be honoured, proving the check rejects on encoding rather than rejecting
// everything.
func TestValidUTF8ConfigStillApplies(t *testing.T) {
	dir := isolateConfigDir(t)
	body := badBytes(
		`{"stop_hook": false, "state_dir": "/opt/jos`, []byte{0xc3, 0xa9}, `/oracle"}`)
	if !utf8.Valid(body) {
		t.Fatal("control fixture must be valid UTF-8")
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Load()
	if cfg.StopHook {
		t.Error("valid UTF-8 config was not applied")
	}
	if cfg.StateDir == "" {
		t.Error("StateDir should have been set")
	}
}

// isolateConfigDir points CLAUDE_PLUGIN_DATA at an owned temp dir and returns
// the oracle-state directory inside it.
func isolateConfigDir(t *testing.T) string {
	t.Helper()
	base := filepath.Join(t.TempDir(), "oracle")
	dir := filepath.Join(base, "oracle-state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_PLUGIN_DATA", base)
	return dir
}

// --- failure_streak knob -----------------------------------------------------

func TestFailureStreakDefaultsToThree(t *testing.T) {
	isolateConfigDir(t)
	if got := Load().FailureStreak; got != 3 {
		t.Errorf("FailureStreak = %d, want 3", got)
	}
}

func TestFailureStreakParsing(t *testing.T) {
	cases := []struct {
		body string
		want int
	}{
		{`{"failure_streak": 5}`, 5},
		{`{"failure_streak": 0}`, 0},
		{`{"failure_streak": 1}`, 1},
		// wrong types and out-of-range values leave the default in place.
		{`{"failure_streak": "3"}`, 3},
		{`{"failure_streak": 3.5}`, 3},
		{`{"failure_streak": true}`, 3},
		{`{"failure_streak": -1}`, 3},
		{`{"failure_streak": null}`, 3},
		{`{"failure_streak": []}`, 3},
	}
	for _, c := range cases {
		t.Run(c.body, func(t *testing.T) {
			dir := isolateConfigDir(t)
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := Load().FailureStreak; got != c.want {
				t.Errorf("FailureStreak = %d, want %d", got, c.want)
			}
		})
	}
}
