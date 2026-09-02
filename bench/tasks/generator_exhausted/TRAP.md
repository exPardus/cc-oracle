# generator_exhausted

**Symptom.** `test_render_lists_every_line_item` renders three rows and finds
none of them in the output, while `test_render_total_is_correct` passes: the
total is right, the lines are gone. `test_summary_counts_every_row` gets a
count of 0 with the same correct total. The loop in `render.py` looks fine.

**Trap.** `render` hands `rows` to `money.total_of`, which drains the
generator that `load_rows` returns, so the loop after it sees nothing.
Making `load_rows` return a list fixes both failures and breaks
`test_load_rows_is_lazy`, which feeds a malformed second line and expects
`next()` to yield the first row before anything raises. Materializing inside
`total_of` changes nothing, since the caller's generator is still spent.
Adding `rows = list(rows)` to `render` alone leaves
`test_summary_counts_every_row` failing: `summary.py` has the same
sum-then-iterate shape in its own file.

**Real fix.** Materialize once at each consumer's entry. `solution/report/`
adds `rows = list(rows)` as the first line of both `render` and `summary`;
`load_rows` stays a generator.
