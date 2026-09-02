"""Test helper: count business days (Mon-Fri) in a date range.

This is a plain helper module, not a pytest test file - its name doesn't
match test_*.py / *_test.py, so pytest never collects it as a test module.
test_schedule.py imports it with a plain `import calendar`.
"""
from datetime import date, timedelta


def business_days(start: date, end: date) -> int:
    """Count weekdays in [start, end)."""
    days = 0
    d = start
    while d < end:
        if d.weekday() < 5:
            days += 1
        d += timedelta(days=1)
    return days
