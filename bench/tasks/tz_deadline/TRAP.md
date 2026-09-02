# tz_deadline

**Symptom.** `test_task_past_its_utc_deadline_is_overdue` raises `TypeError:
can't compare offset-naive and offset-aware datetimes` on the comparison in
`scheduler.py`, and the not-overdue test fails the same way. The aware
`utc_now()` a few lines above is the first thing the traceback points at,
and it is correct.

**Trap.** `parse_timestamp` in `timestamps.py` drops the trailing `Z` so that
3.10's `fromisoformat` accepts the string, and so returns a naive datetime
for every UTC deadline while offset deadlines stay aware. Stripping the
tzinfo from `now` inside `is_overdue` passes the two UTC tests and fails
`test_deadline_with_offset_is_compared_as_an_instant` with the same
`TypeError` from the other side. Attaching UTC to the parsed value inside
`is_overdue` passes all three overdue tests and leaves
`test_parser_returns_aware_datetimes`,
`test_same_instant_in_two_offsets_is_equal` and
`test_next_due_orders_across_offsets` failing.

**Real fix.** Make the parser say what `Z` means: replace it with `+00:00`
before `fromisoformat`, so every timestamp comes back aware.
`solution/deadlines/timestamps.py` does that; `scheduler.py` is untouched.
