"""Schedule tests.

`import calendar` below intentionally picks up the local tests/calendar.py
helper (business_days), not the standard library module - pytest's default
import mode puts tests/ at the front of sys.path.
"""
from datetime import date

import calendar

from scheduling.month import add_month, last_day_of_month, month_end_schedule


def test_business_days_helper_counts_weekdays():
    # Mon 2025-06-02 .. Sun 2025-06-08: 5 weekdays.
    assert calendar.business_days(date(2025, 6, 2), date(2025, 6, 9)) == 5


def test_last_day_of_month_june():
    assert last_day_of_month(date(2025, 6, 15)) == date(2025, 6, 30)


def test_add_month_clamps_to_shorter_month():
    # Jan 31 + 1 month -> Feb 28 (2025 is not a leap year).
    assert add_month(date(2025, 1, 31)) == date(2025, 2, 28)


def test_month_end_schedule_three_months():
    sched = month_end_schedule(date(2025, 1, 10), 3)
    assert sched == [date(2025, 1, 31), date(2025, 2, 28), date(2025, 3, 31)]
