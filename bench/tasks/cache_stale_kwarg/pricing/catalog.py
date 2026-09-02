"""The price list, with a counter so tests can see how often it is consulted."""
from decimal import Decimal

PRICES = {
    "apple": Decimal("1.50"),
    "pear": Decimal("2.00"),
    "fig": Decimal("4.25"),
}

_lookups = 0


def unit_price(sku: str) -> Decimal:
    """Price of one unit of `sku`. In production this is a database round trip."""
    global _lookups
    _lookups += 1
    return PRICES[sku]


def lookups() -> int:
    """How many times `unit_price` has been called."""
    return _lookups
