# import_shadow

**Symptom.** Three of the four tests in `tests/test_schedule.py` fail with
`AttributeError: module 'calendar' has no attribute 'monthrange'`, raised
from `scheduling/month.py::days_in_month`. The traceback points straight at
`calendar.monthrange(year, month)[1]`, and that line is correct Python -
`calendar.monthrange` is a real stdlib function. Nothing in `scheduling/`
looks wrong.

**Trap.** `tests/calendar.py` is a small helper (`business_days`), not a
test file - pytest never collects it, it's just imported by
`test_schedule.py` with a plain `import calendar`. Under pytest's default
"prepend" import mode, `tests/` (the directory holding the test file, since
it has no `__init__.py`) is inserted at the front of `sys.path` before any
test code runs. `import calendar` anywhere in the process from that point
on - including inside `scheduling/month.py`, a completely unrelated
package - resolves to `sys.modules["calendar"]`, and once that key holds
the *helper*, every subsequent `import calendar` gets the helper back, not
the standard library, regardless of which file asks. (On this machine
`import pytest` itself pre-imports the real `calendar` module as a side
effect of a third-party plugin before collection starts, which would mask
the whole scenario; `conftest.py` evicts `sys.modules["calendar"]` up
front specifically so the collision reproduces the way it does on a plain
pytest install - that eviction is scaffolding, not the fix.) Three fixes
that look reasonable all fail: renaming `tests/calendar.py` or adding
`tests/__init__.py` are both edits under `tests/` and are off the table by
the rules of the exercise. Setting `--import-mode=importlib` in
`pytest.ini` (verified in a temp copy) stops pytest from prepending
`tests/` to `sys.path` at all - `scheduling`'s three tests then pass, but
it breaks the test file's *own* `import calendar`, which was always
supposed to reach the local helper: `test_business_days_helper_counts_weekdays`
now fails with the very same `AttributeError`, just relocated.

**Real fix.** Stop depending on the stdlib `calendar` module from
`scheduling/` at all - `days_in_month` doesn't need it. Compute the days in
a month from `date` arithmetic instead (first of next month minus first of
this month), so it makes no difference which module is sitting behind the
name `calendar` in `sys.modules` when the test suite runs. See
`solution/scheduling/month.py`.
