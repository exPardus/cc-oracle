import os

import pytest

from assets.server import AssetServer

ROOT = os.path.abspath("/srv/static")


def test_absolute_request_path_stays_inside_root():
    # A request for /etc/passwd is served from under the root, never from the filesystem root.
    assert AssetServer(ROOT).locate("/etc/passwd") == os.path.join(ROOT, "etc", "passwd")


def test_dotdot_traversal_is_rejected():
    server = AssetServer(ROOT)
    for request in ["../../etc/passwd", "assets/../../x"]:
        with pytest.raises(ValueError):
            server.locate(request)


def test_relative_path_resolves_inside_root():
    assert AssetServer(ROOT).locate("img/logo.png") == os.path.join(ROOT, "img", "logo.png")


def test_root_itself_is_allowed():
    server = AssetServer(ROOT)
    assert server.locate("") == ROOT
    assert server.locate(".") == ROOT


def test_inner_dotdot_that_stays_inside_root_is_allowed():
    # img/../css/site.css never leaves the root; it is css/site.css.
    assert AssetServer(ROOT).locate("img/../css/site.css") == os.path.join(ROOT, "css", "site.css")


def test_sibling_with_root_prefix_is_rejected():
    # /srv/static-private starts with the string /srv/static but is not under it.
    with pytest.raises(ValueError):
        AssetServer(ROOT).locate("../static-private/keys")
