"""A screen: a menu over some items plus a fixed table of commands."""
from typing import Any, Iterable

from .dispatch import Dispatcher
from .menu import build_callbacks

COMMANDS = ("open", "save", "quit")


class Screen:
    def __init__(self, items: Iterable[str]) -> None:
        self.items = list(items)
        self.selected: str | None = None
        self.log: list[tuple[str, Any]] = []
        self.callbacks = build_callbacks(self.items, self.select)
        self.dispatcher = Dispatcher(COMMANDS, self.handle)

    def select(self, item: str) -> None:
        self.selected = item

    def handle(self, command: str, payload: Any) -> str:
        self.log.append((command, payload))
        return command

    def run(self, command: str, payload: Any = None) -> str:
        return self.dispatcher.dispatch(command, payload)
