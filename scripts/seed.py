"""Seed a couple of wallets for local testing."""
from app.db.base import SessionLocal
from app.db.models import Wallet


def seed() -> None:
    with SessionLocal() as session, session.begin():
        for wallet_id, balance in [("wallet_1", 1000), ("wallet_2", 0)]:
            if session.get(Wallet, wallet_id) is None:
                session.add(Wallet(id=wallet_id, balance=balance))
    print("Seeded wallet_1 (1000) and wallet_2 (0)")


if __name__ == "__main__":
    seed()
