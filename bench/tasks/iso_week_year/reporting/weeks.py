"""Week arithmetic for the reporting package."""
from datetime import date


def week_number(d: date) -> int:
    """Number of the week containing `d`; weeks start on Monday."""
    return int(d.strftime("%W"))


def week_year(d: date) -> int:
    """The year that the week containing `d` belongs to."""
    return d.year
