# shared_cache_order

**Symptom.** `python -m pytest -q` reports two failures in
`tests/test_rates_b.py`: `test_rate_from_fixture_b` expects `EUR == 0.92`
and gets `0.9`, and `test_currency_only_in_a_is_unknown_after_b` expects a
`KeyError` for `GBP` and doesn't get one. Both tests load fixture B and
check what came out of it - fixture B itself is correct, and `get_rate`
just does a dict lookup. Run `pytest tests/test_rates_b.py` by itself,
though, and it's green. Run `pytest tests/test_rates_a.py` by itself and
it's also green. The bug only shows up when the whole suite runs, in file
order - it looks flaky.

**Trap.** `pricing/rates.py` keeps rates in a module-level `_CACHE` dict
that both loaders (`load_rates`, `load_rates_from_dict`) fill with
`_CACHE.setdefault(currency, rate)`: first value for a currency wins,
forever, for the rest of the process. `tests/test_rates_a.py` sorts and
therefore collects before `tests/test_rates_b.py`, so its calls to
`load_rates` seed `EUR` and `GBP` into the shared cache first; when
`test_rates_b.py` then loads fixture B, `setdefault` refuses to overwrite
the `EUR` it already has, and never had `GBP` to begin with (verified in a
temp copy). A model that notices "cache" and "stale value" and reaches for
`_CACHE.clear()` right after the dict is declared changes nothing - it
runs once at import time, before either fixture has loaded anything, so
the cross-file contamination during the actual test run is untouched and
the exact same two tests fail (verified). A model that finds the
`setdefault` in `load_rates` and swaps it for `_CACHE[currency] = rate`
also still leaves both `test_rates_b.py` tests failing (verified) - because
`test_rates_b.py` calls `load_rates_from_dict`, not `load_rates`, and that
second loader has its own, separate `setdefault` line that was never
touched. It's an easy miss: the two functions look like near-duplicates of
each other, and grepping for the one call site that's "obviously" the
culprit finds only one of the two.

**Real fix.** Both loaders need to stop merging and start replacing: each
load call should represent everything currently known, so `_CACHE.clear()`
followed by `_CACHE.update(data)` in *both* `load_rates` and
`load_rates_from_dict` (`solution/pricing/rates.py`). The alternative
mentioned in the design notes - keying the cache by `(path, currency)`
instead - would also work but complicates `get_rate`'s signature for no
benefit here; last-load-wins is simpler and is what every test in this
suite actually checks for, including `test_get_rate_does_not_reload`, which
confirms the fix didn't turn `get_rate` itself into something that
re-parses a source file on every lookup.
