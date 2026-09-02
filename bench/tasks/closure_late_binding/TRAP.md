# closure_late_binding

**Symptom.** `test_first_callback_selects_first_item` calls the first
callback and finds `"gamma"`, the last item, selected;
`test_each_callback_selects_its_own_item` and
`test_dispatcher_routes_each_name_to_its_own_handler` fail the same way. The
test imports only `screen.py`, whose `select` and `handle` are correct.

**Trap.** `menu.build_callbacks` appends `lambda: select(item)` inside a `for`,
so every lambda looks `item` up after the loop has finished. Binding it there
(`lambda item=item: ...`) fixes both menu tests, and the run then shows
`test_dispatcher_routes_each_name_to_its_own_handler` still failing:
`dispatch.build_handlers` has the same `lambda payload: handle(name, payload)`
in its own loop, in its own file, and every command routes to `"quit"`.

**Real fix.** Bind the loop variable at definition time in both places.
`solution/ui/menu.py` uses a default argument and `solution/ui/dispatch.py`
uses `functools.partial(handle, name)`; either form works in either file.
