# Go port — package contract

Port of `hooks/oracle_hook.py` (585 lines, stdlib-only) to Go. Behavior is
frozen: the Python file is the specification, and `tests/*.py` is its
executable form. Every Python test must have a Go equivalent that asserts the
same thing.

**Prime directive, inherited from the original:** the hook must never wedge a
session. Any unexpected input, any error, any panic → exit 0 with empty
stdout. In Go this means every entry point recovers from panics; nothing
returns a nonzero code except a genuine block decision (which is still exit 0
plus stdout JSON).

Module: `github.com/exPardus/cc-oracle`, language floor `go 1.21`.

## Layout

```
manifest.go                    package oracle   — plugin identity (DONE, embedded)
internal/detect/               package detect   — marker/idiom detection
internal/config/               package config   — config.json + data dir resolution
internal/transcript/           package transcript — JSONL parsing, turn boundary
internal/state/                package state    — per-turn block record
internal/hook/                 package hook     — RunStop / RunSessionStart
cmd/oracle-hook/main.go        package main     — CLI entry
```

Packages in phase 1 (`detect`, `config`, `transcript`, `state`) must not
import each other. `hook` imports all four. This is what lets them be built
and tested independently and in parallel.

## RE2 gaps — read this before writing any regex

Go's `regexp` is RE2. It has **no lookahead and no lookbehind**. Two patterns
in the Python source depend on them and must be restructured, not
approximated. A "close enough" regex that changes which sentences fire is a
behavior change and will fail the differential test.

### 1. `no idea how(?! long| many| much| big| often)`

Python excludes quantity hedges — "I have no idea how long CI takes" is a
benign hedge about an unknown quantity, not stuckness.

Do **not** solve this with a separate veto regex over the whole text. That is
wrong when one message contains both a genuine "no idea why" and a benign "no
idea how long": the veto would suppress the real hit.

Correct approach — match, then inspect the suffix at each match site:

1. Write the family pattern *without* the lookahead, capturing whether the
   match ended in `how` or `why`.
2. Use `FindAllStringSubmatchIndex` to get every match with offsets.
3. For each match that ended in `how`, look at the text immediately following
   the match end. If it begins with ` long`, ` many`, ` much`, ` big`, or
   ` often`, skip that match and keep scanning.
4. Any surviving match is a hit.

This is exactly equivalent to the lookahead, per match site.

### 2. `(?<=[.!?])\s+` sentence split

Python splits on whitespace *after* sentence punctuation, keeping the
punctuation attached to the preceding sentence. Implement by hand: scan for
`[.!?]` followed by one or more whitespace characters, cut immediately after
the punctuation, and skip the whitespace run. Empty pieces are dropped, and
the input is trimmed first — `_sentences` in Python does
`[s for s in split(text.strip()) if s]`.

### 3. `\s` is not the same class

Python's `\s` on `str` patterns matches Unicode whitespace. Go's `\s` is ASCII
only (`[\t\n\f\r ]`). Anywhere the Python source uses `\s`, use
`[\s\p{Z}\x{0085}\x{000B}]` in Go so non-breaking spaces and friends collapse
identically. This matters in two places: `_normalize_marker` and the
whitespace collapse at the top of `marker_hit`.

## package detect

```go
// Markers is the baseline marker set, in the Python source's order.
var Markers []string

// Normalize mirrors Python _normalize_marker: collapse whitespace runs to a
// single space, trim, lowercase.
func Normalize(s string) string

// MarkerHit reports whether text states a stuckness marker. markers is the
// effective set (see config.EffectiveMarkers); entries that name an idiom
// family are matched by that family's anchored regex, all others by plain
// substring. Mirrors Python marker_hit, including the leading
// lowercase + whitespace-collapse of text.
func MarkerHit(text string, markers map[string]struct{}) bool

// StripQuoted removes fenced code, blockquote lines, inline code, and
// double-quoted spans — quoting a marker is not stating one. Single quotes are
// NOT stripped: apostrophes in "I'm" would corrupt matching.
func StripQuoted(text string) string

// IsQuestionTurn reports whether the turn is, or ends with, a question to the
// user. Marker-in-question beats marker-matched.
func IsQuestionTurn(text string, markers map[string]struct{}) bool

// ShouldNudge is the composition: a marker outside quoted spans, and not a
// question turn.
func ShouldNudge(text string, markers map[string]struct{}) bool
```

