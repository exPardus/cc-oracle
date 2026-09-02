"""Month-end scheduling helpers, built on date arithmetic only.

Deliberately does not `import calendar`: under pytest, tests/calendar.py
(a same-named test helper) sits ahead of the standard library on
sys.path, so any `import calendar` anywhere in the process - including
inside this package - resolves to that helper, not the stdlib module.
"""
from datetime import date


def days_in_month(year: int, month: int) -> int:
    if month == 12:
        next_month_first = date(year + 1, 1, 1)
    else:
        next_month_first = date(year, month + 1, 1)
    return (next_month_first - date(year, month, 1)).days


def last_day_of_month(d: date) -> date:
    return date(d.year, d.month, days_in_month(d.year, d.month))


def add_month(d: date) -> date:
    """Return the same day next month, clamped to the last day if needed."""
    if d.month == 12:
        year, month = d.year + 1, 1
    else:
        year, month = d.year, d.month + 1
    day = min(d.day, days_in_month(year, month))
    return date(year, month, day)


def month_end_schedule(start: date, n: int) -> list[date]:
    """`n` consecutive month-end dates, the first one in `start`'s month."""
    out = []
    cur = date(start.year, start.month, 1)
    for _ in range(n):
        out.append(last_day_of_month(cur))
        cur = add_month(cur)
    return out
