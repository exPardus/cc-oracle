"""Layered settings: defaults < file < env < cli (cli wins ties).

Environment variables are prefixed `APP_` (e.g. `APP_PORT` for the `port`
key) and are coerced to the type of the matching default before merging.
"""
from .coerce import coerce

ENV_PREFIX = "APP_"


def load(defaults: dict, file_values: dict, env: dict, cli: dict) -> dict:
    merged = dict(defaults)
    merged.update(file_values)

    for key, default in defaults.items():
        env_key = ENV_PREFIX + key.upper()
        if env_key in env:
            merged[key] = coerce(env[env_key], default)

    merged.update(cli)

    return merged
