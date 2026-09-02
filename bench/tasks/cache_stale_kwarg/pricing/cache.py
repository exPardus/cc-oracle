"""A memoizer for pure pricing functions."""
import functools


def cached_on_items(fn):
    """Memoize `fn(items, **options)` on its item list.

    Items arrive as (sku, qty) pairs in whatever order the basket produced
    them; they are sorted and frozen so equal baskets share one cache entry.
    """
    options: dict = {}

    @functools.lru_cache(maxsize=256)
    def compute(key):
        return fn(list(key), **options)

    @functools.wraps(fn)
    def wrapper(items, **kwargs):
        options.clear()
        options.update(kwargs)
        return compute(tuple(sorted(items)))

    wrapper.cache_info = compute.cache_info
    wrapper.cache_clear = compute.cache_clear
    return wrapper
