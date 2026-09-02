"""Week arithmetic for the reporting package."""
from datetime import date


def week_number(d: date) -> int:
    """ISO week number of the week containing `d`; weeks start on Monday."""
    return d.isocalendar()[1]


def week_year(d: date) -> int:
    """The ISO year that the week containing `d` belongs to.

    Not `d.year`: the week around New Year belongs to one year or the other
    as a whole, so 29 December can be week 1 of the next year and 1 January
    week 53 of the previous one.
    """
    return d.isocalendar()[0]
