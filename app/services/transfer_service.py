import hashlib
import json
import uuid
from dataclasses import dataclass

from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from app.domain.enums import IdempotencyStatus, TransferStatus
from app.domain.errors import IdempotencyConflict, TransferInProgress, WalletNotFound
from app.repositories import idempotency, ledger, transfer, wallet


@dataclass
class TransferOutcome:
    transfer_id: str
    status: TransferStatus
    from_wallet_id: str
    to_wallet_id: str
    amount: int
    replayed: bool = False
    failure_code: str | None = None


def create_transfer(
    session: Session, from_wallet_id: str, to_wallet_id: str, amount: int, idempotency_key: str
) -> TransferOutcome:
    request_hash = _hash(from_wallet_id, to_wallet_id, amount)

    with session.begin():
        existing = idempotency.get(session, idempotency_key)
        if existing is not None:
            return _replay(existing, request_hash)

        try:
            with session.begin_nested():
                record = idempotency.insert_pending(session, idempotency_key, request_hash)
        except IntegrityError:
            # A concurrent request reserved the key first; return its result.
            existing = idempotency.get(session, idempotency_key)
            return _replay(existing, request_hash)

        outcome = _execute(session, from_wallet_id, to_wallet_id, amount)
        idempotency.complete(session, record, outcome.transfer_id, _body(outcome))
        return outcome


def _execute(
    session: Session, from_wallet_id: str, to_wallet_id: str, amount: int
) -> TransferOutcome:
    wallets = wallet.lock_wallets(session, (from_wallet_id, to_wallet_id))
    if from_wallet_id not in wallets or to_wallet_id not in wallets:
        raise WalletNotFound()

    source = wallets[from_wallet_id]
    destination = wallets[to_wallet_id]

    transfer_id = str(uuid.uuid4())
    row = transfer.create_pending(session, transfer_id, from_wallet_id, to_wallet_id, amount)

    if source.balance < amount:
        transfer.mark(session, row, TransferStatus.FAILED)
        return TransferOutcome(
            transfer_id=transfer_id,
            status=TransferStatus.FAILED,
            from_wallet_id=from_wallet_id,
            to_wallet_id=to_wallet_id,
            amount=amount,
            failure_code="INSUFFICIENT_FUNDS",
        )

    source.balance -= amount
    destination.balance += amount
    ledger.record_double_entry(session, transfer_id, from_wallet_id, to_wallet_id, amount)
    transfer.mark(session, row, TransferStatus.PROCESSED)

    return TransferOutcome(
        transfer_id=transfer_id,
        status=TransferStatus.PROCESSED,
        from_wallet_id=from_wallet_id,
        to_wallet_id=to_wallet_id,
        amount=amount,
    )


def _replay(record, request_hash: str) -> TransferOutcome:
    if record.request_hash != request_hash:
        raise IdempotencyConflict()
    if record.status != IdempotencyStatus.COMPLETED.value:
        raise TransferInProgress()

    body = record.response_body
    return TransferOutcome(
        transfer_id=body["transferId"],
        status=TransferStatus(body["status"]),
        from_wallet_id=body["fromWalletId"],
        to_wallet_id=body["toWalletId"],
        amount=body["amount"],
        replayed=True,
        failure_code=body.get("code"),
    )


def _hash(from_wallet_id: str, to_wallet_id: str, amount: int) -> str:
    payload = json.dumps(
        {"from": from_wallet_id, "to": to_wallet_id, "amount": amount},
        sort_keys=True,
        separators=(",", ":"),
    )
    return hashlib.sha256(payload.encode()).hexdigest()


def _body(outcome: TransferOutcome) -> dict:
    body = {
        "transferId": outcome.transfer_id,
        "status": outcome.status.value,
        "fromWalletId": outcome.from_wallet_id,
        "toWalletId": outcome.to_wallet_id,
        "amount": outcome.amount,
    }
    if outcome.failure_code:
        body["code"] = outcome.failure_code
    return body