`MarkerHit` and friends take the effective marker set. For a default-config
call, pass `detect.DefaultMarkerSet()` — add that helper, returning the
normalized `Markers` as a set, so `detect` stays independent of `config`.

The eight idiom families and their exact regexes are in the Python source at
`_FAMILY_PATTERNS`. Port them character for character apart from the two RE2
gaps above. The `_ADV` intensifier allowlist is a **closed list** — never
`\w+` — because that is what keeps `not` breaking adjacency, so
"I am not out of ideas" cannot match.

Family keys stay in the marker set so `markers.remove` can drop a whole family
like any other marker.

Port `tests/test_detection.py` in full, including every `parametrize` case.

## package config

```go
type Config struct {
    StopHook      bool     // default true
    Doctrine      bool     // default true
    MarkersAdd    []string // normalized
    MarkersRemove []string // normalized
    StateDir      string   // "" means unset
}

func Defaults() Config
func Load() Config                                  // never errors; malformed → defaults
func EffectiveMarkers(cfg Config) map[string]struct{}
func OracleDataDir() string                         // <own plugin data or os.TempDir()>/oracle-state
func ConfigPath() string                            // OracleDataDir()/config.json
func OwnPluginData() string                         // "" if unset or foreign
func DisabledByEnv() bool                           // CC_ORACLE_DISABLE in 1/true/yes, case-insensitive
```

`OwnPluginData` reads `CLAUDE_PLUGIN_DATA` and accepts it **only** when its
cleaned basename is in `oracle.OwnDataDirNames`. The env var is inherited by
child processes, so another plugin's value genuinely leaks into ours — this
guard is load-bearing, not defensive decoration. Use `filepath.Clean` +
`filepath.Base`, and handle a trailing separator the way Python's
`basename(normpath(env))` does.

Per-key type tolerance is exact: a key whose JSON type is wrong is ignored and
its default stands, while other keys in the same file still apply. `markers.add`
and `markers.remove` must be lists; non-string elements inside them are
dropped; an add/remove list that ends up empty leaves the default.

Port the config half of `tests/test_config.py` (through
`test_remove_unknown_marker_is_noop`), plus the `_config_path` /
`_state_path` location tests that concern config.

## package transcript

The Python original parses each JSONL line into a plain dict and reads it with
`.get()` chains, so a line whose field has an unexpected JSON type is still
usable. Straight `json.Unmarshal` into a rigid struct would *error* on such a
line and drop it — a behavior change in the fail-closed direction.

Resolve this by decoding into structs whose awkward fields are
`json.RawMessage` or `any`, and interpreting them with Python semantics:

- `isSidechain` is a **truthiness** test in Python (`not obj.get("isSidechain")`).
  Python-falsy is: absent, `null`, `false`, `0`, `""`, `[]`, `{}`. Everything
  else — including the string `"false"` — is truthy and skips the entry.
  Implement a `pythonTruthy(json.RawMessage) bool` helper.
- `message.content` is either a string or an array of blocks. Decode
  leniently into a type that records which.
- `input.subagent_type` is `str(...)`-ed in Python, so a non-string value is
  stringified rather than rejected. Keep `input` as `json.RawMessage` and
  extract lazily — this is also faster, since only tool_use blocks in the
  current turn are ever inspected.

```go
func LoadEntries(path string) []Entry        // missing/unreadable file → nil, never an error
func LastAssistantText(entries []Entry) string
func TurnStart(entries []Entry) int          // index of the LAST real user prompt; 0 if none
func TurnTailFlushed(entries []Entry) bool
func OracleConsultedThisTurn(entries []Entry) bool
```

Invalid UTF-8 must not disable the hook: Python opens with
`errors="replace"`. Decode the file bytes with the same substitution
(U+FFFD per invalid byte) before splitting lines. A line that fails to parse
as JSON is skipped; a missing file yields an empty slice.

`TurnStart` finds the last entry that is a *real* user prompt: type `user`,
with text content and no `tool_result` block. Tool results must not reset the
turn boundary.

`TurnTailFlushed` is the flush-race probe. It asks whether the turn's **last**
assistant entry carries text — deliberately not "does this turn have any
text", because nearly every agentic turn opens with a preamble and an any-text
probe is satisfied by that preamble while the real final message is still in
flight. Read the Python docstring before implementing.

