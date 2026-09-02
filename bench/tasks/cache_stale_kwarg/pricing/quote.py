"""Quotes: catalogue line totals, less a percentage discount."""
from decimal import Decimal

from .cache import cached_on_items
from .catalog import unit_price

CENT = Decimal("0.01")


@cached_on_items
def quote(items: list[tuple[str, int]], discount: int = 0) -> Decimal:
    """Total for `items`, a list of (sku, qty), less `discount` percent."""
    subtotal = sum((unit_price(sku) * qty for sku, qty in items), Decimal("0"))
    return (subtotal - subtotal * discount / 100).quantize(CENT)
