"""A registry of account names that refuses to hold two spellings of the
same name."""
from __future__ import annotations

from .names import normalize


class Registry:
    def __init__(self) -> None:
        self._seen: set[str] = set()

    def add(self, name: str) -> None:
        key = normalize(name)
        if key in self._seen:
            raise ValueError(f"duplicate name: {name!r}")
        self._seen.add(key)
