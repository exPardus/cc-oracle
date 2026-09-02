"""Loads fixture B, which shares a currency (EUR) with fixture A. Collected
after test_rates_a.py, so by the time these tests run the cache may
already hold A's values - loading B here must still make B's values win.
"""
import json
import os

import pytest

from pricing import rates

FIXTURE_B = os.path.join(os.path.dirname(__file__), "fixtures", "rates_b.json")


def test_rate_from_fixture_b():
    with open(FIXTURE_B, "r", encoding="utf-8") as f:
        data = json.load(f)
    rates.load_rates_from_dict(data)
    # EUR is in both fixtures; whichever was loaded most recently must win.
    assert rates.get_rate("EUR") == 0.92
    assert rates.get_rate("JPY") == 149.5


def test_currency_only_in_a_is_unknown_after_b():
    with open(FIXTURE_B, "r", encoding="utf-8") as f:
        data = json.load(f)
    rates.load_rates_from_dict(data)
    # GBP was never in fixture B. A currency that the most recent load
    # didn't mention is not available, no matter what an earlier,
    # unrelated load once cached for it.
    with pytest.raises(KeyError):
        rates.get_rate("GBP")
