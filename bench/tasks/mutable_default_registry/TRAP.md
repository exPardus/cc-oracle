# mutable_default_registry

**Symptom.** `test_registries_do_not_share_hooks` installs `alpha` into one
registry and `beta` into another and finds `beta` running
`['alpha.on_load', 'beta.on_load']`; `test_fresh_registry_starts_empty`
finds a new registry already holding `alpha`. Both tests talk to `Registry`,
which is where the first look goes.

**Trap.** There are two mutable defaults, one per file. `register(name,
hooks=[])` in `hooks.py` appends every plugin's on_load to the same list,
which is what the symptom test sees; `Registry.__init__(self, items={})` in
`registry.py` hands every registry the same dict. Fixing only `hooks.py`
leaves `test_fresh_registry_starts_empty` failing. Fixing only `registry.py`
leaves the symptom test and `test_two_register_calls_do_not_share_hooks`
failing. Patching the call site instead, `register(name, list(hooks or []))`
in `Registry.install`, clears the symptom test and still fails
`test_fresh_registry_starts_empty` and
`test_two_register_calls_do_not_share_hooks`.

**Real fix.** A `None` sentinel in both places, so each call and each
registry starts from a fresh container. `solution/plugins/hooks.py` and
`solution/plugins/registry.py` do that; lists and dicts passed explicitly
are still used as given.
