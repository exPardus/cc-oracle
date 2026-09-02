"""Path arithmetic for the asset server. Nothing here touches the filesystem."""
import os


def join_under(root: str, user_path: str) -> str:
    """Filesystem path for `user_path`, a request path relative to `root`.

    A leading slash names the root itself, not the filesystem root, which is
    what `os.path.join` would make of it. `.` and `..` segments are collapsed
    so the result is canonical, and the result must stay under `root`; that
    is decided on the normalized result, because `..` can climb out of the
    root without ever appearing at the start of the request.
    """
    root = os.path.normpath(root)
    target = os.path.normpath(os.path.join(root, user_path.lstrip("/")))
    try:
        inside = os.path.commonpath([root, target]) == root
    except ValueError:  # a different drive on Windows
        inside = False
    if not inside:
        raise ValueError(f"{user_path!r} resolves outside the asset root {root!r}")
    return target
