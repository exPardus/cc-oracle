"""Command dispatch: a name-to-handler table built from a list of names."""
from typing import Any, Callable, Iterable

Handle = Callable[[str, Any], Any]


def build_handlers(names: Iterable[str], handle: Handle) -> dict[str, Callable[[Any], Any]]:
    """Map each name to a one-argument handler that runs `handle(name, payload)`."""
    handlers: dict[str, Callable[[Any], Any]] = {}
    for name in names:
        handlers[name] = lambda payload: handle(name, payload)
    return handlers


class Dispatcher:
    def __init__(self, names: Iterable[str], handle: Handle) -> None:
        self.handlers = build_handlers(names, handle)

    def dispatch(self, name: str, payload: Any = None) -> Any:
        return self.handlers[name](payload)
