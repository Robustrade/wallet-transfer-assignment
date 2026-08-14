from sqlalchemy.orm import Session

from app.db.models import IdempotencyRecord
from app.domain.enums import IdempotencyStatus


def get(session: Session, key: str) -> IdempotencyRecord | None:
    return session.get(IdempotencyRecord, key)


def insert_pending(session: Session, key: str, request_hash: str) -> IdempotencyRecord:
    """Insert the reservation. Raises IntegrityError if the key already exists."""
    record = IdempotencyRecord(
        idempotency_key=key,
        request_hash=request_hash,
        status=IdempotencyStatus.IN_PROGRESS.value,
    )
    session.add(record)
    session.flush()
    return record


def complete(
    session: Session, record: IdempotencyRecord, transfer_id: str, response_body: dict
) -> None:
    record.transfer_id = transfer_id
    record.response_body = response_body
    record.status = IdempotencyStatus.COMPLETED.value
    session.flush()