`OracleConsultedThisTurn` scans from `TurnStart` for a `tool_use` block named
`Task` or `Agent` whose lowercased `subagent_type` is exactly `oracle` or ends
with `:oracle`. Never a substring test — `my-oracledb-helper` must not count.

Port `tests/test_transcript.py` in full.

## package state

```go
func StatePath(sessionID string, cfg config.Config) string  // see note below
func AlreadyBlocked(sessionID, promptID string, cfg config.Config) bool
func RecordBlock(sessionID, promptID string, cfg config.Config)
```

To keep `state` free of a `config` import, take the two values it actually
needs instead of the whole Config: `StatePath(sessionID, stateDirOverride,
defaultDir string)`. `hook` supplies them. Adjust the signatures above
accordingly and document it.

- Path is `<state dir>/<first 16 hex of sha1(sessionID)>.json`. Session IDs
  are hashed so `a/b` and `ab` cannot collide and no separator escapes the dir.
- `RecordBlock` writes to a temp file in the same directory and `os.Rename`s
  over the target. A crash mid-write must never truncate an existing record —
  a truncated record would let the same prompt be blocked twice.
- Every failure is swallowed. A temp file left behind by a failed write is
  removed.
- `RecordBlock` first prunes state files older than 30 days. The `state_dir`
  knob means the resolved directory can be a user's own folder, so deletion is
  **allowlisted to the exact shapes we create**: `^[0-9a-f]{16}\.json$` and Go's
  `os.CreateTemp` leftovers. Nothing else is ever touched, whatever its age.
  `config.json` lives in the same directory and is never pruned.

Go's `os.CreateTemp(dir, "*.tmp")` produces names like `1234567890.tmp`, not
Python's `tmpXXXX.tmp`. Use an explicit pattern — `os.CreateTemp(dir, "tmp*.tmp")` —
and make the prune allowlist regex match both that shape and Python's, so a
directory shared with a Python-era install stays clean.

Port the state tests from `tests/test_config.py` and `tests/test_stop_entry.py`
(prune safety, atomicity, path derivation, foreign-env rejection).

## package hook

```go
func RunStop(stdinText string) (int, string)
func RunSessionStart(stdinText string) (int, string)
```

Both return `(exitCode, stdout)` and both must be total: recover from panics
and return `(0, "")`. Port the control flow in `run_stop` exactly, in order:

1. `CC_ORACLE_DISABLE` → silent
2. strip a leading UTF-8 BOM (Windows pipes prepend one), parse JSON payload
3. non-object payload → silent
4. `stop_hook_active` **is exactly boolean true** → silent. The string
   `"false"` must not suppress; so must the string `"true"` not suppress.
5. config `stop_hook` not true → silent
6. no `transcript_path` → silent
7. load entries; empty → silent
8. slice to the current turn via `TurnStart`
9. flush-race loop over the backoff schedule (below)
10. `ShouldNudge(LastAssistantText(...))` false → silent
11. oracle already consulted this turn → silent
12. `prompt_id` present and already blocked → silent
13. record the block, emit `{"decision":"block","reason":NUDGE}`

The flush-race loop: for each delay in the schedule, break if
`TurnTailFlushed`; otherwise sleep, re-read the transcript, and if the re-read
returned entries, recompute the turn boundary from the fresh entries. An empty
re-read means a torn or locked file mid-write — keep the entries already held
and spend the next delay rather than abandoning the budget.

Recomputing the boundary is deliberate: if the user started a new turn during
the wait, the slice moves past the old one and the stuck statement goes
unnudged. That is the fail-open direction and must stay that way.

Schedule is `50ms, 100ms, 200ms`. Expose it as a package var and make the
sleep function a package var too, so tests can stub it — that is how the
Python tests drive the race deterministically.

`RunSessionStart` has the **inverse** failure posture: config trouble means the
doctrine IS injected. Only an explicit, well-formed `doctrine: false` silences
it. The env kill switch still applies.

Emitted JSON must be ASCII-safe — it has to survive any console codepage.
Go's `encoding/json` escapes non-ASCII to `\uXXXX` by default, which matches
Python's `ensure_ascii=True`. Do not disable that. Note that Go additionally
escapes `<`, `>`, and `&` unless you turn `SetEscapeHTML(false)` off; the
NUDGE and DOCTRINE strings contain `<oracle-plugin>` tags, so the
SessionStart envelope **will** differ from Python's byte-for-byte unless HTML
escaping is disabled. Disable it, so the differential test can compare bytes.

