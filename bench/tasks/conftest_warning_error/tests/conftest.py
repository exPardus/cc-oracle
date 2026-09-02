"""This suite treats deprecation warnings as bugs, not noise: any
DeprecationWarning raised while a test runs fails that test, the same way
CI is configured for this project. Do not remove or weaken this."""


def pytest_configure(config):
    config.addinivalue_line("filterwarnings", "error::DeprecationWarning")
