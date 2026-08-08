// Package transcript reads Claude Code JSONL transcripts and locates the
// current turn inside them.
//
// This is a port of the load_entries / last_assistant_text / _turn_start /
// _turn_tail_flushed / oracle_consulted_this_turn group in
// hooks/oracle_hook.py. The Python original parses each line into a plain
// dict and reads it with .get() chains, so a line whose field carries an
// unexpected JSON type is still usable. Decoding straight into a rigid struct
// would make encoding/json *error* on such a line and drop the entry — a
// behavior change in the fail-closed direction, which this hook must never
// take. Every field that the harness could plausibly write with a surprising
// type is therefore decoded through json.RawMessage and interpreted with
// Python semantics.
//
// The package has no dependencies outside the standard library and imports no
// other package in this module.
package transcript

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Entry is one non-sidechain record from a transcript file, flattened to the
// two things the hook reads: the record's "type" and its "message.content".
// Nothing else in the JSONL is consulted by any function here, so nothing else
// is retained — transcripts reach several megabytes and are read on every Stop
// hook invocation.
//
// A field that was absent, null, or of an unexpected JSON type degrades to its
// zero value rather than failing the decode.
type Entry struct {
	// Type is the record's top-level "type" ("user", "assistant", ...).
	// Empty when absent or not a JSON string.
	Type string

	// Content is message.content, normalized. Empty when "message" is absent,
	// is not a JSON object, or carries no "content" key.
	Content Content
}

// Content is a message's content field, which the harness writes either as a
// plain string or as an array of blocks. IsString records which form arrived,
// because the Python code branches on isinstance(content, str) and the two
// branches are not interchangeable.
type Content struct {
	// IsString is true when content decoded as a JSON string.
	IsString bool

	// Str is the content when IsString; otherwise empty.
	Str string

	// Blocks holds the object elements of an array content, mirroring Python's
	// [b for b in content if isinstance(b, dict)] — non-object elements are
	// dropped rather than failing the decode. Nil unless content was an array.
	Blocks []Block
}

// Block is one content block. Type, Text and Name are empty when the
// corresponding key is absent or is not a JSON string, which is what makes a
// block with, say, "type": 5 compare unequal to "text" instead of killing the
// decode.
type Block struct {
	Type string
	Text string
	Name string

	// Input is the tool_use "input" object, kept raw and interpreted lazily by
	// subagentType. Only tool_use blocks inside the current turn are ever
	// inspected, so the whole transcript's tool inputs are never decoded.
	Input json.RawMessage
}

// dispatchTools are the tool names that dispatch a subagent: "Task" in older
// harnesses, "Agent" in newer ones.
var dispatchTools = [...]string{"Task", "Agent"}

// LoadEntries reads a JSONL transcript and returns its non-sidechain records.
//
// A missing or unreadable file yields nil, never an error — the hook must not
// wedge a session over a path that moved. Lines that are blank, that fail to
// parse as JSON, or that are not JSON objects are skipped, and so are sidechain
// (subagent) entries: they share the transcript file, but their text and
// tool_use are not the main thread's and never count.
//
// Invalid UTF-8 does not disable the hook. Python opens the file with
// errors="replace"; the bytes are decoded here with the same substitution.
//
// This is the reference implementation, and it parses the whole file. Callers
// that only want the current turn — which is every caller in the hook — should
// use LoadTurn instead: on a real multi-megabyte transcript it parses a few
// dozen lines instead of a few thousand.
func LoadEntries(path string) []Entry {
	lines, ok := readLines(path)
	if !ok {
		return nil
	}
	var entries []Entry
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if e, ok := parseEntry(line); ok {
			entries = append(entries, e)
		}
	}
	return entries
}

// LoadTurn returns exactly what LoadEntries(path)[TurnStart(...):] returns,
// without parsing the entries before the turn boundary.
//
// The hook throws away everything before TurnStart on every invocation, and
// TurnStart is the LAST real user prompt, which sits near the end of the file.
// A 3.7MB transcript is ~1500 records whose final turn is a few dozen; parsing
// the other ~1450 to discard them cost more than the whole Python process
// saved. So the walk runs backward from the end and stops at the first real
// user prompt it parses — which, scanning backward, is the last one in the
// file. That entry is the boundary and is included.
//
// A transcript with no real user prompt anywhere parses in full and returns
// every entry, which is what TurnStart returning 0 means. That degenerate case
// is unavoidable and correct.
func LoadTurn(path string) []Entry {
	lines, ok := readLines(path)
	if !ok {
		return nil
	}
	// Collected newest-first; reversed below. Every line parsed here is a line
	// that ends up in the result, so there is no wasted work to filter out.
	var rev []Entry
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		e, ok := parseEntry(line)
		if !ok {
			continue
		}
		rev = append(rev, e)
		if isRealUserPrompt(e) {
			break
		}
	}
	if len(rev) == 0 {
		return nil
	}
	entries := make([]Entry, len(rev))
	for i, e := range rev {
		entries[len(rev)-1-i] = e
	}
	return entries
}

