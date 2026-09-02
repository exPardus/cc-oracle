"""Invoices are lists of lines. Every line is stored in whole cents."""
from datetime import date
from decimal import ROUND_HALF_UP, Decimal

from .prorate import prorate

CENT = Decimal("0.01")


def to_cents(amount: Decimal) -> Decimal:
    """Round to the cent, halves away from zero, as the price list promises."""
    return amount.quantize(CENT, rounding=ROUND_HALF_UP)


class Invoice:
    def __init__(self) -> None:
        self.lines: list[tuple[str, Decimal]] = []

    def add_prorated(self, description: str, monthly_price: Decimal, start: date, end: date) -> None:
        self.lines.append((description, to_cents(prorate(monthly_price, start, end))))

    def total(self) -> Decimal:
        return sum((amount for _, amount in self.lines), Decimal("0.00"))
