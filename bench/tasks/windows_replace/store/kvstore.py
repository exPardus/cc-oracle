"""A tiny key=value text store, saved atomically."""
from __future__ import annotations

from .atomic import write_atomic


def save_kv(path: str, mapping: dict[str, str]) -> None:
    lines = [f"{k}={v}" for k, v in sorted(mapping.items())]
    write_atomic(path, "\n".join(lines) + ("\n" if lines else ""))


def load_kv(path: str) -> dict[str, str]:
    mapping: dict[str, str] = {}
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.rstrip("\n")
            if not line:
                continue
            key, _, value = line.partition("=")
            mapping[key] = value
    return mapping
