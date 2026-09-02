"""Loads fixture A. Collected before test_rates_b.py (alphabetical order),
so its calls to load_rates() run - and populate the shared module-level
cache - first.
"""
import os

from pricing import rates

FIXTURE_A = os.path.join(os.path.dirname(__file__), "fixtures", "rates_a.json")


def test_rate_from_fixture_a():
    rates.load_rates(FIXTURE_A)
    assert rates.get_rate("USD") == 1.0
    assert rates.get_rate("EUR") == 0.9


def test_get_rate_does_not_reload():
    rates.load_rates(FIXTURE_A)
    before = rates.load_calls()
    for _ in range(5):
        rates.get_rate("USD")
    assert rates.load_calls() == before
