"""Menus are lists of callbacks, one per item, built from a list of items."""
from typing import Callable, Sequence, TypeVar

T = TypeVar("T")


def build_callbacks(items: Sequence[T], select: Callable[[T], None]) -> list[Callable[[], None]]:
    """One zero-argument callback per item; calling the i-th selects items[i].

    The default argument binds `item` when the lambda is created. A bare
    `lambda: select(item)` would look the name up when called, after the
    loop has finished, and every callback would select the last item.
    """
    callbacks: list[Callable[[], None]] = []
    for item in items:
        callbacks.append(lambda item=item: select(item))
    return callbacks
