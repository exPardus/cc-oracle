from datetime import date
from decimal import Decimal

from billing.invoice import Invoice, to_cents
from billing.prorate import prorate, usage_ratio


def test_one_day_of_june_rounds_half_up():
    # 30.15 for 30 days is 1.005 per day; the price list rounds halves up.
    amount = to_cents(prorate(Decimal("30.15"), date(2025, 6, 1), date(2025, 6, 2)))
    assert amount == Decimal("1.01")


def test_invoice_total_is_sum_of_rounded_lines():
    inv = Invoice()
    inv.add_prorated("seat A", Decimal("30.15"), date(2025, 6, 1), date(2025, 6, 2))
    inv.add_prorated("seat B", Decimal("30.15"), date(2025, 6, 1), date(2025, 6, 2))
    assert inv.total() == Decimal("2.02")


def test_full_month_is_exact():
    # No rounding may creep in when the whole month is used.
    assert prorate(Decimal("30.15"), date(2025, 6, 1), date(2025, 7, 1)) == Decimal("30.15")
    assert usage_ratio(date(2025, 6, 1), date(2025, 7, 1)) == Decimal(1)


def test_below_half_rounds_down():
    # 10.00 / 31 = 0.32258...; must not round up.
    amount = to_cents(prorate(Decimal("10.00"), date(2025, 1, 1), date(2025, 1, 2)))
    assert amount == Decimal("0.32")


def test_prorate_returns_decimal():
    assert isinstance(prorate(Decimal("10.00"), date(2025, 1, 1), date(2025, 1, 16)), Decimal)
