# round_half_even_tax

**Symptom.** `test_two_six_seven_five_rounds_up` expects 2.68 and gets 2.67;
the 1.005, -2.675 and 0.125 tests and the invoice total fail the same way.
`settle` in `line.py` is where the Decimal appears, and it is not the bug.

**Trap.** `rounding.half_up` is `round(amount, 2)` on a float: 2.675 is
stored a hair below 2.675 and rounds down, and 0.125, which is exact, ties
to even. Adding an epsilon (`round(amount + 1e-9, 2)`) fixes the three
positive cases and leaves `test_negative_half_rounds_away_from_zero` at
-2.67; it also turns the refund line's -0.375, which the shipped code
already gets right, into -0.37, so the invoice test keeps failing at a new
total. Converting first with `Decimal(amount)` carries the binary value
across and fixes only 0.125. Reading "halves round up" as `ROUND_UP`
breaks `test_below_half_rounds_down`.

**Real fix.** `Decimal(str(amount)).quantize(Decimal("0.01"),
rounding=ROUND_HALF_UP)`: `str` gives the shortest decimal that reads back
as the same float, so the half is really a half, and `ROUND_HALF_UP` rounds
ties away from zero on both sides. `solution/tax/rounding.py` does that and
returns a Decimal; `solution/tax/line.py` passes it through.
