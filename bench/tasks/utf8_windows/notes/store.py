"""One text file per note under a root directory."""
from pathlib import Path


class NoteStore:
    def __init__(self, root: Path) -> None:
        self.root = Path(root)
        self.root.mkdir(parents=True, exist_ok=True)

    def path_for(self, name: str) -> Path:
        return self.root / f"{name}.txt"

    def write(self, name: str, text: str) -> None:
        with open(self.path_for(name), "w") as f:
            f.write(text)

    def read(self, name: str) -> str:
        with open(self.path_for(name)) as f:
            return f.read()

    def names(self) -> list[str]:
        return sorted(p.stem for p in self.root.glob("*.txt"))
