"""Write text to a file without ever leaving a half-written file in place."""
from __future__ import annotations

import os


def write_atomic(path: str, text: str) -> None:
    """Write `text` to `path`, replacing any existing file only once the
    new content is fully on disk.

    The new content goes to a temporary file beside the target first, so a
    crash mid-write never leaves `path` holding half of the new content.
    """
    tmp = f"{path}.tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        f.write(text)
        os.rename(tmp, path)
