// Package state records, per session, which prompt the Stop hook has already
// blocked, so a single prompt is nudged at most once.
//
// Port of the `_state_path` / `_already_blocked` / `_prune_stale_state` /
// `_record_block` block in hooks/oracle_hook.py.
//
// The package deliberately has zero internal dependencies: instead of a
// config.Config it takes the two directory values it actually needs, so
// `state` and `config` can be built and tested independently.
//
// Nothing here returns an error. This code runs inside a hook that must never
// wedge a session, so every failure is swallowed: the worst outcome of a
// failed state write is that a prompt gets nudged twice, which is strictly
// better than a broken session.
package state

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ttl matches Python's _STATE_TTL_SECONDS (30 * 86400). Sessions older than
// this have long ended; their state files are dead weight.
const ttl = 30 * 24 * time.Hour

// tmpPattern is the os.CreateTemp pattern for the atomic-write temp file.
//
// It is NOT the obvious "*.tmp". Go's os.CreateTemp fills `*` with a decimal
// random run, so "*.tmp" yields names like "1234567890.tmp" — which the
// Python-era allowlist regex below does not match, orphaning every temp
// leftover forever in a directory shared with a Python install. The explicit
// "tmp" prefix reproduces Python mkstemp's "tmpXXXXXXXX.tmp" shape, so one
// allowlist covers both implementations.
const tmpPattern = "tmp*.tmp"

// configName is exempt from pruning regardless of age: config.json lives in
// the same directory and is long-lived by design.
const configName = "config.json"

// Deletion allowlist. The state_dir knob is user-facing, so the resolved
// directory can be a user's own folder (Documents, a synced drive, ...). The
// age sweep may therefore only ever delete files WE create: 16-hex sha1-prefix
// state records, and ".tmp" leftovers from an interrupted atomic write.
// Anything else is untouched, whatever its age.
var (
	stateFileRe = regexp.MustCompile(`^[0-9a-f]{16}\.json$`)
	// Matches Python mkstemp's "tmp" + 8 chars of [a-z0-9_] and Go's
	// os.CreateTemp(dir, tmpPattern) "tmp" + digits.
	tmpFileRe = regexp.MustCompile(`^tmp[A-Za-z0-9_]*\.tmp$`)
)

// marshal is an indirection so tests can inject an encoding failure and prove
// the previous record survives it. Mirrors the Python test that monkeypatches
// oracle_hook.json.dump to raise.
var marshal = json.Marshal

// record is the on-disk shape: {"blocked_prompt": "<prompt id>"}.
type record struct {
	BlockedPrompt string `json:"blocked_prompt"`
}

// Dir resolves the directory holding state files.
//
// stateDirOverride is the config `state_dir` knob ("" when unset).
// defaultDir is the already-resolved <plugin data or temp>/oracle-state.
//
// Dir is pure: it performs no I/O and does not create the directory.
func Dir(stateDirOverride, defaultDir string) string {
	if stateDirOverride != "" {
		return stateDirOverride
	}
	return defaultDir
}

// Path returns the state file path for a session:
// <dir>/<first 16 hex chars of sha1(sessionID)>.json
//
// The session ID is hashed rather than used directly so that "a/b" and "ab"
// cannot collide and no path separator in a session ID can escape the
// directory.
//
// Like the Python original, Path creates the directory as a side effect
// (mkdir -p, best effort, errors ignored) so callers can write immediately.
func Path(sessionID, stateDirOverride, defaultDir string) string {
	dir := Dir(stateDirOverride, defaultDir)
	_ = os.MkdirAll(dir, 0o755)
	sum := sha1.Sum([]byte(sessionID))
	return filepath.Join(dir, hex.EncodeToString(sum[:])[:16]+".json")
}

// AlreadyBlocked reports whether this session's record already names promptID.
//
// Missing file, unreadable file, malformed JSON, or a wrongly typed value all
// report false. That is the fail-open direction: the prompt stays eligible to
// be blocked.
//
// This collapses BlockState's second result. Callers that must reproduce the
// Python hook's exact control flow want BlockState instead — see its doc for
// the one case where false does not mean "go ahead and block".
func AlreadyBlocked(sessionID, promptID, stateDirOverride, defaultDir string) bool {
	blocked, _ := BlockState(sessionID, promptID, stateDirOverride, defaultDir)
	return blocked
}

