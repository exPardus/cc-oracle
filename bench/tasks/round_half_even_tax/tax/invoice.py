"""Invoices are lists of tax lines. Every line is stored in whole cents."""
from decimal import Decimal

from .line import settle


class Invoice:
    def __init__(self) -> None:
        self.lines: list[tuple[str, Decimal]] = []

    def add_tax(self, description: str, amount: float) -> None:
        self.lines.append((description, settle(amount)))

    def total(self) -> Decimal:
        return sum((amount for _, amount in self.lines), Decimal("0.00"))
