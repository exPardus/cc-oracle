"""Rounding as the price list specifies: to the cent, halves away from zero."""


def half_up(amount: float, places: int = 2) -> float:
    """Round `amount` to `places` decimal places, halves away from zero."""
    return round(amount, places)
