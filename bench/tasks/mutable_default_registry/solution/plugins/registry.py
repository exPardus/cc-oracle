"""A registry maps plugin names to plugins. Each application owns one."""
from .hooks import Plugin, register


class Registry:
    def __init__(self, items: dict[str, Plugin] | None = None) -> None:
        self._items = {} if items is None else items

    def install(self, name: str, hooks: list[str] | None = None) -> Plugin:
        plugin = register(name) if hooks is None else register(name, hooks)
        self._items[name] = plugin
        return plugin

    def names(self) -> list[str]:
        return sorted(self._items)

    def hooks_for(self, name: str) -> list[str]:
        return list(self._items[name].hooks)
