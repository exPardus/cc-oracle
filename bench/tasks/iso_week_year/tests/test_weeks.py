from datetime import date, timedelta

from reporting.labels import week_label
from reporting.summary import count_by_week


def test_last_days_of_december_belong_to_the_next_iso_year():
    # Monday 29 December 2025 opens ISO week 1 of 2026.
    assert week_label(date(2025, 12, 29)) == "2026-W01"


def test_first_days_of_january_belong_to_the_previous_iso_year():
    # Friday 1 January 2027 is still in ISO week 53 of 2026.
    assert week_label(date(2027, 1, 1)) == "2026-W53"


def test_mid_year_label():
    assert week_label(date(2025, 6, 15)) == "2025-W24"


def test_count_by_week_across_new_year():
    counts = count_by_week(date(2025, 12, 29), date(2026, 1, 12))
    assert counts == {"2026-W01": 7, "2026-W02": 7}
    assert list(counts) == ["2026-W01", "2026-W02"]


def test_count_by_week_uses_the_same_labels_as_week_label():
    start, end = date(2026, 12, 28), date(2027, 1, 11)
    days = [start + timedelta(days=i) for i in range((end - start).days)]
    assert set(count_by_week(start, end)) == {week_label(d) for d in days}
