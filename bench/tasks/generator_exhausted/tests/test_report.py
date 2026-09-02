from collections.abc import Iterator
from decimal import Decimal

import pytest

from report.render import render
from report.rows import Row, load_rows
from report.summary import summary

SAMPLE = """\
# sku, qty, unit
widget, 2, 1.25
gadget, 1, 5.00
gizmo, 4, 1.25
"""


def test_render_lists_every_line_item():
    out = render(load_rows(SAMPLE))
    assert "widget" in out and "gadget" in out and "gizmo" in out


def test_render_total_is_correct():
    out = render(load_rows(SAMPLE))
    assert out.splitlines()[-1].endswith("12.50")


def test_summary_counts_every_row():
    s = summary(load_rows(SAMPLE))
    assert s["count"] == 3
    assert s["skus"] == ["widget", "gadget", "gizmo"]
    assert s["total"] == Decimal("12.50")


def test_load_rows_is_lazy():
    # The second line is malformed; a lazy loader must not touch it until asked.
    rows = load_rows("widget, 2, 1.25\nthis line is not a row")
    assert isinstance(rows, Iterator)
    assert next(rows) == Row("widget", 2, Decimal("1.25"))
    with pytest.raises(ValueError):
        next(rows)


def test_render_accepts_a_list():
    out = render(list(load_rows(SAMPLE)))
    assert len(out.splitlines()) == 5  # header, three rows, total