// readLines reads the transcript and splits it into raw lines. The bool is
// false only when the file could not be read at all.
//
// Lines are handed back as []byte rather than string so that parseEntry can
// pass them to encoding/json without the copy that []byte(string) forces on
// every line — on a multi-megabyte transcript that copy is a second full pass
// over the file's bytes.
func readLines(path string) ([][]byte, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return splitLines(decodeUTF8Replace(data)), true
}

// LastAssistantText returns the text of the final assistant message that has
// text.
//
// Only assistant entries are scanned — user messages, tool results and
// hook-injected context must never be matched (spec: assistant-text-only).
// Multiple text blocks are joined with "\n" and the result is trimmed.
func LastAssistantText(entries []Entry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Type != "assistant" {
			continue
		}
		var joined string
		if e.Content.IsString {
			joined = strings.TrimSpace(e.Content.Str)
		} else {
			var texts []string
			for _, b := range e.Content.Blocks {
				// Python filters empty strings out before joining:
				// "\n".join(t for t in texts if t).
				if b.Type == "text" && b.Text != "" {
					texts = append(texts, b.Text)
				}
			}
			joined = strings.TrimSpace(strings.Join(texts, "\n"))
		}
		if joined != "" {
			return joined
		}
	}
	return ""
}

// TurnTailFlushed reports whether the turn's LAST assistant entry carries text.
//
// This is the flush-race probe, and it deliberately does NOT ask "does this
// turn have any assistant text". Almost every agentic turn opens with a
// preamble ("Let me look at this.") before its tool calls, so an any-text probe
// is satisfied by that preamble and the hook proceeds on stale text while the
// turn's real final message is still in flight.
//
// An unflushed tail has a specific shape: a trailing assistant entry holding
// only thinking or tool_use blocks. Scanning back to the last assistant entry
// and asking whether IT carries text catches that, and still short-circuits on
// the common case where the final text is already on disk.
func TurnTailFlushed(entries []Entry) bool {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Type != "assistant" {
			continue
		}
		if e.Content.IsString {
			return strings.TrimSpace(e.Content.Str) != ""
		}
		for _, b := range e.Content.Blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				return true
			}
		}
		// Decided by the last assistant entry alone: do not keep scanning back.
		return false
	}
	return false
}

// TurnStart returns the index of the last real user prompt, or 0 when the
// transcript holds none.
func TurnStart(entries []Entry) int {
	start := 0
	for i, e := range entries {
		if isRealUserPrompt(e) {
			start = i
		}
	}
	return start
}

// OracleConsultedThisTurn reports whether the current turn already dispatched
// the oracle subagent.
func OracleConsultedThisTurn(entries []Entry) bool {
	for _, e := range entries[TurnStart(entries):] {
		if e.Type != "assistant" {
			continue
		}
		for _, b := range e.Content.Blocks {
			if b.Type != "tool_use" || !isDispatchTool(b.Name) {
				continue
			}
			if isOracleSubagent(strings.ToLower(subagentType(b.Input))) {
				return true
			}
		}
	}
	return false
}

// isRealUserPrompt reports whether the entry is a genuine user turn opener: a
// user entry with text and no tool_result. Tool results arrive as user entries
// too, and a turn is full of them — counting one would drag the turn boundary
// forward past the statement being judged.
func isRealUserPrompt(e Entry) bool {
	if e.Type != "user" {
		return false
	}
	if e.Content.IsString {
		return strings.TrimSpace(e.Content.Str) != ""
	}
	hasText, hasToolResult := false, false
	for _, b := range e.Content.Blocks {
		switch b.Type {
		case "text":
			hasText = true
		case "tool_result":
			hasToolResult = true
		}
	}
	return hasText && !hasToolResult
}

// isOracleSubagent applies the exact-name rule: "oracle" or plugin-scoped
// "<plugin>:oracle". Never a bare substring test — "my-oracledb-helper" must
// not count.
func isOracleSubagent(name string) bool {
	return name == "oracle" || strings.HasSuffix(name, ":oracle")
}

