"""Rounding as the price list specifies: to the cent, halves away from zero."""
from decimal import ROUND_HALF_UP, Decimal


def half_up(amount: float, places: int = 2) -> Decimal:
    """Round `amount` to `places` decimal places, halves away from zero.

    `str(amount)` is the shortest decimal that reads back as the same float,
    so 2.675 arrives as the literal "2.675" and not as the binary value a
    hair below it. Built-in `round` uses that binary value and ties to even.
    """
    exponent = Decimal(1).scaleb(-places)
    return Decimal(str(amount)).quantize(exponent, rounding=ROUND_HALF_UP)