`NUDGE` and `DOCTRINE` are copied verbatim from the Python source. DOCTRINE
must stay ≤ 8 lines.

`main.go`: mode is `argv[1]`; `session-start` and `stop` are handled, anything
else exits 0 silently. stdin read failures exit 0 silently.

## Known divergences

Every entry below was reproduced by driving both implementations with identical
input, not reasoned about. The differential suite passes, so these are the
shapes it does not generate.

Two bugs found in the Python during the port were **fixed** rather than
mirrored: `main()` read stdin through the locale codec (on Windows a piped
handle is cp1252, so the UTF-8 BOM it explicitly set out to tolerate arrived as
mojibake and the hook did nothing), and `test_malformed_config_ignored` wrote
its fixture to a directory the code never reads, so it never exercised the
branch it was named for.

One Go-side divergence was **fixed** rather than documented: `config.Load` now
rejects a config file that is not valid UTF-8, because Python reads it with a
strict decoder and discards the whole file on the first bad byte. A config saved
as cp1252 is an ordinary Windows mistake, and either implementation may win the
dispatch chain, so the two must agree on whether such a file applies.

### Go is more permissive than Python (over-blocking direction)

This is the direction the project's doctrine cares about — a false positive is
worse than a miss — so these matter most.

**1. Malformed entry shapes abort the whole hook in Python but blank only one
entry in Go.** Python's `.get()` chains raise `AttributeError` on a truthy
non-dict `message` or `input`, and `TypeError` on a numeric `text`. That
exception reaches `run_stop`'s catch-all and silences **the entire hook**. Go's
lenient decoding yields a zero value for that one entry, and the *other* entries
still drive a block. Confirmed triggers, each py=SILENT / go=BLOCK when the
turn's tail text carries a marker:

- `{"type":"user","message":"a plain string"}` anywhere in the turn
- `{"type":"assistant","message":5.5}` (also `true`, `[1]`, `"0"`)
- `{"type":"tool_use","name":"Task","input":"a string not a dict"}`
- `{"type":"text","text":5}` in the tail assistant entry

