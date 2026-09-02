# utf8_windows

**Symptom.** `test_note_round_trips_unchanged` raises `UnicodeEncodeError:
'charmap' codec can't encode characters` from inside `Notebook.add`; the
on-disk and reopening tests die the same way. The traceback ends in
`store.py`, one frame below the notebook code the test calls.

**Trap.** Both `open()` calls in `NoteStore` leave `encoding` unset, so on a
cp1252 machine the write cannot represent the CJK or the emoji at all.
`errors="replace"` (or `"ignore"`) on the opens makes the exception go away
and fails `test_note_round_trips_unchanged` with `'naïve — ??? ?'`, while
`test_file_on_disk_is_utf8` fails with a `UnicodeDecodeError` because the
bytes on disk are cp1252. Fixing only the write side stops the raise and
turns the round trip into `'naÃ¯ve â€” æ—¥æœ¬èªž ðŸ™‚'`;
`test_note_survives_reopening` fails the same way. This task only fails
where Python's default text encoding is not UTF-8. It does on the benchmark
machine (Windows 10, Python 3.10.1, `locale.getpreferredencoding()` is
`cp1252`, UTF-8 mode off), where the verifier was run.

**Real fix.** `encoding="utf-8"` on both the write and the read in
`store.py`, so the file format does not depend on the machine that wrote it.
`solution/notes/store.py` does that and nothing else.
