"""Proration of a monthly price over part of a month."""
import calendar
from datetime import date
from decimal import Decimal


def days_in_month(d: date) -> int:
    return calendar.monthrange(d.year, d.month)[1]


def usage_ratio(start: date, end: date) -> Decimal:
    """Share of `start`'s month covered by [start, end).

    A full month (the first of one month to the first of the next) is 1.
    """
    days_used = (end - start).days
    total = days_in_month(start)
    return Decimal(days_used / total)


def prorate(monthly_price: Decimal, start: date, end: date) -> Decimal:
    """The part of `monthly_price` owed for the days in [start, end)."""
    return monthly_price * usage_ratio(start, end)