A biased fuzz diverged on 13 of 500 transcripts, and every residual divergence
traced to this one class. A scan of 1,240 real transcripts / 248,730 entries
found **zero** occurrences of any of these shapes, so it is latent rather than
live. Not fixed, because exact emulation requires modelling which malformed
entry each traversal reaches first — Python raises only where the specific
function actually touches it — and the obvious over-approximation ("any
malformed entry silences the turn") would introduce a fresh divergence in the
opposite direction.

An earlier draft of this document listed these as "not observable", on the
grounds that Python's exception and Go's zero value both end in silence. That
was **wrong**: only Python's ends in silence.

**2. Word boundaries are ASCII-only in Go, Unicode-aware in Python**, so a
non-ASCII letter glued to an idiom's edge creates a boundary for Go but not for
Python. A single-codepoint sweep flips the decision for **132,959 of 1,112,064**
codepoints. Both "caféi am stumped by this." and "I am stumpedé by this." are
py=SILENT / go=BLOCK. Space-separated prose does not produce this; CJK abutting
Latin without a space could.

**3. `strings.ToLower` does simple case mapping; `str.lower()` does full
mapping** and can change length. "İ'm not sure why this fails." is py=SILENT /
go=BLOCK. A full sweep found exactly one marker-relevant codepoint (U+0130); the
other 44 differences are Python/Go Unicode-version skew and touch no marker.

**4. Lone surrogate in `session_id`.** Python's
`str(session_id).encode("utf-8", "surrogateescape")` raises
`UnicodeEncodeError`; `_already_blocked` catches it but `_record_block`'s
`except OSError` does not, so it escapes to the catch-all and silences the hook.
Go hashes the U+FFFD-substituted string and blocks.

**5. NUL in `state_dir`.** Python's `Path(...).mkdir()` raises `ValueError`,
which escapes `_record_block`'s `except OSError` the same way. Go ignores the
`MkdirAll` error and blocks.

**6. `NaN` / `Infinity` in config.** Python's `json` accepts these bare
literals; Go rejects the document and falls back to defaults, so
`{"stop_hook": false, "z": NaN}` is py=SILENT / go=BLOCK.

### Go is stricter than Python (miss direction)

**7. `NaN` / `Infinity` in a transcript line or the stdin payload.** Go skips the
line, or rejects the payload outright: a payload with `"extra": NaN` is py=BLOCK
/ go=SILENT.

**8. A lone `\uD800` escape** becomes U+FFFD in Go and survives in Python.

### Bidirectional

**9. U+001C-U+001F (the C0 separators) are whitespace to Python, not to Go.** An
earlier draft classified this as miss-only and therefore safe. It is both:

- miss direction: "I'm notsure why this fails." is py=BLOCK / go=SILENT
- **over-block direction**: "I'm not sure which one?" — Python's `rstrip()`
  exposes the question mark, the question-turn exemption fires, and it stays
  silent. Go leaves the separator in place, sees no trailing `?`, and blocks.

**10. Temp-dir resolution.** `os.TempDir()` reads `TMP, TEMP, USERPROFILE`;
`tempfile.gettempdir()` reads `TMPDIR, TEMP, TMP`. An earlier draft said this
needed a Windows user who sets `TMPDIR` alone — not so: `TMPDIR` merely
*differing* from `TMP` is enough, and Git Bash, MSYS2 and CI images commonly set
it. Since the dispatch chain can fall through from Go to Python, a user's config
and block-state can change location with the winner. The differential suite sets
all three variables where it exercises that path.

### Not observable end to end

| # | Divergence | Why it cannot change a decision |
|---|---|---|
| 11 | `str()` of a non-string `subagent_type` renders differently | neither form can equal `oracle` or end with `:oracle`, the value's only consumer |
| 12 | Session and prompt ids render through a reimplementation of `str()` | the value only selects a state file; `pythonStr` preserves distinctness, including `1e3` becoming `"1000.0"` so it cannot collide with `1000`. Verified identical across 16 exotic shapes |
| 13 | `os.Remove` succeeds on an empty directory where Python's raises | `pruneStale` skips directories explicitly; Go is deliberately stricter |
| 14 | Python's regex `$` also matches before a trailing newline | Go is safer; POSIX-only and absurd |
| 15 | `marshal` cannot fail in production in Go | the type system removes the failure mode; the injection point exists only for the atomicity test |
| 16 | SUSPECTED, not reproduced: `pruneStale` reads the link's mtime where Python follows the symlink | a 30-day-old symlink named like a state file would be pruned by Go and kept by Python. Bounded by the allowlist; state files are disposable |

### Verified equivalent by exhaustive testing

These were attacked specifically and came back clean:

- **Control flow** in `RunStop` versus `run_stop` — identical, check for check,
  in order, including `LoadTurn`'s emptiness test, the flush-backoff loop with
  its boundary recomputation and empty-re-read retention, and the
  BlockState/RecordBlock/emit sequence.
- **The `no idea how` lookahead rewrite** — exactly equivalent, not
  approximately. The gap between Go's non-overlapping `FindAllStringSubmatchIndex`
  and Python's advance-by-one on lookahead failure was attacked with **381,024
  enumerated clauses**, 600 hedged/genuine concatenations built specifically to
  exploit it, and 120,000 random strings: zero divergence. It holds because the
  only pronoun token in any match sits at position 0, so no genuine match can
  begin inside a skipped hedged one.
- **The hand-rolled sentence splitter** — a 1,155-case punctuation x whitespace
  x tail grid: zero divergence outside item 9.
- **`pyjson` against CPython's `json.dumps`** — byte-identical across **all
  1,112,064 valid codepoints**, 841 escape-boundary pairs, 20,000 random
  strings, and both hook constants.
- **`decodeUTF8Replace`** — byte-identical to CPython's
  `bytes.decode("utf-8", "replace")` across all 256 one-byte and all 65,536
  two-byte inputs, a 15,625-case three-byte grid, a 200,000-case four-byte grid,
  and 100,000 random buffers. The maximal-subpart rule is right.
- **`StripQuoted`**, the **marker lists** (all three copies element-identical and
  in order), the **state layer** (including cross-implementation state-file
  migration in both directions), the **`CLAUDE_PLUGIN_DATA` allowlist** across 19
  basename forms, and **universal newlines**.

### Operational limits found by audit

These came out of an adversarial safety review of the finished port. Each was
reproduced, not theorised.

**Memory: the transcript is read whole, where Python streamed it.**
`readLines` does `os.ReadFile` plus a split, versus Python's `for line in f`.
Measured peak RSS for one `stop`: 4 MB transcript → 12 MB; 500 MB → 595 MB, or
1.6 GB when all three flush retries fire; a 10 MB file of ten million empty
lines → 246 MB, a 24x amplification from the per-line slice headers.

This is the one input-driven path that can produce a nonzero exit, because a Go
OOM is a runtime fatal error that no `recover` can catch — so in the worst case
it violates the prime directive.

Calibrating against reality before acting on it: across all 2,063 transcripts on
the development machine the largest is 3.8 MB, and `LoadTurn`'s backward scan
reads a median of 35 and a 99th-percentile of 662 records. None of the 1,240
real main-session transcripts lacks a user prompt, so the degenerate
parse-everything case does not occur in practice. It is a robustness ceiling
rather than a live problem, and it is left as one deliberately: a bounded tail
read would fix the memory profile but risks placing the turn boundary wrongly
when the boundary falls outside the window, which turns a memory ceiling into a
false-positive block. That trade is not worth taking against a 3.8 MB observed
maximum.

**Concurrency: the once-per-turn guarantee does not survive concurrent hooks.**
`BlockState` (read) and `RecordBlock` (write) are a plain check-then-act pair
with no lock, so N hooks racing on the same `session_id` **and** `prompt_id`
emit N blocks — measured 7 of 7 and 20 of 20. Inherited from the Python, which
has the same shape. It needs the *same turn* to be stop-checked concurrently;
the documented 7-concurrent-hook incident was seven separate sessions, which do
not share a state file. Not fixed, because an atomic claim (`O_CREATE|O_EXCL`)
would change behavior beyond what a port should.

On Windows this is mildly amplified: Go's `os.Open` does not pass
`FILE_SHARE_DELETE`, so reads of the state file fail under contention far more
often than Python's (3,672 vs 80 failures in a 3-second hammer), and a failed
read reads as "not blocked". The rename half is at parity — Python fails
identically. Critically, the atomic-write design held: **zero torn reads and
zero corrupted state files** in either implementation.

**A relative `state_dir` resolves against the process's working directory**,
i.e. the user's project, so `{"state_dir": "."}` litters state files there and
points the 30-day sweep at the repo root. This matches Python exactly.

**`CLAUDE_PLUGIN_DATA` matching is case-sensitive** on filesystems that are not,
so `.../Oracle` is treated as a foreign directory and state silently relocates
to the temp dir. Matches Python exactly.

### Verified safe under direct attack

The prune allowlist was attacked with a seeded victim directory: uppercase name
variants, `.json.bak`, a *directory* named like a state file, an NTFS junction
pointing at a precious folder, and a hard link to a precious file. Only genuine
matches were removed; the directory and junction were skipped, and removing the
hard link left its target intact. Dry runs against `C:\Windows`, `%TEMP%`,
`%USERPROFILE%`, `C:\` and `Documents` matched **zero** files, so even a
catastrophically misconfigured `state_dir` deletes nothing.

One caveat: .NET's `Path.GetTempFileName()` produces `tmpXXXX.tmp`, which the
allowlist matches by design (it is also Python's `mkstemp` shape). A `state_dir`
pointed at a shared temp directory could therefore remove another application's
30-day-old temp files.

Fuzzing both entry points — no args, unknown mode, 4 KB of random bytes, 200 MB
of stdin, closed stdout, `transcript_path` pointing at a directory and at `NUL`,
a config nested five million deep, a record nested two hundred thousand deep —
produced exit 0 every time.

The cross-compiled `darwin-arm64` binary carries a valid ad-hoc code signature
(`LC_CODE_SIGNATURE` present), which Apple Silicon requires to exec. Go's linker
emits it even when cross-compiling from Windows, so the binaries built by
`scripts/build.sh` will run on Apple Silicon.

### Duplication accepted on purpose

The baseline marker list exists in both `internal/config` and `internal/detect`
so the two could be built independently. `internal/hook`'s
`TestConfigAndDetectAgreeOnBaselineMarkers` is the tripwire that keeps them
identical.

## Testing rules

- Table-driven tests, one case per Python assertion. Do not collapse the
  `parametrize` lists — each phrase is a regression someone paid for.
- Tests that touch `CLAUDE_PLUGIN_DATA` must use `t.Setenv` and `t.TempDir`.
  `t.Setenv` forbids `t.Parallel()` in that test; do not add it.
- No new module dependencies. Standard library only, matching the original's
  stated constraint.
- `go vet ./...` must be clean.
