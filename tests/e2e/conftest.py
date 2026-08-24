from __future__ import annotations

from collections.abc import Iterator

import pytest
from fastapi.testclient import TestClient

from wallet_transfer.handlers.app import create_app

pytestmark = pytest.mark.usefixtures("clean_db")


@pytest.fixture
def client(postgres_dsn: str) -> Iterator[TestClient]:
    app = create_app(dsn=postgres_dsn)
    with TestClient(app) as test_client:
        yield test_client
