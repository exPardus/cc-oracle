# iso_week_year

**Symptom.** `test_last_days_of_december_belong_to_the_next_iso_year` expects
`2026-W01` for 29 December 2025 and gets `2025-W52`; 1 January 2027 comes back
`2027-W00` instead of `2026-W53`, 15 June 2025 is a week low, and the per-week
counts across New Year land in the wrong buckets. `week_label` in `labels.py`
is where the failing tests point, and it only formats what `weeks.py` hands it.

**Trap.** `week_number` uses `strftime("%W")`, which starts week 1 on the
first Monday of the calendar year and is not the ISO week, and `week_year`
returns `d.year`. Switching to `%V` alone gives `2025-W01` for 29 December
2025, the right week under the wrong year, so both boundary tests still fail.
Fixing both functions in `weeks.py` (`%G` with `%V`, or `isocalendar()`)
passes every label test and breaks
`test_count_by_week_uses_the_same_labels_as_week_label`, which passed as
shipped: `count_by_week` in `summary.py` carries its own copy of the `%Y-W%W`
pattern, so its buckets no longer agree with the labels, and
`test_count_by_week_across_new_year` stays failing.

**Real fix.** `date.isocalendar()` for both the week number and the year, and
one source of truth for the label. `solution/reporting/weeks.py` does the
former; `solution/reporting/summary.py` keys its buckets on `week_label`
instead of a second format string.
