from sqlalchemy import select
from sqlalchemy.orm import Session

from app.db.models import LedgerEntry
from app.domain.enums import EntryType


def record_double_entry(
    session: Session, transfer_id: str, from_wallet: str, to_wallet: str, amount: int
) -> None:
    session.add_all(
        [
            LedgerEntry(
                transfer_id=transfer_id,
                wallet_id=from_wallet,
                type=EntryType.DEBIT.value,
                amount=amount,
            ),
            LedgerEntry(
                transfer_id=transfer_id,
                wallet_id=to_wallet,
                type=EntryType.CREDIT.value,
                amount=amount,
            ),
        ]
    )
    session.flush()


def for_wallet(session: Session, wallet_id: str) -> list[LedgerEntry]:
    stmt = (
        select(LedgerEntry)
        .where(LedgerEntry.wallet_id == wallet_id)
        .order_by(LedgerEntry.id)
    )
    return list(session.execute(stmt).scalars().all())
