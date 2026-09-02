// Package config resolves the plugin-local configuration surface (v1.1).
//
// Config lives in exactly one optional file at
// <own CLAUDE_PLUGIN_DATA or OS temp>/oracle-state/config.json — the same
// base-dir resolution the hook uses for its per-turn state, so the location is
// environment-independent (no cwd, no HOME) and, like the state, cannot be
// redirected by a foreign plugin's leaked CLAUDE_PLUGIN_DATA.
//
// Failure posture is fail-open and per key: a missing, unreadable, or
// malformed file yields the defaults, and a key whose JSON type is wrong is
// ignored while the well-typed keys in the same file still apply. Config can
// only tune behavior, never break it.
package config

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	oracle "github.com/exPardus/cc-oracle"
)

// KillSwitchEnv is the environment kill switch consulted by both entry points.
const KillSwitchEnv = "CC_ORACLE_DISABLE"

// pluginDataEnv is inherited by child processes, so another plugin's value
// genuinely leaks into ours (live incident: codex's data dir received our
// state file). See OwnPluginData for the guard.
const pluginDataEnv = "CLAUDE_PLUGIN_DATA"

// dataDirName is the subdirectory we own inside the resolved base dir.
const dataDirName = "oracle-state"

// baselineMarkers is a verbatim copy of MARKERS in hooks/oracle_hook.py
// (lines 22-51), in source order.
//
// DUPLICATION, DELIBERATE AND TEMPORARY: internal/detect owns the canonical
// list, but phase-1 packages must not import each other (see
// docs/specs/2026-08-08-go-port-contract.md), so this package carries its own
// copy until the hook package reconciles the two into one. TestBaselineMarkers
// pins the exact contents so a drift is a test failure, not a silent
// behavior change.
var baselineMarkers = []string{
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
	// Deflection variant families. These entries are FAMILY KEYS, not
	// substrings: detect maps each to a first-person-anchored regex. They stay
	// in the baseline so markers.remove can drop a family like any core marker.
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

// whitespaceRun mirrors Python's `\s+` on str patterns, which is
// Unicode-aware. Go's `\s` is ASCII-only, so the Unicode separator classes are
// spelled out to keep non-breaking spaces and friends collapsing identically.
var whitespaceRun = regexp.MustCompile(`[\s\p{Z}\x{0085}\x{000B}]+`)

// Config is the effective configuration: the defaults overlaid with the
// well-typed keys of config.json.
type Config struct {
	// StopHook enables the Stop-hook safety net. Default true.
	StopHook bool
	// Doctrine enables SessionStart doctrine injection. Default true.
	Doctrine bool
	// MarkersAdd holds normalized markers added on top of the baseline.
	MarkersAdd []string
	// MarkersRemove holds normalized markers subtracted from the baseline.
	MarkersRemove []string
	// StateDir overrides where per-turn block-state files live. "" means unset.
	StateDir string
	// FailureStreak is how many consecutive non-probe tool failures trigger a
	// nudge on their own, independent of what the model said. 0 disables the
	// behavioral trigger. Default 3.
	FailureStreak int
}

// Defaults returns the zero-config configuration. Every default reproduces v1
// behavior exactly.
func Defaults() Config {
	return Config{
		StopHook:      true,
		Doctrine:      true,
		FailureStreak: 3,
	}
}

// OwnPluginData returns the value of CLAUDE_PLUGIN_DATA, but only when it is
// ours: the cleaned basename must be an exact member of oracle.OwnDataDirNames.
// Unset or foreign returns "".
//
// The allowlist is exact known forms, never a prefix test — an open
// strings.HasPrefix(name, "oracle-") would accept an unrelated "oracle-db-tools".
// The value is returned verbatim (not cleaned), mirroring Python's
// _own_plugin_data; path joining cleans it downstream.
func OwnPluginData() string {
	env := os.Getenv(pluginDataEnv)
	if env == "" {
		return ""
	}
	// filepath.Clean + Base mirrors Python's basename(normpath(env)):
	// trailing separators, "." segments, and mixed separators all collapse
	// before the basename is taken.
	if _, ok := oracle.OwnDataDirNames[filepath.Base(filepath.Clean(env))]; ok {
		return env
	}
	return ""
}

// OracleDataDir is the directory holding config.json and, by default, the
// per-turn state files. Falls back to the OS temp dir when CLAUDE_PLUGIN_DATA
// is unset or belongs to another plugin.
func OracleDataDir() string {
	base := OwnPluginData()
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, dataDirName)
}

