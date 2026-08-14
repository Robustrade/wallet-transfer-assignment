from collections.abc import Iterable

from sqlalchemy import select
from sqlalchemy.orm import Session

from app.db.models import Wallet


def lock_wallets(session: Session, wallet_ids: Iterable[str]) -> dict[str, Wallet]:
    """Lock the given wallets FOR UPDATE, ordered by id to avoid deadlocks."""
    stmt = (
        select(Wallet)
        .where(Wallet.id.in_(set(wallet_ids)))
        .order_by(Wallet.id)
        .with_for_update()
    )
    return {w.id: w for w in session.execute(stmt).scalars().all()}


def get(session: Session, wallet_id: str) -> Wallet | None:
    return session.get(Wallet, wallet_id)
