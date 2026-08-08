package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Ports tests/test_cli_smoke.py at the entry-point level. The end-to-end
// process-level smoke lives in tests/test_differential.py, which drives the
// real binary alongside the real Python hook.

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("pipe gone") }

func isolate(t *testing.T) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "oracle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_PLUGIN_DATA", dir)
	t.Setenv("CC_ORACLE_DISABLE", "")
}

func TestUnknownModeExitsZeroSilently(t *testing.T) { // test_cli_unknown_mode_exits_zero
	isolate(t)
	var out strings.Builder
	if code := run([]string{"oracle-hook", "bogus-mode"}, strings.NewReader("{}"), &out); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

func TestNoModeArgumentExitsZero(t *testing.T) {
	isolate(t)
	var out strings.Builder
	if code := run([]string{"oracle-hook"}, strings.NewReader(""), &out); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

func TestBrokenStdinExitsZero(t *testing.T) { // test_main_returns_zero_when_stdin_read_raises
	isolate(t)
	var out strings.Builder
	if code := run([]string{"oracle-hook", "stop"}, failingReader{}, &out); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

func TestUnknownModeNeverTouchesStdin(t *testing.T) {
	// The Python entry point returns before reading stdin for an unknown mode,
	// so a broken pipe must not matter there either.
	isolate(t)
	var out strings.Builder
	if code := run([]string{"oracle-hook", "nope"}, failingReader{}, &out); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
}

func TestSessionStartEmitsEnvelope(t *testing.T) { // test_cli_session_start_emits_envelope
	isolate(t)
	var out strings.Builder
	if code := run([]string{"oracle-hook", "session-start"}, strings.NewReader(""), &out); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), `"hookEventName": "SessionStart"`) {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestGarbageStdinExitsZeroSilently(t *testing.T) { // test_cli_stop_garbage_stdin_exits_zero_silent
	isolate(t)
	var out strings.Builder
	code := run([]string{"oracle-hook", "stop"},
		strings.NewReader("\xff not json at all"), &out)
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

func TestNoTrailingNewlineOnOutput(t *testing.T) {
	// Python writes with sys.stdout.write, which adds nothing. A stray newline
	// would break byte-level parity with the Python implementation.
	isolate(t)
	var out strings.Builder
	run([]string{"oracle-hook", "session-start"}, strings.NewReader(""), &out)
	if s := out.String(); strings.HasSuffix(s, "\n") {
		t.Error("output must not end with a newline")
	}
}