// ConfigPath is the single optional config file location.
func ConfigPath() string {
	return filepath.Join(OracleDataDir(), "config.json")
}

// Load returns the effective config. It never errors: a missing, unreadable,
// malformed, or non-object file yields the defaults, and a wrong-typed key is
// ignored while its siblings still apply.
//
// The document is decoded into map[string]any rather than a struct on purpose:
// a rigid struct unmarshal fails the whole file on one bad key, which would
// turn a single typo into a total config loss.
func Load() Config {
	cfg := Defaults()

	raw, err := os.ReadFile(ConfigPath())
	if err != nil {
		return cfg
	}
	// Python reads this file with read_text(encoding="utf-8"), which is STRICT:
	// one non-UTF-8 byte raises UnicodeDecodeError (a ValueError) and the whole
	// config is discarded in favour of the defaults. Go's json decoder instead
	// substitutes U+FFFD inside strings and carries on, so without this check a
	// config saved as cp1252 — what Notepad's "ANSI" produces, and entirely
	// plausible for a Windows user writing a state_dir with an accented name —
	// would be honoured here and ignored by the Python fallback. The two halves
	// of the dispatch chain must not disagree about whether a config applies.
	if !utf8.Valid(raw) {
		return cfg
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		// Covers both invalid JSON and a well-formed non-object document,
		// matching Python's ValueError / isinstance(raw, dict) pair.
		return cfg
	}

	if v, ok := doc["stop_hook"].(bool); ok {
		cfg.StopHook = v
	}
	if v, ok := doc["doctrine"].(bool); ok {
		cfg.Doctrine = v
	}
	// encoding/json decodes every number to float64, so a threshold must be
	// checked for integrality here the way Python's isinstance(x, int) does --
	// and 3.5 must be rejected, not truncated.
	if v, ok := doc["failure_streak"].(float64); ok && v >= 0 && v == math.Trunc(v) {
		cfg.FailureStreak = int(v)
	}
	if v, ok := doc["state_dir"].(string); ok && strings.TrimSpace(v) != "" {
		// Stored verbatim, like Python: only the emptiness test trims.
		cfg.StateDir = v
	}
	if markers, ok := doc["markers"].(map[string]any); ok {
		if cleaned := cleanMarkerList(markers["add"]); len(cleaned) > 0 {
			cfg.MarkersAdd = cleaned
		}
		if cleaned := cleanMarkerList(markers["remove"]); len(cleaned) > 0 {
			cfg.MarkersRemove = cleaned
		}
	}
	return cfg
}

// cleanMarkerList normalizes the string elements of a JSON list, dropping
// non-strings and blanks. A non-list, or a list that cleans to nothing, yields
// nil so the caller leaves the default in place.
func cleanMarkerList(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range items {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		out = append(out, normalizeMarker(s))
	}
	return out
}

// EffectiveMarkers is the baseline marker set with the config's removals
// applied and then its additions, in that order. Both sides are normalized, so
// removal is case- and whitespace-insensitive.
//
// Config mutates the POST-variant-family set: the built-in families are part of
// the baseline, so a family key can be removed like any core marker.
func EffectiveMarkers(cfg Config) map[string]struct{} {
	marks := make(map[string]struct{}, len(baselineMarkers)+len(cfg.MarkersAdd))
	for _, m := range baselineMarkers {
		marks[normalizeMarker(m)] = struct{}{}
	}
	for _, m := range cfg.MarkersRemove {
		delete(marks, m)
	}
	for _, m := range cfg.MarkersAdd {
		marks[m] = struct{}{}
	}
	return marks
}

// DisabledByEnv reports whether the environment kill switch is engaged.
func DisabledByEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(KillSwitchEnv))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// normalizeMarker mirrors Python _normalize_marker: collapse whitespace runs to
// a single space, trim, lowercase.
func normalizeMarker(m string) string {
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(strings.ToLower(m), " "))
}
