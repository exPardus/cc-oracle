"""Normalize display names so equivalent spellings compare equal."""
from __future__ import annotations

import unicodedata


def normalize(name: str) -> str:
    """Canonical form of `name` for comparison and deduplication.

    Two steps, both needed: NFC composes a base letter plus combining
    marks (as some input methods produce) into the same single codepoint
    a different input method would have produced directly, so
    canonically-equivalent spellings become byte-equal. `casefold`, not
    `lower`, is what makes case-insensitive comparison work for letters
    like German "ß", which `lower` leaves untouched but which is supposed
    to compare equal to "SS".
    """
    return unicodedata.normalize("NFC", name.strip()).casefold()
