import glob

from store.atomic import write_atomic
from store.kvstore import load_kv, save_kv


def test_creates_new_file_with_content(tmp_path):
    target = tmp_path / "note.txt"
    write_atomic(str(target), "hello world")
    assert target.read_text(encoding="utf-8") == "hello world"
    assert glob.glob(str(tmp_path / "*.tmp")) == []


def test_overwrite_replaces_content_exactly(tmp_path):
    target = tmp_path / "note.txt"
    target.write_text("OLD CONTENT THAT MUST NOT LEAK THROUGH", encoding="utf-8")
    write_atomic(str(target), "new")
    # Exactly the new text: nothing from the old file, no half-written mix
    # of old and new, and no leftover temp file.
    assert target.read_text(encoding="utf-8") == "new"
    assert glob.glob(str(tmp_path / "*.tmp")) == []


def test_overwrite_does_not_delete_target_first(tmp_path, monkeypatch):
    target = tmp_path / "note.txt"
    target.write_text("old", encoding="utf-8")

    def boom(*args, **kwargs):
        raise AssertionError("write_atomic must not remove the target itself")

    monkeypatch.setattr("store.atomic.os.remove", boom)
    write_atomic(str(target), "new")
    assert target.read_text(encoding="utf-8") == "new"


def test_repeated_writes_keep_latest_only(tmp_path):
    target = tmp_path / "note.txt"
    write_atomic(str(target), "first")
    write_atomic(str(target), "second")
    write_atomic(str(target), "third")
    assert target.read_text(encoding="utf-8") == "third"
    assert glob.glob(str(tmp_path / "*.tmp")) == []


def test_kvstore_round_trip_overwrites(tmp_path):
    target = tmp_path / "settings.kv"
    save_kv(str(target), {"a": "1"})
    save_kv(str(target), {"a": "2", "b": "3"})
    assert load_kv(str(target)) == {"a": "2", "b": "3"}
