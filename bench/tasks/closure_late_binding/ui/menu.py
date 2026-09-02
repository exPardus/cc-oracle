"""Menus are lists of callbacks, one per item, built from a list of items."""
from typing import Callable, Sequence, TypeVar

T = TypeVar("T")


def build_callbacks(items: Sequence[T], select: Callable[[T], None]) -> list[Callable[[], None]]:
    """One zero-argument callback per item; calling the i-th selects items[i]."""
    callbacks: list[Callable[[], None]] = []
    for item in items:
        callbacks.append(lambda: select(item))
    return callbacks
