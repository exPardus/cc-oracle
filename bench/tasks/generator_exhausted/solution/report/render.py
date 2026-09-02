"""Plain-text rendering: a header, one line per row, then the total."""
from typing import Iterable

from .money import fmt, total_of
from .rows import Row

HEADER = f"{'SKU':<10} {'QTY':>3} {'AMOUNT':>10}"


def render(rows: Iterable[Row]) -> str:
    # `rows` may be a one-shot iterator; take it once, then read it twice.
    rows = list(rows)
    total = total_of(rows)
    lines = [HEADER]
    for row in rows:
        lines.append(f"{row.sku:<10} {row.qty:>3} {fmt(row.amount):>10}")
    lines.append(f"{'TOTAL':<14} {fmt(total):>10}")
    return "\n".join(lines)
