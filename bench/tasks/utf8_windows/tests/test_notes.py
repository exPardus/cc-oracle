from notes.notebook import Notebook, slug

# Latin-1 with a diacritic, an em dash, CJK, and an emoji outside the BMP.
TEXT = "naïve — 日本語 🙂"


def test_note_round_trips_unchanged(tmp_path):
    nb = Notebook(tmp_path)
    nb.add("Greeting", TEXT)
    assert nb.get("Greeting") == TEXT


def test_file_on_disk_is_utf8(tmp_path):
    Notebook(tmp_path).add("Greeting", TEXT)
    raw = (tmp_path / "greeting.txt").read_bytes()
    assert raw.decode("utf-8") == TEXT


def test_note_survives_reopening(tmp_path):
    Notebook(tmp_path).add("Greeting", TEXT)
    assert Notebook(tmp_path).get("Greeting") == TEXT


def test_ascii_note_round_trips(tmp_path):
    nb = Notebook(tmp_path)
    nb.add("Todo", "buy milk")
    assert nb.get("Todo") == "buy milk"


def test_titles_are_slugged(tmp_path):
    nb = Notebook(tmp_path)
    nb.add("Weekly Plan!", "x")
    assert nb.titles() == ["weekly-plan"]
    assert slug("Weekly Plan!") == "weekly-plan"