// pythonOnlyLiterals are the top-level documents Python's json module accepts
// and Go's rejects. Python decodes each to a float, so the .get() below it
// raises AttributeError — corrupt (ok=false), not merely unparseable.
var pythonOnlyLiterals = map[string]bool{"NaN": true, "Infinity": true, "-Infinity": true}

// jsonSpace is JSON's whitespace set, matching Python's scanner. Deliberately
// not strings.TrimSpace, which also trims Unicode spaces that Python rejects.
const jsonSpace = " \t\n\r"

// BlockState reports whether promptID is already recorded as blocked, and
// whether the record was usable at all.
//
// ok=false means a record exists but is not a JSON object. Python's
// _already_blocked calls .get() on the decoded value, which raises
// AttributeError for a list/string/number/bool/None — an exception its own
// except (OSError, ValueError) does not catch, so it propagates to run_stop's
// catch-all and the hook exits silently without blocking. Callers must
// reproduce that: ok=false means abort the turn silently. The doctrine is that
// a false positive (an unwanted block) is worse than a miss, so this
// accidental Python behavior is the one worth preserving.
//
// A missing or unparseable file is NOT corrupt for this purpose — Python
// catches OSError and ValueError there and simply proceeds. Those cases
// return (false, true).
func BlockState(sessionID, promptID, stateDirOverride, defaultDir string) (blocked bool, ok bool) {
	data, err := os.ReadFile(Path(sessionID, stateDirOverride, defaultDir))
	if err != nil {
		return false, true // Python: except OSError -> return False
	}
	// Decoded loosely rather than into `record` so that a null, numeric, or
	// otherwise wrongly typed blocked_prompt compares unequal instead of
	// collapsing to the zero string — Python's dict.get() == prompt_id has
	// exactly this behavior.
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		if pythonOnlyLiterals[strings.Trim(string(data), jsonSpace)] {
			return false, false
		}
		return false, true // Python: except ValueError -> return False
	}
	obj, isObject := decoded.(map[string]any)
	if !isObject {
		// Array, string, number, bool, or null. Python raises AttributeError
		// here; note that null decodes to None, and None.get raises too.
		return false, false
	}
	s, isString := obj["blocked_prompt"].(string)
	return isString && s == promptID, true
}

// RecordBlock records that promptID has been blocked for this session.
//
// The write is atomic: content goes to a temp file in the SAME directory and
// is then renamed over the target. A crash mid-write must never truncate an
// existing record, because a truncated record reads as "not blocked" and would
// let the same prompt be blocked twice — the user-visible failure this design
// exists to prevent. A temp file left behind by a failed write is removed.
//
// It also prunes state files older than 30 days first. Errors are swallowed.
func RecordBlock(sessionID, promptID, stateDirOverride, defaultDir string) {
	path := Path(sessionID, stateDirOverride, defaultDir)
	dir := filepath.Dir(path)
	pruneStale(dir)

	f, err := os.CreateTemp(dir, tmpPattern)
	if err != nil {
		return
	}
	tmp := f.Name()
	// From here on the temp file must not survive an unsuccessful rename.
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmp)
		}
	}()

	data, err := marshal(record{BlockedPrompt: promptID})
	if err != nil {
		_ = f.Close()
		return
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return
	}
	if err := f.Close(); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		return
	}
	renamed = true
}

// pruneStale deletes allowlisted files in dir older than the TTL. Mirrors
// Python's _prune_stale_state, including the explicit config.json skip, which
// is belt-and-braces: config.json does not match the allowlist anyway.
func pruneStale(dir string) {
	cutoff := time.Now().Add(-ttl)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		// Never recurse into or remove directories. Python's os.remove raises
		// on a directory (and the raise is swallowed); skipping is the same
		// outcome without relying on Go's os.Remove, which WOULD delete an
		// empty directory whose name happened to match the allowlist.
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == configName {
			continue
		}
		if !stateFileRe.MatchString(name) && !tmpFileRe.MatchString(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}
