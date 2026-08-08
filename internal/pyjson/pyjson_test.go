package pyjson

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The goldens are produced by CPython itself (a script recorded them from
// json.dumps), so these tests pin the encoder to the real interpreter rather
// than to a reading of its source.
//
// Expected values below are written as Go escapes of the ESCAPE TEXT, e.g.
// "\"\\u007f\"" for the six characters \u007f in quotes. Writing them as raw
// literals embeds the actual control character instead and silently tests the
// wrong thing.

type stringCase struct {
	In  string `json:"in"`
	Out string `json:"out"`
}

func loadJSONL(t *testing.T, name string, fn func([]byte)) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fn([]byte(line))
	}
}

func TestStringEncodingMatchesCPython(t *testing.T) {
	n := 0
	loadJSONL(t, "strings.jsonl", func(line []byte) {
		var c stringCase
		if err := json.Unmarshal(line, &c); err != nil {
			t.Fatalf("bad golden line: %v", err)
		}
		n++
		t.Run(strings.ToValidUTF8(c.In, "?"), func(t *testing.T) {
			var buf bytes.Buffer
			EncodeString(&buf, c.In)
			if got := buf.String(); got != c.Out {
				t.Errorf("input %q\n got: %s\nwant: %s", c.In, got, c.Out)
			}
		})
	})
	if n == 0 {
		t.Fatal("no golden cases loaded; the goldens are the whole point")
	}
}

func TestDocumentEncodingMatchesCPython(t *testing.T) {
	// The goldens were produced with sort_keys=True, and Marshal sorts the keys
	// of a Go map, so the two orders agree by construction. Decoding into a map
	// discards Python's insertion order entirely, so it cannot be checked here —
	// TestMemberOrderIsPreserved and TestHookShapes cover that using Object.
	n := 0
	loadJSONL(t, "docs.jsonl", func(line []byte) {
		var c struct {
			Doc map[string]any `json:"doc"`
			Out string         `json:"out"`
		}
		if err := json.Unmarshal(line, &c); err != nil {
			t.Fatalf("bad golden line: %v", err)
		}
		n++
		t.Run(c.Out, func(t *testing.T) {
			got, err := Marshal(normalizeNumbers(c.Doc))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != c.Out {
				t.Errorf("\n got: %s\nwant: %s", got, c.Out)
			}
		})
	})
	if n == 0 {
		t.Fatal("no golden documents loaded")
	}
}

// encoding/json decodes every JSON number to float64; the encoder deliberately
// does not accept floats, since the hook never emits one. Re-integerize whole
// numbers so the golden documents round-trip.
func normalizeNumbers(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = normalizeNumbers(item)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = normalizeNumbers(item)
		}
		return out
	case float64:
		if val == float64(int(val)) {
			return int(val)
		}
		return val
	default:
		return v
	}
}

func TestMemberOrderIsPreserved(t *testing.T) {
	// A Python dict preserves insertion order and a Go map does not, so the
	// hook's output shapes must be built from Object, not a map.
	got, err := Marshal(Object{
		{Key: "zebra", Value: "z"},
		{Key: "apple", Value: "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"zebra": "z", "apple": "a"}`
	if string(got) != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestSeparatorsMatchPythonDefaults(t *testing.T) {
	got, err := Marshal(Object{
		{Key: "a", Value: "1"},
		{Key: "b", Value: []any{"x", "y"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a": "1", "b": ["x", "y"]}`
	if string(got) != want {
		t.Errorf("got %s want %s", got, want)
	}
	if bytes.Contains(got, []byte(`","`)) {
		t.Error("compact separators leaked in; Python writes \", \"")
	}
}

func TestHTMLCharactersAreNotEscaped(t *testing.T) {
	// encoding/json rewrites these to \u003c etc. unless explicitly disabled.
	// DOCTRINE is wrapped in <oracle-plugin> tags, so this is load-bearing.
	var buf bytes.Buffer
	EncodeString(&buf, "<oracle-plugin> & </oracle-plugin>")
	want := `"<oracle-plugin> & </oracle-plugin>"`
	if got := buf.String(); got != want {
		t.Errorf("got %s want %s", got, want)
	}

	stdlib, _ := json.Marshal("<oracle-plugin>")
	if string(stdlib) == `"<oracle-plugin>"` {
		t.Log("note: stdlib no longer HTML-escapes by default")
	}
}

func TestDELIsEscaped(t *testing.T) {
	// CPython's ESCAPE_ASCII is ([\\"]|[^\ -~]); U+007F falls outside
	// U+0020..U+007E and so is escaped. A naive "r > 0x7F" test misses it.
	var buf bytes.Buffer
	EncodeString(&buf, "\x7f")
	want := "\"\\u007f\""
	if got := buf.String(); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestNonBMPBecomesSurrogatePair(t *testing.T) {
	var buf bytes.Buffer
	EncodeString(&buf, "\U0001d54f")
	want := "\"\\ud835\\udd4f\""
	if got := buf.String(); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestOutputIsAlwaysASCII(t *testing.T) {
	// The Python suite asserts emitted JSON is ASCII-safe so it survives any
	// console codepage. Preserve that property.
	for _, s := range []string{
		"em \u2014 dash", "\u65e5\u672c\u8a9e", "\U0001f600", "\u00a0", "caf\u00e9",
	} {
		var buf bytes.Buffer
		EncodeString(&buf, s)
		for i, b := range buf.Bytes() {
			if b >= 0x80 {
				t.Errorf("non-ASCII byte 0x%02x at %d for input %q", b, i, s)
				break
			}
		}
	}
}

func TestInvalidUTF8BecomesReplacementEscape(t *testing.T) {
	var buf bytes.Buffer
	EncodeString(&buf, string([]byte{0xff}))
	want := "\"\\ufffd\""
	if got := buf.String(); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestUnsupportedValueReturnsError(t *testing.T) {
	if _, err := Marshal(3.14); err == nil {
		t.Error("expected an error for float64")
	}
	if _, err := Marshal(struct{ A int }{1}); err == nil {
		t.Error("expected an error for a struct")
	}
}

func TestHookShapes(t *testing.T) {
	// The document the hook emits on a block, in Python's key order, compared
	// against a golden recorded from the Python implementation itself.
	golden, err := os.ReadFile(filepath.Join("..", "hook", "testdata", "python_stop_block.json"))
	if err != nil {
		t.Skipf("stop golden not available: %v", err)
	}
	var doc struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(golden, &doc); err != nil {
		t.Fatalf("golden is not valid JSON: %v", err)
	}
	got, err := Marshal(Object{
		{Key: "decision", Value: doc.Decision},
		{Key: "reason", Value: doc.Reason},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, golden) {
		t.Errorf("stop block output differs from Python\n got: %s\nwant: %s", got, golden)
	}
}
