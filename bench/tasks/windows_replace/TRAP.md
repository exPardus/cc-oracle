# windows_replace

**Symptom.** Every test in `test_atomic.py` fails, including the one that
just writes a brand-new file, with `PermissionError: [WinError 32] The
process cannot access the file because it is being used by another
process`. That reads like a permissions or antivirus-lock problem, and the
traceback points at the `os.rename` line, which looks unremarkable.

**Trap.** `write_atomic` calls `os.rename(tmp, path)` while the `with
open(tmp, ...)` block that created `tmp` is still open, so on Windows the
rename is attempted against a file that still has a live handle and fails.
Closing the handle before renaming (moving the call outside the `with`)
fixes `test_creates_new_file_with_content` but flips the other four tests
from `PermissionError` to `FileExistsError`: `os.rename` on Windows refuses
to land on a file that already exists, unlike POSIX where it silently
replaces it. Catching that and falling back to `shutil.copy(tmp, path)`
makes the content correct again but leaves `tmp` sitting next to `path`
forever, since nothing ever removes it, so
`test_overwrite_replaces_content_exactly` and
`test_repeated_writes_keep_latest_only` still fail on the leftover
`*.tmp` glob. Removing the target first (`os.remove(path)` then
`os.rename(tmp, path)`) gets the content and the cleanup both right, but
`test_overwrite_does_not_delete_target_first` monkeypatches
`store.atomic.os.remove` to raise, specifically to catch this: a real
atomic write never needs to delete the destination, only replace it, and a
delete-then-rename briefly leaves no file at `path` at all, which is the
same partial-write hazard `write_atomic` exists to avoid. On POSIX none of
this triggers: `os.rename` there already replaces an existing destination
atomically, and renaming a file out from under an open handle is legal, so
the second bug is invisible off Windows and the shipped code would pass
except for the first bug.

**Real fix.** Close the temp file's handle before renaming, and use
`os.replace(tmp, path)` instead of `os.rename`: `os.replace` is documented
to replace an existing destination atomically on every platform, which is
exactly the POSIX behavior `os.rename` already had and Windows never did.
`solution/store/atomic.py` does both.
