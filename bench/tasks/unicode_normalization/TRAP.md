# unicode_normalization

**Symptom.** Four tests fail. Two are about `normalize()` directly:
"Ångstrom" typed as a single precomposed character does not equal the same
word typed with the ring-above as a separate combining mark, and
"straße" does not equal "STRASSE" even after normalizing case. The other
two are about `Registry.add()`: adding "STRASSE" does not block a later
"straße", and adding the precomposed form does not block the decomposed
one. All four look like they might have independent causes, since two
live in `accounts/names.py` and two in `accounts/registry.py`.

**Trap.** They are one bug in two places. `normalize()` is `strip().lower()`,
which is not enough on its own for either failing case, and neither
half-measure fixes both: adding `.casefold()` in place of `.lower()`
(needed because `"ß".lower() == "ß"`, while `"ß".casefold() == "ss"`)
fixes the eszett test but leaves the precomposed/decomposed test failing,
because casefold does not recompose combining sequences. Adding
`unicodedata.normalize("NFC", ...)` without casefold fixes the
precomposed/decomposed test but leaves the eszett test failing, because
NFC does not touch casing. Both are needed together. Even with `normalize()`
fully fixed, the two `Registry` tests still fail, because `Registry.add()`
never calls `normalize()` at all — it has its own inline
`name.strip().lower()`, written before anyone imagined non-ASCII names,
and duplicated the same shortcut in a second place. Fixing `names.py` and
stopping there leaves the registry exactly as broken as it started.

**Real fix.** `normalize()` becomes
`unicodedata.normalize("NFC", name.strip()).casefold()`, and
`Registry.add()` calls `normalize(name)` instead of its own inline
`.lower()`. `solution/accounts/names.py` and
`solution/accounts/registry.py` both change; fixing only one leaves two of
the four failing tests exactly as broken as they were before.
