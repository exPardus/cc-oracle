"""Parsing of the ISO-8601 timestamps the API hands us."""
from datetime import datetime


def parse_timestamp(text: str) -> datetime:
    """Parse '2025-06-01T12:00:00Z' or '2025-06-01T14:00:00+02:00'.

    Always returns an aware datetime. fromisoformat on 3.10 rejects the
    trailing Z, so spell it as the +00:00 offset it stands for rather
    than dropping it, which would silently make the result naive.
    """
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    return datetime.fromisoformat(text)
