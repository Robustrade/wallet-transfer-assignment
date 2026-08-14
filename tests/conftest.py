import pytest
from fastapi.testclient import TestClient
from sqlalchemy import text

from app.db import models
from app.db.base import Base, SessionLocal, engine
from app.main import app


@pytest.fixture(scope="session", autouse=True)
def _schema():
    Base.metadata.create_all(engine)
    yield


@pytest.fixture(autouse=True)
def _clean_tables():
    with engine.begin() as conn:
        conn.execute(
            text(
                "TRUNCATE ledger_entries, idempotency_records, transfers, wallets "
                "RESTART IDENTITY CASCADE"
            )
        )
    yield


@pytest.fixture
def client():
    return TestClient(app)


@pytest.fixture
def seed_wallet():
    def _seed(wallet_id: str, balance: int):
        with SessionLocal() as session, session.begin():
            session.add(models.Wallet(id=wallet_id, balance=balance))

    return _seed


@pytest.fixture
def balance_of():
    def _balance(wallet_id: str) -> int:
        with SessionLocal() as session:
            return session.get(models.Wallet, wallet_id).balance

    return _balance
