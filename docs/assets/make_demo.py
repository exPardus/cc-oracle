#!/usr/bin/env python3
"""Render docs/assets/demo.gif: a fake-terminal walkthrough of what cc-oracle does.

Re-run with:  python docs/assets/make_demo.py
Requires Pillow (>= 9.1 for Image.quantize(dither=...)). fontTools is optional;
it is only used to detect which glyphs the primary font lacks.

The animation shows a Claude Code session stalling on a failing test, the plugin's
Stop hook catching a statement of uncertainty, the `oracle` agent being briefed,
its answer, and the fix landing.
"""
from __future__ import annotations

import os
import sys

from PIL import Image, ImageDraw, ImageFont

# --------------------------------------------------------------------------- config
HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "demo.gif")

W, H = 960, 560
PAD = 24
FONT_SIZE = 18
LINE_SPACING = 8
FONT_PATH = os.environ.get("DEMO_FONT", "C:/Windows/Fonts/CascadiaMono.ttf")
FALLBACK_FONT_PATH = os.environ.get("DEMO_FALLBACK_FONT", "C:/Windows/Fonts/seguisym.ttf")
PALETTE_COLORS = 128
SIZE_LIMIT = 1_500_000  # bytes

EVENT_MS = 900
HOLD_MS = 2600
FINAL_MS = 4000

BG = (0x0D, 0x11, 0x17)
BORDER = (0x30, 0x36, 0x3D)
TEXT = (0xC9, 0xD1, 0xD9)
DIM = (0x8B, 0x94, 0x9E)
GREEN = (0x3F, 0xB9, 0x50)
RED = (0xF8, 0x51, 0x49)
YELLOW = (0xD2, 0x99, 0x22)
BLUE = (0x58, 0xA6, 0xFF)
PURPLE = (0xBC, 0x8C, 0xFF)
FG_COLORS = [TEXT, DIM, GREEN, RED, YELLOW, BLUE, PURPLE, BORDER]

