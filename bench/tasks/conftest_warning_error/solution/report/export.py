"""Public export API used by callers outside this package."""
from __future__ import annotations

from .csvout import write_csv


def export(rows: list[list[str]], fmt: str = "csv", sep: str | None = None) -> str:
    """Render `rows` in the requested format.

    Only "csv" is supported today. `sep` overrides the default field
    separator; leave it unset to get a comma.
    """
    if fmt != "csv":
        raise ValueError(f"unsupported export format: {fmt!r}")
    if sep is None:
        return write_csv(rows)
    return write_csv(rows, sep=sep)
