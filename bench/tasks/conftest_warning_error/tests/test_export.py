import warnings

import pytest

from report.csvout import write_csv
from report.export import export

ROWS = [["id", "name"], ["1", "Ann"], ["2", "Bo"]]


def test_export_default_uses_comma():
    assert export(ROWS) == "id,name\n1,Ann\n2,Bo\n"


def test_export_explicit_comma_matches_default():
    assert export(ROWS, sep=",") == "id,name\n1,Ann\n2,Bo\n"


def test_export_custom_separator():
    assert export(ROWS, sep=";") == "id;name\n1;Ann\n2;Bo\n"


def test_export_rejects_unknown_format():
    with pytest.raises(ValueError):
        export(ROWS, fmt="xml")


def test_deprecated_alias_still_warns():
    # Direct callers of the old parameter name still get a heads-up.
    with pytest.warns(DeprecationWarning):
        text = write_csv(ROWS, delim="|")
    assert text == "id|name\n1|Ann\n2|Bo\n"


def test_deprecated_alias_output_matches_when_warning_captured():
    # A caller that has silenced the warning still gets correct output.
    with warnings.catch_warnings():
        warnings.simplefilter("ignore", DeprecationWarning)
        via_alias = write_csv(ROWS, delim=";")
    via_new_name = write_csv(ROWS, sep=";")
    assert via_alias == via_new_name
