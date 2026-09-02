from datetime import datetime, timedelta, timezone

from deadlines.scheduler import Task, is_overdue, next_due
from deadlines.timestamps import parse_timestamp

NOW = datetime(2025, 6, 1, 12, 0, 1, tzinfo=timezone.utc)


def test_task_past_its_utc_deadline_is_overdue():
    assert is_overdue(Task("report", "2025-06-01T12:00:00Z"), now=NOW)


def test_task_before_its_utc_deadline_is_not_overdue():
    assert not is_overdue(Task("report", "2025-06-01T12:00:02Z"), now=NOW)


def test_deadline_with_offset_is_compared_as_an_instant():
    # 14:00 at +02:00 is 12:00Z, one second before NOW.
    assert is_overdue(Task("report", "2025-06-01T14:00:00+02:00"), now=NOW)
    assert not is_overdue(Task("report", "2025-06-01T14:00:02+02:00"), now=NOW)


def test_parser_returns_aware_datetimes():
    assert parse_timestamp("2025-06-01T12:00:00Z").utcoffset() == timedelta(0)
    assert parse_timestamp("2025-06-01T14:00:00+02:00").utcoffset() == timedelta(hours=2)


def test_same_instant_in_two_offsets_is_equal():
    assert parse_timestamp("2025-06-01T12:00:00Z") == parse_timestamp("2025-06-01T14:00:00+02:00")


def test_next_due_orders_across_offsets():
    # 14:30 at +02:00 is 12:30Z, half an hour before the 13:00Z task.
    tasks = [Task("later", "2025-06-01T13:00:00Z"), Task("sooner", "2025-06-01T14:30:00+02:00")]
    assert next_due(tasks).name == "sooner"
