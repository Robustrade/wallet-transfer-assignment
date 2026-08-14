from sqlalchemy.orm import Session

from app.db.models import Transfer
from app.domain.enums import TransferStatus


def create_pending(
    session: Session, transfer_id: str, from_wallet: str, to_wallet: str, amount: int
) -> Transfer:
    transfer = Transfer(
        id=transfer_id,
        from_wallet=from_wallet,
        to_wallet=to_wallet,
        amount=amount,
        status=TransferStatus.PENDING.value,
    )
    session.add(transfer)
    session.flush()
    return transfer


def mark(session: Session, transfer: Transfer, status: TransferStatus) -> None:
    """Only PENDING transfers may transition; guards against duplicate processing."""
    if transfer.status != TransferStatus.PENDING.value:
        return
    transfer.status = status.value
    session.flush()


def get(session: Session, transfer_id: str) -> Transfer | None:
    return session.get(Transfer, transfer_id)
