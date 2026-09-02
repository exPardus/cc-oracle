"""Period labels used as report headings and bucket keys."""
from datetime import date

from .weeks import week_number, week_year


def month_label(d: date) -> str:
    return f"{d.year}-{d.month:02d}"


def week_label(d: date) -> str:
    """ISO week label such as 2026-W01 for the week containing `d`."""
    return f"{week_year(d)}-W{week_number(d):02d}"
