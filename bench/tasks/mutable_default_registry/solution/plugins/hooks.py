"""A plugin is a name plus the ordered list of hooks it runs."""
from dataclasses import dataclass


@dataclass
class Plugin:
    name: str
    hooks: list[str]


def register(name: str, hooks: list[str] | None = None) -> Plugin:
    """Build a plugin from `name`, running any caller-supplied hooks before its own on_load."""
    if hooks is None:
        hooks = []
    hooks.append(f"{name}.on_load")
    return Plugin(name, hooks)
