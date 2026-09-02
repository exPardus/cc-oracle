# cache_stale_kwarg

**Symptom.** `test_discount_applies_after_an_undiscounted_quote_of_the_same_items`
expects 5.80 for a 20% discount and gets 7.25, the undiscounted price;
`test_cache_keeps_an_entry_per_discount` fails the same way. `quote` in
`quote.py` is where the failing tests point, and its arithmetic is correct.

**Trap.** `cached_on_items` in `cache.py` keys its `lru_cache` on the sorted
item tuple alone and smuggles the keyword options in through a dict it
overwrites before each call, so the first discount seen for a basket is the
one every later call gets back. The obvious repair, dropping the cache and
calling the function directly, fixes the prices and breaks
`test_repeated_identical_call_does_not_reprice` and
`test_same_items_in_any_order_share_one_cache_entry`, which passed as shipped:
both count catalogue lookups and require the second call to be a hit.
Clearing the cache whenever the options change keeps those two passing but
leaves `test_cache_keeps_an_entry_per_discount` failing, because alternating
discounts on one basket then re-price every time.

**Real fix.** Make the options part of the key: `compute(key, options)` with
the options frozen as a sorted tuple of items. `solution/pricing/cache.py`
does that and keeps the sorted item tuple so equal baskets in any order still
share one entry.
