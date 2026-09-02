"""Currency exchange rates, loaded from JSON fixtures and cached by currency.

Each load call defines the full set of rates it knows about: loading a
file (or dict) clears whatever was cached before and replaces it, so the
most recent load always wins and nothing from an earlier, unrelated load
can leak through. Both loaders implement this the same way.
"""
import json

_CACHE: dict[str, float] = {}
_LOAD_CALLS = 0


def load_rates(path: str) -> None:
    """Load a `{currency: rate}` JSON file into the cache."""
    global _LOAD_CALLS
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
    _LOAD_CALLS += 1
    _CACHE.clear()
    _CACHE.update(data)


def load_rates_from_dict(data: dict) -> None:
    """Same as `load_rates`, for callers that already have a parsed dict."""
    global _LOAD_CALLS
    _LOAD_CALLS += 1
    _CACHE.clear()
    _CACHE.update(data)


def get_rate(currency: str) -> float:
    """Look up a cached rate. Raises KeyError if it was never loaded."""
    return _CACHE[currency]


def load_calls() -> int:
    """How many times a load function has been called - a cheap way for
    tests to confirm `get_rate` isn't silently re-reading its source."""
    return _LOAD_CALLS


def clear_cache() -> None:
    _CACHE.clear()
