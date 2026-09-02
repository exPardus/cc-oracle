"""One-line summary of a row stream for the dashboard."""
from decimal import Decimal
from typing import Iterable

from .money import total_of
from .rows import Row


def summary(rows: Iterable[Row]) -> dict:
    # `rows` may be a one-shot iterator; take it once, then read it twice.
    rows = list(rows)
    total: Decimal = total_of(rows)
    skus = [row.sku for row in rows]
    return {"count": len(skus), "skus": skus, "total": total}
