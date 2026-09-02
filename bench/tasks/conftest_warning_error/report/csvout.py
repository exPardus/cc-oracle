"""Low-level delimited-text writer."""
from __future__ import annotations

import warnings


def write_csv(rows: list[list[str]], sep: str = ",", delim: str | None = None) -> str:
    """Render `rows` as text, one row per line, fields joined by `sep`.

    `delim` is a deprecated alias for `sep`, kept only for callers that
    have not migrated yet; passing it emits a DeprecationWarning.
    """
    if delim is not None:
        warnings.warn(
            "write_csv(delim=...) is deprecated, use write_csv(sep=...) instead",
            DeprecationWarning,
            stacklevel=2,
        )
        sep = delim
    return "\n".join(sep.join(str(cell) for cell in row) for row in rows) + ("\n" if rows else "")
