"""A memoizer for pure pricing functions."""
import functools


def cached_on_items(fn):
    """Memoize `fn(items, **options)` on its item list and its options.

    Items arrive as (sku, qty) pairs in whatever order the basket produced
    them; they are sorted and frozen so equal baskets share one cache entry.
    The options are part of the key too: a discount changes the result, so
    an entry is only good for the options it was computed with.
    """
    @functools.lru_cache(maxsize=256)
    def compute(key, options):
        return fn(list(key), **dict(options))

    @functools.wraps(fn)
    def wrapper(items, **kwargs):
        return compute(tuple(sorted(items)), tuple(sorted(kwargs.items())))

    wrapper.cache_info = compute.cache_info
    wrapper.cache_clear = compute.cache_clear
    return wrapper
