import pytest

from accounts.names import normalize
from accounts.registry import Registry

# "Angstrom" spelled with a ring-above A, two different ways a real input
# method can produce it. Both render identically; neither is byte-equal to
# the other, and NFC normalization is what makes them compare equal.
PRECOMPOSED = "Ångstrom"  # U+00C5 LATIN CAPITAL LETTER A WITH RING ABOVE
DECOMPOSED = "Ångstrom"  # "A" + U+030A COMBINING RING ABOVE


def test_ascii_case_insensitive():
    assert normalize("ADA") == normalize("ada") == "ada"


def test_strips_surrounding_whitespace():
    assert normalize("  Ada Lovelace  ") == "ada lovelace"


def test_precomposed_equals_decomposed():
    assert PRECOMPOSED != DECOMPOSED  # sanity: the fixtures really do differ
    assert normalize(PRECOMPOSED) == normalize(DECOMPOSED)


def test_eszett_equals_double_s_uppercase():
    assert normalize("straße") == normalize("STRASSE")


def test_registry_allows_distinct_names():
    reg = Registry()
    reg.add("Alice")
    reg.add("Bob")  # must not raise


def test_registry_rejects_eszett_case_variant():
    reg = Registry()
    reg.add("STRASSE")
    with pytest.raises(ValueError):
        reg.add("straße")


def test_registry_rejects_decomposed_after_precomposed():
    reg = Registry()
    reg.add(PRECOMPOSED)
    with pytest.raises(ValueError):
        reg.add(DECOMPOSED)