# --------------------------------------------------------------------------- fonts
font = ImageFont.truetype(FONT_PATH, FONT_SIZE)
fallback = ImageFont.truetype(FALLBACK_FONT_PATH, FONT_SIZE - 3)
ASCENT, DESCENT = font.getmetrics()
CELL = font.getlength("M")  # monospace advance
LINE_H = FONT_SIZE + LINE_SPACING
COLS = int((W - 2 * PAD) // CELL)
ROWS = (H - 2 * PAD) // LINE_H


def _missing_glyphs(path: str) -> set[str]:
    """Characters used in the demo that the primary font cannot draw."""
    candidates = "\u276f\u23fa\u2717\u2713\u2014\u2026"  # ❯ ⏺ ✗ ✓ — …
    try:
        from fontTools.ttLib import TTFont  # type: ignore

        cmap = TTFont(path).getBestCmap()
        return {c for c in candidates if ord(c) not in cmap}
    except Exception:
        return {"\u23fa", "\u2717"}  # known gaps in Cascadia Mono


FALLBACK_CHARS = _missing_glyphs(FONT_PATH)

# vertical centre of a capital in the primary font, relative to the baseline
_l, _t, _r, _b = font.getbbox("M", anchor="ls")
CAP_MID = (_t + _b) / 2

# --------------------------------------------------------------------------- wrapping
Span = tuple[str, tuple[int, int, int]]
Line = list[Span]


def wrap_line(spans: Line, width: int = COLS) -> list[Line]:
    """Word-wrap a rich line at `width` columns.

    Continuations are indented two spaces past the line's own leading indent.
    """
    chars = [(c, col) for text, col in spans for c in text]
    lead = 0
    while lead < len(chars) and chars[lead][0] == " ":
        lead += 1
    cont_indent = lead + 2

    tokens: list[list] = []
    buf: list = []
    for c, col in chars:
        if c == " ":
            if buf:
                tokens.append(buf)
                buf = []
            tokens.append([(c, col)])
        else:
            buf.append((c, col))
    if buf:
        tokens.append(buf)

    rows: list[list] = []
    cur: list = []
    limit = width
    for tok in tokens:
        is_space = tok[0][0] == " "
        if cur and len(cur) + len(tok) > limit:
            while cur and cur[-1][0] == " ":
                cur.pop()
            rows.append(cur)
            cur = []
            limit = width - cont_indent
            if is_space:
                continue
        while len(tok) > limit - len(cur):  # a single word longer than the row
            take = limit - len(cur)
            rows.append(cur + tok[:take])
            tok = tok[take:]
            cur = []
            limit = width - cont_indent
        cur.extend(tok)
    rows.append(cur)

    out: list[Line] = []
    for i, row in enumerate(rows):
        if i:
            row = [(" ", DIM)] * cont_indent + row
        # merge runs of the same colour back into spans
        merged: Line = []
        for c, col in row:
            if merged and merged[-1][1] == col:
                merged[-1] = (merged[-1][0] + c, col)
            else:
                merged.append((c, col))
        out.append(merged)
    return out


# --------------------------------------------------------------------------- drawing
def draw_frame(lines: list[Line]) -> Image.Image:
    img = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(img)
    d.rectangle((0, 0, W - 1, H - 1), outline=BORDER, width=1)

    rows: list[Line] = []
    for ln in lines:
        rows.extend(wrap_line(ln) if ln else [[]])
    rows = rows[-ROWS:]  # scroll: newest rows stay visible

    for r, row in enumerate(rows):
        base_y = PAD + r * LINE_H + ASCENT
        x = float(PAD)
        for text, col in row:
            run = ""
            for c in text:
                if c in FALLBACK_CHARS:
                    if run:
                        d.text((x, base_y), run, font=font, fill=col, anchor="ls")
                        x += len(run) * CELL
                        run = ""
                    l, t, rr, b = fallback.getbbox(c, anchor="ls")
                    gx = x + (CELL - (rr - l)) / 2 - l
                    gy = base_y + CAP_MID - (t + b) / 2
                    d.text((gx, gy), c, font=fallback, fill=col, anchor="ls")
                    x += CELL
                else:
                    run += c
            if run:
                d.text((x, base_y), run, font=font, fill=col, anchor="ls")
                x += len(run) * CELL
    return img


def build_palette() -> Image.Image:
    """Fixed palette: bg + blends of every foreground colour over bg (anti-aliasing)."""
    steps = (PALETTE_COLORS - 1) // len(FG_COLORS)
    colors = [BG]
    for fg in FG_COLORS:
        for i in range(1, steps + 1):
            a = i / steps
            colors.append(tuple(round(BG[k] + (fg[k] - BG[k]) * a) for k in range(3)))
    colors = colors[:PALETTE_COLORS]
    pal = Image.new("P", (1, 1))
    pal.putpalette([v for c in colors for v in c])
    return pal


# --------------------------------------------------------------------------- script
def L(*spans: Span) -> Line:
    return list(spans)


BASH = "\u23fa Bash(pytest tests/test_billing.py -q)"
ASSERT = "AssertionError: assert Decimal('12.90') == Decimal('12.91')"
HOOK_MSG = (
    "You stated uncertainty this turn without consulting the oracle: "
    "\"I'm not sure why the test still fails\". Dispatch the `oracle` agent now with a "
    "full brief \u2014 Goal, Problem (errors verbatim), Tried (attempts + why each failed), "
    "Context (files/constraints), Question (specific ask) \u2014 then implement its plan."
)
BRIEF = [
    "Goal: make tests/test_billing.py pass after the invoice refactor.",
    "Problem: AssertionError: assert Decimal('12.90') == Decimal('12.91')",
    "Tried: rounding at the end (off by one cent); per-line rounding (breaks 2 tests).",
    "Context: billing/prorate.py, billing/invoice.py; money is always Decimal.",
    "Question: where is the cent lost, and which rounding point is right for all 3 tests?",
]
ANSWER = [
    ("Diagnosis", " \u2014 prorate.py:38 builds the day ratio from a float ((end - start).days / 30) "
                  "before it meets a Decimal. The cent is lost there, not at rounding."),
    ("Plan", " \u2014 1. prorate.py:38: Decimal(days_used) / Decimal(days_in_month)  "
             "2. drop the per-line rounding in invoice.py  3. pytest tests/test_billing.py -q"),
    ("Pitfalls", " \u2014 Decimal(0.1) != Decimal(\"0.1\"); construct from int or str."),
]

BULLET = "\u23fa "
PROMPT = "\u276f "
FAIL = "\u2717 "
PASS = "\u2713 "
ELLIPSIS = "\u2026"


def script() -> list[tuple[list[Line], int]]:
    """Return (lines-so-far, duration_ms) for every frame."""
    frames: list[tuple[list[Line], int]] = []
    lines: list[Line] = []

    def emit(new: list[Line], ms: int = EVENT_MS, replace_last: bool = False) -> None:
        if replace_last:
            lines.pop()
        lines.extend(new)
        frames.append(([list(l) for l in lines], ms))

    def bash_then(result: Line, detail: Line | None = None) -> None:
        emit([L((BASH, DIM), ("  running" + ELLIPSIS, DIM))])
        new = [L((BASH, DIM), ("  0.8s", DIM)), result]
        if detail:
            new.append(detail)
        emit(new, replace_last=True)

    emit([L((PROMPT, PURPLE), ("fix the failing test in tests/test_billing.py", TEXT)), []])
    emit([L((BULLET + "Read(billing/prorate.py)", DIM), ("  read 64 lines", DIM))])
    bash_then(L(("  " + FAIL + "1 failed", RED)), L(("  " + ASSERT, DIM)))
    emit([L((BULLET + "Edit(billing/prorate.py)", DIM), ("  1 replacement", DIM))])
    bash_then(L(("  " + FAIL + "1 failed", RED)))
    emit([L((BULLET + "Edit(billing/invoice.py)", DIM), ("  2 replacements", DIM))])
    bash_then(L(("  " + FAIL + "3 failed", RED)))
    emit([[], L((BULLET + "I'm not sure why the test still fails \u2014 the rounding looks right.", TEXT))], HOLD_MS)
    emit([[], L((BULLET + "Stop hook (oracle):", YELLOW)), L(("  " + HOOK_MSG, YELLOW))], HOLD_MS)
    emit([[], L((BULLET + "Agent(oracle)", BLUE))])
    emit([L(("  " + b, DIM)) for b in BRIEF], 1400)
    emit([[], L(("  ", TEXT), (ANSWER[0][0], GREEN), (ANSWER[0][1], TEXT))], 1200)
    emit([L(("  ", TEXT), (h, GREEN), (body, TEXT)) for h, body in ANSWER[1:]], HOLD_MS)
    emit([[], L((BULLET + "Edit(billing/prorate.py)", DIM), ("  1 replacement", DIM))])
    emit([L((BULLET + "Edit(billing/invoice.py)", DIM), ("  1 replacement", DIM))])
    emit([L((BASH, DIM), ("  running" + ELLIPSIS, DIM))])
    emit([L((BASH, DIM), ("  0.7s", DIM)), L(("  " + PASS + "3 passed", GREEN))], FINAL_MS, replace_last=True)
    return frames


def main() -> int:
    steps = script()
    pal = build_palette()
    frames = [draw_frame(lines).quantize(palette=pal, dither=Image.Dither.NONE) for lines, _ in steps]
    durations = [ms for _, ms in steps]

    if os.environ.get("DEMO_DUMP_FRAMES"):  # debugging aid: write each frame as PNG
        dump = os.environ["DEMO_DUMP_FRAMES"]
        os.makedirs(dump, exist_ok=True)
        for i, fr in enumerate(frames):
            fr.convert("RGB").save(os.path.join(dump, f"frame_{i:02d}.png"))

    frames[0].save(
        OUT,
        save_all=True,
        append_images=frames[1:],
        duration=durations,
        loop=0,
        disposal=1,
        optimize=False,
    )

    # ---- verify
    size = os.path.getsize(OUT)
    with Image.open(OUT) as gif:
        n = getattr(gif, "n_frames", 1)
        total = 0
        for i in range(n):
            gif.seek(i)
            total += gif.info.get("duration", 0)
        dims = gif.size
    print(f"frames:    {n}")
    print(f"duration:  {total} ms")
    print(f"size:      {size} bytes")
    print(f"dims:      {dims[0]}x{dims[1]}")
    print(f"grid:      {COLS} cols x {ROWS} rows, cell {CELL:.1f}px, line {LINE_H}px")
    print("fallback glyphs: " + ascii(sorted(FALLBACK_CHARS)))  # ascii(): safe on cp1252 consoles
    ok = n > 1 and size <= SIZE_LIMIT and dims == (W, H)
    print("OK" if ok else "FAILED CHECKS")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
