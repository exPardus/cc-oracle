"""Normalize display names so equivalent spellings compare equal."""
from __future__ import annotations


def normalize(name: str) -> str:
    """Canonical form of `name` for comparison and deduplication."""
    return name.strip().lower()
