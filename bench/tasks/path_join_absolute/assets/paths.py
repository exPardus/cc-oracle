"""Path arithmetic for the asset server. Nothing here touches the filesystem."""
import os


def join_under(root: str, user_path: str) -> str:
    """Filesystem path for `user_path`, a request path relative to `root`.

    `.` and `..` segments are collapsed so the result is canonical.
    """
    return os.path.normpath(os.path.join(root, user_path))
