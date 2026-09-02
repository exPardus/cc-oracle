from decimal import Decimal

from pricing.catalog import lookups
from pricing.quote import quote


def test_undiscounted_quote_is_the_sum_of_lines():
    assert quote([("apple", 2), ("pear", 1)]) == Decimal("5.00")


def test_discount_applies_after_an_undiscounted_quote_of_the_same_items():
    items = [("apple", 2), ("fig", 1)]
    assert quote(items) == Decimal("7.25")
    assert quote(items, discount=20) == Decimal("5.80")


def test_repeated_identical_call_does_not_reprice():
    items = [("fig", 3), ("pear", 2)]
    first = quote(items, discount=5)
    before = lookups()
    assert quote(items, discount=5) == first
    assert lookups() == before


def test_same_items_in_any_order_share_one_cache_entry():
    total = quote([("pear", 1), ("apple", 3)], discount=10)
    before = lookups()
    assert quote([("apple", 3), ("pear", 1)], discount=10) == total == Decimal("5.85")
    assert lookups() == before


def test_cache_keeps_an_entry_per_discount():
    items = [("apple", 1), ("fig", 1)]
    quote(items)
    quote(items, discount=20)
    before = lookups()
    assert quote(items) == Decimal("5.75")
    assert quote(items, discount=20) == Decimal("4.60")
    assert lookups() == before
