"""Per-week counts over a date range."""
from datetime import date, timedelta

from .labels import week_label


def count_by_week(start: date, end: date) -> dict[str, int]:
    """Days of [start, end) in each ISO week, keyed by week label, in order."""
    counts: dict[str, int] = {}
    d = start
    while d < end:
        key = week_label(d)
        counts[key] = counts.get(key, 0) + 1
        d += timedelta(days=1)
    return counts
