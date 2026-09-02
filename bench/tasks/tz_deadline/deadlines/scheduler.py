"""Which tasks are late, and which one comes up next."""
from dataclasses import dataclass
from datetime import datetime, timezone

from .timestamps import parse_timestamp


@dataclass
class Task:
    name: str
    due: str  # ISO-8601, exactly as the API sent it


def utc_now() -> datetime:
    """The current instant, timezone-aware. Tests pass `now` explicitly instead."""
    return datetime.now(timezone.utc)


def is_overdue(task: Task, now: datetime | None = None) -> bool:
    if now is None:
        now = utc_now()
    return parse_timestamp(task.due) < now


def next_due(tasks: list[Task]) -> Task | None:
    """The task with the earliest deadline, or None if there are none."""
    if not tasks:
        return None
    return min(tasks, key=lambda t: parse_timestamp(t.due))
