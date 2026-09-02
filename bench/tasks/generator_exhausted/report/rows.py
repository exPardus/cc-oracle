"""Rows of a sales report, parsed lazily from `sku, qty, unit` lines."""
from decimal import Decimal
from typing import Iterator, NamedTuple


class Row(NamedTuple):
    sku: str
    qty: int
    unit: Decimal

    @property
    def amount(self) -> Decimal:
        return self.qty * self.unit


def load_rows(text: str) -> Iterator[Row]:
    """Yield one Row per non-blank, non-comment line of `text`.

    Parsing is lazy on purpose: a malformed line raises where it is reached,
    not before the first good row has been handed out.
    """
    for raw in text.splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        sku, qty, unit = (part.strip() for part in line.split(","))
        yield Row(sku, int(qty), Decimal(unit))
