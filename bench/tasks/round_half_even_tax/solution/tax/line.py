"""Tax lines: a raw tax amount, as computed upstream, settled to whole cents."""
from decimal import Decimal

from .rounding import half_up


def settle(amount: float) -> Decimal:
    """Settle a raw tax amount to the cent, halves away from zero."""
    return half_up(amount)