func isDispatchTool(name string) bool {
	for _, t := range dispatchTools {
		if name == t {
			return true
		}
	}
	return false
}

// parseEntry decodes one JSONL line. The bool is false when the line is not a
// JSON object or is a sidechain entry, both of which Python drops.
func parseEntry(line []byte) (Entry, bool) {
	var obj map[string]json.RawMessage
	// Anything that is not a JSON object — an array, a bare scalar, malformed
	// text — fails here, matching Python's isinstance(obj, dict) guard plus its
	// JSONDecodeError skip.
	if err := json.Unmarshal(line, &obj); err != nil || obj == nil {
		return Entry{}, false
	}
	// Python: `not obj.get("isSidechain")` — a truthiness test, not a bool
	// decode. The string "false" is truthy and skips the entry.
	if pythonTruthy(obj["isSidechain"]) {
		return Entry{}, false
	}
	e := Entry{Type: jsonString(obj["type"])}
	// Python: (entry.get("message") or {}).get("content"). A message that is
	// absent, null, or not an object leaves content unset.
	if msgRaw, ok := obj["message"]; ok {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(msgRaw, &msg); err == nil && msg != nil {
			e.Content = parseContent(msg["content"])
		}
	}
	return e, true
}

// parseContent normalizes message.content. Anything that is neither a JSON
// string nor a JSON array (null, a number, an object) yields the zero Content,
// which is how Python's two isinstance checks both failing behaves.
func parseContent(raw json.RawMessage) Content {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return Content{}
	}
	switch b[0] {
	case '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return Content{}
		}
		return Content{IsString: true, Str: s}
	case '[':
		var elems []json.RawMessage
		if err := json.Unmarshal(b, &elems); err != nil {
			return Content{}
		}
		c := Content{}
		for _, el := range elems {
			var m map[string]json.RawMessage
			// Non-object elements are dropped, mirroring
			// [b for b in content if isinstance(b, dict)]. A null element
			// decodes without error into a nil map, so check for that too.
			if err := json.Unmarshal(el, &m); err != nil || m == nil {
				continue
			}
			c.Blocks = append(c.Blocks, Block{
				Type:  jsonString(m["type"]),
				Text:  jsonString(m["text"]),
				Name:  jsonString(m["name"]),
				Input: m["input"],
			})
		}
		return c
	default:
		return Content{}
	}
}

// subagentType extracts input.subagent_type.
//
// Python does str((b.get("input") or {}).get("subagent_type", "")), so a
// non-string value is stringified rather than rejected. The stringification
// here is the value's raw JSON text, which differs from Python's repr for
// non-strings (JSON null vs Python "None", and so on) but is equally incapable
// of matching the oracle name rule, which is the only thing the result feeds.
func subagentType(input json.RawMessage) string {
	b := bytes.TrimSpace(input)
	// An input that is absent, null, or not an object is Python's `or {}` case
	// (or, for a truthy non-dict, a Python AttributeError — Go degrades to the
	// empty string instead, which is the fail-open direction).
	if len(b) == 0 || b[0] != '{' {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return ""
	}
	v := bytes.TrimSpace(m["subagent_type"])
	if len(v) == 0 {
		return ""
	}
	if v[0] == '"' {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return ""
		}
		return s
	}
	return string(v)
}

// jsonString returns raw as a Go string when raw is a JSON string, and ""
// otherwise. A key holding an unexpected type therefore compares unequal to
// every name we test for, instead of aborting the decode.
func jsonString(raw json.RawMessage) string {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 || b[0] != '"' {
		return ""
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return ""
	}
	return s
}

// pythonTruthy applies Python's truth test to a raw JSON value, for fields the
// original reads with `not obj.get(key)`.
//
// Falsy: absent, null, false, a zero number, "", [], {}. Everything else is
// truthy — including the string "false" and the string "0", which Python
// treats as truthy non-empty strings.
func pythonTruthy(raw json.RawMessage) bool {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return false // key absent
	}
	switch b[0] {
	case 'n': // null
		return false
	case 't': // true
		return true
	case 'f': // false
		return false
	case '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return true
		}
		return s != ""
	case '[':
		var a []json.RawMessage
		if err := json.Unmarshal(b, &a); err != nil {
			return true
		}
		return len(a) > 0
	case '{':
		var o map[string]json.RawMessage
		if err := json.Unmarshal(b, &o); err != nil {
			return true
		}
		return len(o) > 0
	default: // number
		f, err := strconv.ParseFloat(string(b), 64)
		if err != nil {
			// Out-of-range still yields a usable value (±Inf, or 0 for a
			// denormal underflow), and that is what Python's float would be.
			// Anything else cannot occur: the JSON was already validated.
			if ne, ok := err.(*strconv.NumError); !ok || ne.Err != strconv.ErrRange {
				return true
			}
		}
		return f != 0
	}
}

