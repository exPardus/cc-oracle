# decimal_prorate

**Symptom.** `test_one_day_of_june_rounds_half_up` expects 1.01 and gets 1.00;
the invoice total test fails the same way. The rounding code in `invoice.py`
is the first thing the failing test points at, and it is correct.

**Trap.** `usage_ratio` computes `days_used / total` as a float before wrapping
it in `Decimal`, so 1/30 arrives as 0.0333…328 and 30.15 times that is
1.00499…, which rounds to 1.00. Every rounding-side "fix" breaks
`test_below_half_rounds_down` (switching to `ROUND_UP` or `ROUND_CEILING`) or
`test_full_month_is_exact` (adding an epsilon) instead. Note: `Decimal(days_used)
/ Decimal(total)` does pass, because the 28-digit context rounds the product
30.15 × 0.0333…333 up to exactly 1.005; an earlier version of this file
claimed otherwise and was wrong.

**Real fix.** Compute the ratio in `Decimal`, or better, multiply before
dividing so `30.15 * 1 / 30` is exactly 1.005 at any precision.
`solution/billing/prorate.py` does the latter and keeps `usage_ratio` exact for
the full-month case.
