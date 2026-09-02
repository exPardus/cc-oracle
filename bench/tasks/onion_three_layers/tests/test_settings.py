from settings.loader import load

DEFAULTS = {
    "debug": True,
    "port": 80,
    "host": "localhost",
    "name": "app",
    "region": "us-east",
}


def test_file_beats_defaults():
    merged = load(DEFAULTS, {"name": "from-file"}, {}, {})
    assert merged["name"] == "from-file"


def test_env_overrides_file():
    merged = load(DEFAULTS, {"host": "file-host"}, {"APP_HOST": "env-host"}, {})
    assert merged["host"] == "env-host"


def test_env_bool_false():
    # File already says False; env agrees but as the raw string "false".
    # If env is applied without coercion, the string overwrites the bool.
    merged = load(DEFAULTS, {"debug": False}, {"APP_DEBUG": "false"}, {})
    assert merged["debug"] is False


def test_env_int():
    # Same idea for an int: file already says 8080, env agrees as "8080".
    merged = load(DEFAULTS, {"port": 8080}, {"APP_PORT": "8080"}, {})
    assert merged["port"] == 8080
    assert isinstance(merged["port"], int)


def test_cli_beats_env():
    merged = load(DEFAULTS, {}, {"APP_REGION": "eu-west"}, {"region": "cli-region"})
    assert merged["region"] == "cli-region"