// splitLines splits on Python's universal newlines. Python opens the
// transcript in text mode with newline=None, which translates "\r\n" and a
// lone "\r" to "\n" before iteration; splitting on "\n" alone would merge
// records written with classic-Mac line endings into one unparseable line.
//
// The returned lines alias b.
func splitLines(b []byte) [][]byte {
	// The scan is worth it: transcripts are written with "\n", so the common
	// case skips two full-buffer rewrites.
	if bytes.IndexByte(b, '\r') >= 0 {
		b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
		b = bytes.ReplaceAll(b, []byte("\r"), []byte("\n"))
	}
	return bytes.Split(b, []byte("\n"))
}

// decodeUTF8Replace decodes bytes as UTF-8 the way Python's
// errors="replace" does: each invalid sequence is replaced by a single U+FFFD,
// where "sequence" is the maximal subpart CPython's decoder consumes. A lone
// invalid byte costs exactly one U+FFFD, and a truncated but so-far-valid
// multi-byte prefix costs one U+FFFD for the whole prefix.
//
// Neither string(b) (which defers the substitution to iteration time and never
// changes the bytes) nor strings.ToValidUTF8 (which collapses an entire invalid
// run into a single replacement) reproduces this.
//
// Valid input — the overwhelmingly common case — is returned as-is rather than
// copied, so the fast path costs one validating pass and no allocation.
func decodeUTF8Replace(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	var sb bytes.Buffer
	sb.Grow(len(b))
	n := len(b)
	i := 0
	for i < n {
		c := b[i]
		switch {
		case c < 0x80: // ASCII
			sb.WriteByte(c)
			i++

		case c < 0xC2: // continuation byte or overlong 2-byte lead: invalid start
			sb.WriteRune(utf8.RuneError)
			i++

		case c < 0xE0: // 2-byte sequence
			if i+1 >= n {
				sb.WriteRune(utf8.RuneError) // unexpected end of data
				i = n
			} else if !isCont(b[i+1]) {
				sb.WriteRune(utf8.RuneError)
				i++
			} else {
				sb.Write(b[i : i+2])
				i += 2
			}

		case c < 0xF0: // 3-byte sequence
			if i+1 >= n {
				sb.WriteRune(utf8.RuneError)
				i = n
			} else if c2 := b[i+1]; !isCont(c2) ||
				(c == 0xE0 && c2 < 0xA0) || // overlong
				(c == 0xED && c2 >= 0xA0) { // surrogate
				sb.WriteRune(utf8.RuneError)
				i++
			} else if i+2 >= n {
				sb.WriteRune(utf8.RuneError)
				i = n
			} else if !isCont(b[i+2]) {
				sb.WriteRune(utf8.RuneError)
				i += 2
			} else {
				sb.Write(b[i : i+3])
				i += 3
			}

		case c < 0xF5: // 4-byte sequence
			if i+1 >= n {
				sb.WriteRune(utf8.RuneError)
				i = n
			} else if c2 := b[i+1]; !isCont(c2) ||
				(c == 0xF0 && c2 < 0x90) || // overlong
				(c == 0xF4 && c2 >= 0x90) { // beyond U+10FFFF
				sb.WriteRune(utf8.RuneError)
				i++
			} else if i+2 >= n {
				sb.WriteRune(utf8.RuneError)
				i = n
			} else if !isCont(b[i+2]) {
				sb.WriteRune(utf8.RuneError)
				i += 2
			} else if i+3 >= n {
				sb.WriteRune(utf8.RuneError)
				i = n
			} else if !isCont(b[i+3]) {
				sb.WriteRune(utf8.RuneError)
				i += 3
			} else {
				sb.Write(b[i : i+4])
				i += 4
			}

		default: // 0xF5..0xFF: invalid start byte
			sb.WriteRune(utf8.RuneError)
			i++
		}
	}
	return sb.Bytes()
}

func isCont(c byte) bool { return c&0xC0 == 0x80 }
