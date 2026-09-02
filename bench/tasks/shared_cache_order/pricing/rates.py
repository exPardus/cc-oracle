"""Currency exchange rates, loaded from JSON fixtures and cached by currency.

`load_rates` and `load_rates_from_dict` both fill the module-level cache
with `setdefault`, so a currency's rate is whatever the *first* load that
mentioned it said - later loads that mention the same currency again are
no-ops for that key.
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
    for currency, rate in data.items():
        _CACHE.setdefault(currency, rate)


def load_rates_from_dict(data: dict) -> None:
    """Same as `load_rates`, for callers that already have a parsed dict."""
    global _LOAD_CALLS
    _LOAD_CALLS += 1
    for currency, rate in data.items():
        _CACHE.setdefault(currency, rate)


def get_rate(currency: str) -> float:
    """Look up a cached rate. Raises KeyError if it was never loaded."""
    return _CACHE[currency]


def load_calls() -> int:
    """How many times a load function has been called - a cheap way for
    tests to confirm `get_rate` isn't silently re-reading its source."""
    return _LOAD_CALLS


def clear_cache() -> None:
    _CACHE.clear()
