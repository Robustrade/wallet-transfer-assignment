import pytest


@pytest.fixture(autouse=True)
def _clean_db_before_each_test(clean_db: None) -> None:
    return None
