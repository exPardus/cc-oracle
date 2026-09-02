"""Coerce a raw environment-variable string to the type of a default value.

Environment variables only ever arrive as strings; this maps them back to
whatever type the matching default declared, so callers of `load()` don't
have to care whether a setting came from the file layer or the env layer.
"""


def coerce(raw: str, default: object) -> object:
    """Return `raw` converted to `type(default)`.

    `bool` is checked before `int` because `bool` is a subclass of `int` in
    Python - `isinstance(True, int)` is true, so the order matters.
    """
    if isinstance(default, bool):
        return raw.strip().lower() in ("1", "true", "yes", "on")
    if isinstance(default, int):
        return int(raw)
    if isinstance(default, float):
        return float(raw)
    return raw
