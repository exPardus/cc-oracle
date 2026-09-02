"""Titles map to file names; bodies round-trip through the store unchanged."""
import re
from pathlib import Path

from .store import NoteStore


def slug(title: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", title.lower()).strip("-") or "untitled"


class Notebook:
    def __init__(self, root: Path) -> None:
        self.store = NoteStore(root)

    def add(self, title: str, body: str) -> None:
        self.store.write(slug(title), body)

    def get(self, title: str) -> str:
        return self.store.read(slug(title))

    def titles(self) -> list[str]:
        return self.store.names()
