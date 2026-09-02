from decimal import Decimal

from tax.invoice import Invoice
from tax.line import settle


def test_two_six_seven_five_rounds_up():
    # The price list says halves round up; 2.675 is a half.
    assert settle(2.675) == Decimal("2.68")


def test_one_zero_zero_five_rounds_up():
    assert settle(1.005) == Decimal("1.01")


def test_negative_half_rounds_away_from_zero():
    assert settle(-2.675) == Decimal("-2.68")


def test_exact_binary_half_rounds_up():
    # 0.125 is exact in binary, so this one is about the rule, not the representation.
    assert settle(0.125) == Decimal("0.13")


def test_below_half_rounds_down():
    assert settle(2.674) == Decimal("2.67")
    assert isinstance(settle(2.674), Decimal)


def test_invoice_total_is_sum_of_rounded_lines():
    inv = Invoice()
    inv.add_tax("widgets", 2.675)
    inv.add_tax("gadgets", 2.674)
    inv.add_tax("gizmos", 0.125)
    inv.add_tax("refund", -0.375)
    assert inv.total() == Decimal("5.10")  # 2.68 + 2.67 + 0.13 - 0.38
