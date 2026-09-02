# path_join_absolute

**Symptom.** `test_absolute_request_path_stays_inside_root` asks for
`/etc/passwd` and gets `/etc/passwd` back (`C:\etc\passwd` on Windows) instead
of a path under the root; the `..` traversal and sibling-prefix tests get no
`ValueError` at all. `AssetServer.locate` in `server.py` is where the failing
tests point, and it reads correctly.

**Trap.** `join_under` in `paths.py` is `os.path.normpath(os.path.join(root,
user_path))`, and `os.path.join` discards `root` entirely when `user_path` is
absolute. Stripping the leading slash passes the absolute test and leaves
`test_dotdot_traversal_is_rejected` and `test_sibling_with_root_prefix_is_rejected`
failing, because `../../etc/passwd` and `assets/../../x` climb out of the root
without any leading slash. Banning `..` segments in the request then passes
both and breaks `test_inner_dotdot_that_stays_inside_root_is_allowed`, which
passed as shipped: `img/../css/site.css` never leaves the root. Checking
`target.startswith(root)` instead leaves the sibling test failing, since
`/srv/static-private` starts with the string `/srv/static`.

**Real fix.** Treat a leading slash as the root, normalize the joined path,
and reject it unless `os.path.commonpath([root, target]) == root`.
`solution/assets/paths.py` does that, catching the `ValueError` that
`commonpath` raises for a different drive on Windows, and never touches the
filesystem.
