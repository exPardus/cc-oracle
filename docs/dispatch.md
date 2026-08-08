# How the hook picks a binary

`hooks/hooks.json` runs a `||` chain over every committed binary and falls back
to the original Python implementation. It looks blunt; each part is load-bearing.

## Why binaries are committed

Claude Code installs a plugin by cloning its repository. There is no build step
and no postinstall hook, so anything the hook needs at run time has to already
be in the repo. That rules out `go install` and rules out downloading a release
artifact on first use. Six binaries, ~2.2 MB each, are committed under
`hooks/bin/`.

## Why a `||` chain rather than OS detection

The `command` string is handed to a shell, and that shell is `cmd.exe` on
Windows and a POSIX shell everywhere else. The two share almost no syntax:
`%VAR%` vs `$VAR`, `2>nul` vs `2>/dev/null`, no common `if`. `||` is one of the
few operators that means the same thing in both, which is why the previous
Python-only version already used it for `python || python3`.

`${CLAUDE_PLUGIN_ROOT}` is substituted by the harness before the shell ever
sees it, so it works under both.

## Why trying the wrong platform's binary is safe

- **On Windows**, `cmd.exe` resolves an extension-less path through `PATHEXT`.
  It looks for `oracle-hook-darwin-arm64.exe`, `.bat`, `.cmd` and so on, finds
  none, and reports "not recognized". The Mach-O and ELF files are never
  executed at all — the attempt costs a few file lookups.
- **On macOS and Linux**, exec of a foreign binary fails with `ENOEXEC` and the
  shell exits 126 with "cannot execute binary file". Shells do *not* fall back
  to interpreting it as a script: every modern shell refuses a file containing
  NUL bytes. Nothing inside a binary is ever run as a command. Verified against
  bash with both a Mach-O and an ELF binary.
- **stdin** is consumed only by a process that actually starts. A failed exec
  reads nothing, so the winning binary still receives the full hook payload.
  Verified.
- **stdout stays clean.** Failed attempts write to stderr only, so the JSON
  channel the harness parses is never polluted. Verified.

The cost is one line of stderr per failed attempt. That is not new: on any
machine without a `python` on `PATH` — which is most macOS and Linux installs —
the previous version already printed `python: command not found` before falling
through to `python3`.

## Why the Python fallback stays

If no binary can exec — an unforeseen platform, a corrupted checkout, a
filesystem mounted `noexec` — the original `hooks/oracle_hook.py` still runs and
the safety net still works. It costs nothing to keep and removes the entire
class of "the plugin silently stopped working".

It also gives crash resilience for free. A binary that exits nonzero for any
reason falls through to the next entry and ultimately to Python, which handles
the turn correctly.

## Why the chain ends in `exit 0`

A hook that exits nonzero is treated by the harness as a failure and re-run.
If every entry somehow failed, the trailing `exit 0` makes the chain report
success and stay silent — the same fail-open posture the hook itself takes
internally. `exit 0` is valid in both `cmd.exe` and POSIX shells.

## What the substituted path is trusted to contain

`${CLAUDE_PLUGIN_ROOT}` is substituted textually before the shell parses the
line, so the resulting characters are shell syntax. Verified behavior under a
POSIX shell when the plugin root contains metacharacters:

- a backtick in the root causes command substitution — the enclosed text is
  **executed** on every turn end
- a `$` causes variable expansion, so every entry misses and the plugin goes
  silently dead
- an unbalanced double quote collapses the quoting and the plugin goes silently
  dead

This is inherited, not introduced: the previous Python-only chain interpolated
the same value into the same quoting and behaves identically. In practice the
plugin root is a path the harness chooses for its own plugin cache, not
user-supplied text. It is recorded here because `docs/dispatch.md` previously
documented the `ENOEXEC` and `PATHEXT` behavior thoroughly while saying nothing
about the substituted value itself.

Under `cmd.exe` the chain was verified against 13 hostile roots — space, `&`,
`%`, `!`, `^`, `(`, `)`, `'`, `;`, `,`, `=`, Cyrillic and Japanese characters —
and all 13 exited 0 with clean, valid JSON on stdout.

One accidental property worth not relying on: the plugin root appears an even
number of times in the chain, so an odd number of quote characters in the root
still yields matching quotes overall and the shell exits 0. A chain with an odd
number of entries does *not* have that property. Do not treat exit-0-on-broken-
quoting as guaranteed.

## Ordering

Order affects only how many cheap misses precede the hit, never correctness.
The current order is by expected platform share:

1. `darwin-arm64`
2. `linux-amd64`
3. `windows-amd64.exe`
4. `darwin-amd64`
5. `linux-arm64`
6. `windows-arm64.exe`

## Rebuilding

```sh
sh scripts/build.sh
```

Cross-compiles all six targets from any one machine — no C toolchain, no
per-OS build agent. That property is the main reason this port is in Go.
The script also stages the executable bit for the Unix binaries, which git
tracks and which a Unix user needs in their checkout.
