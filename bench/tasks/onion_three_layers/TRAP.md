# onion_three_layers

**Symptom.** As shipped, exactly one test fails:
`test_env_overrides_file` expects an `APP_HOST` environment variable to
beat a file-provided `host`, and gets the file's value instead. The other
four tests - including two that also load `APP_*` variables
(`test_env_bool_false`, `test_env_int`) and one that pits `cli` against
`env` (`test_cli_beats_env`) - all pass. It reads like a single, narrow
bug: environment overrides just aren't reaching the merge.

**Trap.** `settings/loader.py` has three bugs stacked so that fixing the
visible one un-masks the other two. First, the env lookup builds
`env_key = key.upper()` instead of `ENV_PREFIX + key.upper()`, so it never
finds anything in a real `APP_`-prefixed env dict - env is a no-op, full
stop. Second, `merged[key] = env[env_key]` never calls `coerce()` from
`settings/coerce.py` - it assigns the raw string. Third,
`merged.update(cli)` runs *before* the env loop, so if env ever did apply,
it would land on top of `cli` instead of under it. Bugs two and three are
invisible as shipped because bug one keeps env from ever touching
`merged` at all: `test_env_bool_false` and `test_env_int` pass because the
file layer already supplies the correctly-typed value, and
`test_cli_beats_env` passes because `cli`'s value is simply never
overwritten by anything. Fixing only the obvious thing - add
`ENV_PREFIX` to `env_key` (verified in a temp copy) - turns
`test_env_overrides_file` green and immediately turns three previously
green tests red: `test_env_bool_false` and `test_env_int` now fail because
env's raw strings (`"false"`, `"8080"`) land uncoerced and don't equal the
booleans/ints the tests check for, and `test_cli_beats_env` now fails
because env, applied after `cli`, overwrites it. It looks like the prefix
fix broke three things - the natural reaction is to revert it - but the
prefix fix is correct and necessary; it just stopped hiding two other,
pre-existing bugs that were always going to need fixing too.

**Real fix.** All three: prefix the env lookup with `ENV_PREFIX`, run
`coerce(env[env_key], default)` (from `settings/coerce.py`) instead of
assigning the raw string, and move `merged.update(cli)` to *after* the env
loop so cli has final say. `solution/settings/loader.py` does all three;
`test_file_beats_defaults` passes throughout and is there to confirm the
one always-correct layer stays correct.
