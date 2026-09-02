"""Evict any pre-imported stdlib `calendar` before collection.

Some pytest plugins installed in this environment import the standard
library `calendar` module as a side effect of their own startup (Faker's
date providers, in particular), which would pre-populate
sys.modules["calendar"] before any test code runs and mask the import
collision this suite is built around. Evicting it here makes the
collision reproduce deterministically regardless of which third-party
pytest plugins happen to be installed alongside this task.
"""
import sys

sys.modules.pop("calendar", None)
