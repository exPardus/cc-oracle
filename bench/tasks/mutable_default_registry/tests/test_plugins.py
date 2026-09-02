from plugins.hooks import register
from plugins.registry import Registry


def test_registries_do_not_share_hooks():
    a, b = Registry(), Registry()
    a.install("alpha")
    b.install("beta")
    assert b.hooks_for("beta") == ["beta.on_load"]


def test_fresh_registry_starts_empty():
    a = Registry()
    a.install("alpha")
    assert Registry().names() == []


def test_two_register_calls_do_not_share_hooks():
    first = register("alpha")
    second = register("beta")
    assert first.hooks == ["alpha.on_load"]
    assert second.hooks == ["beta.on_load"]


def test_explicit_hooks_run_before_on_load():
    plugin = register("alpha", ["audit.start"])
    assert plugin.hooks == ["audit.start", "alpha.on_load"]


def test_registry_accepts_initial_items():
    seeded = Registry({"alpha": register("alpha", [])})
    assert seeded.names() == ["alpha"]
    assert seeded.hooks_for("alpha") == ["alpha.on_load"]
