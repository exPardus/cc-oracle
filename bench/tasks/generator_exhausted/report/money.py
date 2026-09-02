"""Money helpers shared by every consumer of a row stream."""
from decimal import Decimal
from typing import Iterable

from .rows import Row


def total_of(rows: Iterable[Row]) -> Decimal:
    """The sum of every row's amount."""
    return sum((row.amount for row in rows), Decimal("0.00"))


def fmt(amount: Decimal) -> str:
    return f"{amount:.2f}"
