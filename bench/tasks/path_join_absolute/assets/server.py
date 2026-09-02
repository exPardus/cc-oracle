"""Maps request paths onto files under one static root. Nothing is read here."""
import os

from .paths import join_under


class AssetServer:
    def __init__(self, root: str) -> None:
        self.root = os.path.abspath(root)

    def locate(self, request_path: str) -> str:
        """Filesystem path a request for `request_path` would be served from.

        Only paths under the root are served; anything else is a ValueError.
        """
        return join_under(self.root, request_path)
