from ui.screen import Screen

ITEMS = ["alpha", "beta", "gamma"]


def test_first_callback_selects_first_item():
    screen = Screen(ITEMS)
    screen.callbacks[0]()
    assert screen.selected == "alpha"


def test_each_callback_selects_its_own_item():
    screen = Screen(ITEMS)
    for i, callback in enumerate(screen.callbacks):
        callback()
        assert screen.selected == ITEMS[i]


def test_dispatcher_routes_each_name_to_its_own_handler():
    screen = Screen(ITEMS)
    assert screen.run("open", "a.txt") == "open"
    assert screen.run("save", "b.txt") == "save"
    assert screen.log == [("open", "a.txt"), ("save", "b.txt")]


def test_one_callback_per_item():
    screen = Screen(ITEMS)
    assert len(screen.callbacks) == len(ITEMS)
