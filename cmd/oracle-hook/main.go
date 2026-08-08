// Command oracle-hook is the compiled replacement for hooks/oracle_hook.py.
//
// Usage: oracle-hook <session-start|stop>, hook payload on stdin, response on
// stdout. It exits 0 in every circumstance — a nonzero exit is treated by the
// harness as a hook failure and turns into a re-run.
package main

import (
	"io"
	"os"

	"github.com/exPardus/cc-oracle/internal/hook"
)

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout))
}

func run(argv []string, stdin io.Reader, stdout io.Writer) (code int) {
	// Nothing may escape: an uncaught panic exits nonzero and surfaces noise
	// (or a re-run) in the user's session.
	defer func() {
		if r := recover(); r != nil {
			code = 0
		}
	}()

	if len(argv) < 2 {
		return 0
	}

	var handler func(string) (int, string)
	switch argv[1] {
	case "session-start":
		handler = hook.RunSessionStart
	case "stop":
		handler = hook.RunStop
	default:
		// Unknown mode: exit silently without touching stdin, matching the
		// Python entry point.
		return 0
	}

	raw, err := io.ReadAll(stdin)
	if err != nil {
		// A broken pipe must not produce a traceback or a nonzero exit.
		return 0
	}

	exit, out := handler(string(raw))
	if out != "" {
		// Python writes with sys.stdout.write: no trailing newline.
		if _, err := io.WriteString(stdout, out); err != nil {
			return 0
		}
	}
	return exit
}
