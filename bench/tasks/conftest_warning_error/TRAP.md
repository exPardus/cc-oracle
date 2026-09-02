# conftest_warning_error

**Symptom.** `test_export_custom_separator` and
`test_export_explicit_comma_matches_default` fail, not with an assertion
mismatch but with `DeprecationWarning: write_csv(delim=...) is deprecated,
use write_csv(sep=...) instead`, raised as an exception from
`report/csvout.py`. `tests/conftest.py` turns every `DeprecationWarning`
into a test failure (`filterwarnings("error", category=DeprecationWarning)`
in `pytest_configure`, the project's real CI policy for this repo, and off
limits to edit), so the traceback lands squarely inside `write_csv`, the
low-level "library" function, and never mentions `export` at all.

**Trap.** `write_csv` is not the problem: it is deprecating its old
`delim` parameter correctly and the warning is doing its job. The bug is
one call site up, in `report/export.py`, which still calls
`write_csv(rows, delim=sep)` on every request that supplies a custom
separator, including a redundant one that just repeats the default. Two
fixes aimed at the traceback both look plausible and both fail the same
test differently than intended: deleting the `warnings.warn(...)` call
inside `write_csv` (reasoning "this warning is the thing breaking the
build") makes `test_deprecated_alias_still_warns` fail with "DID NOT WARN",
because that test calls `write_csv(..., delim=...)` directly and expects
the warning to still fire for callers who haven't migrated. Leaving the
call but wrapping the whole body of `write_csv` in
`warnings.catch_warnings()` with `simplefilter("ignore", DeprecationWarning)`
fails the exact same test the exact same way: silencing the warning at its
source is indistinguishable from deleting it, once nothing outside
`write_csv` ever sees it. Either change also leaves `export`'s real
mistake in place, so the fix is not just wrong, it doesn't even fix the
right thing.

**Real fix.** Change `export` to call `write_csv(rows, sep=sep)` instead
of `write_csv(rows, delim=sep)`, since `export` already knows the current
parameter name and has no excuse to use the deprecated alias.
`solution/report/export.py` makes only that change; `write_csv` and its
warning are untouched, so direct callers of the deprecated alias (like
`test_deprecated_alias_still_warns`) keep getting warned.
