#!/usr/bin/env sh
# Cross-compile the hook for every platform the plugin ships to.
#
# All six targets build from any one machine with no C toolchain and no
# per-OS build agent — that property is why this port is in Go. Run it from
# anywhere with a Go toolchain on PATH:
#
#     sh scripts/build.sh
#
# Output lands in hooks/bin/, which is committed: Claude Code installs plugins
# by git clone and offers no build step, so the binaries have to be in the
# repo. See docs/specs/2026-08-08-go-port-contract.md.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OUT="$ROOT/hooks/bin"
mkdir -p "$OUT"

TARGETS="windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64"

# -s -w drops the symbol table and DWARF; -trimpath keeps absolute build paths
# out of the binary so the committed artifacts are reproducible across machines.
LDFLAGS="-s -w"

total=0
for target in $TARGETS; do
    goos=${target%/*}
    goarch=${target#*/}
    ext=""
    [ "$goos" = "windows" ] && ext=".exe"
    bin="$OUT/oracle-hook-$goos-$goarch$ext"

    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        go build -trimpath -ldflags="$LDFLAGS" -o "$bin" "$ROOT/cmd/oracle-hook"

    size=$(wc -c < "$bin" | tr -d ' ')
    total=$((total + size))
    printf '%-32s %6s KB\n' "$goos/$goarch" "$((size / 1024))"
done

printf '%-32s %6s KB\n' "TOTAL" "$((total / 1024))"

# Stage EVERY binary. These are the shipped artifact — a target that is built
# but never added leaves that platform with no binary at all, and its users fall
# silently through the dispatch chain to the Python fallback (or, with no Python
# on PATH, to nothing at all, with no error anywhere).
#
# Git also tracks the executable bit, and a Unix user who clones without it gets
# "Permission denied" instead of a working hook. Windows checkouts ignore the
# bit, so setting it here is safe from any build host.
if command -v git >/dev/null 2>&1 && git -C "$ROOT" rev-parse --git-dir >/dev/null 2>&1; then
    git -C "$ROOT" add -- hooks/bin
    for target in $TARGETS; do
        goos=${target%/*}
        goarch=${target#*/}
        [ "$goos" = "windows" ] && continue
        git -C "$ROOT" update-index --chmod=+x "hooks/bin/oracle-hook-$goos-$goarch"
    done

    # Verify rather than assume: count what git actually has staged.
    staged=$(git -C "$ROOT" ls-files -- hooks/bin | wc -l | tr -d ' ')
    expected=$(printf '%s\n' $TARGETS | wc -l | tr -d ' ')
    if [ "$staged" -ne "$expected" ]; then
        echo "ERROR: $staged of $expected binaries are tracked by git." >&2
        echo "Shipping an untracked target leaves that platform with no hook." >&2
        git -C "$ROOT" status --short -- hooks/bin >&2
        exit 1
    fi
    echo "all $staged binaries staged (exec bit set on the unix targets)"
fi
