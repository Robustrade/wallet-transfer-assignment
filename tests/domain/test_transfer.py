from uuid import uuid4

import pytest

from wallet_transfer.domain.transfer import (
    InvalidTransferError,
    InvalidTransferTransitionError,
    Transfer,
    TransferStatus,
)


def _new_transfer(amount: int = 100) -> Transfer:
    return Transfer.create(
        id=uuid4(),
        from_wallet_id=uuid4(),
        to_wallet_id=uuid4(),
        amount=amount,
    )


def test_create_is_pending() -> None:
    transfer = _new_transfer()

    assert transfer.status is TransferStatus.PENDING
    assert transfer.failure_reason is None


def test_create_rejects_self_transfer() -> None:
    wallet_id = uuid4()

    with pytest.raises(InvalidTransferError):
        Transfer.create(id=uuid4(), from_wallet_id=wallet_id, to_wallet_id=wallet_id, amount=100)


@pytest.mark.parametrize("amount", [0, -1, -100])
def test_create_rejects_non_positive_amount(amount: int) -> None:
    with pytest.raises(InvalidTransferError):
        Transfer.create(id=uuid4(), from_wallet_id=uuid4(), to_wallet_id=uuid4(), amount=amount)


def test_mark_processed_from_pending_succeeds() -> None:
    transfer = _new_transfer().mark_processed()

    assert transfer.status is TransferStatus.PROCESSED
    assert transfer.failure_reason is None


def test_mark_failed_from_pending_succeeds() -> None:
    transfer = _new_transfer().mark_failed("INSUFFICIENT_FUNDS")

    assert transfer.status is TransferStatus.FAILED
    assert transfer.failure_reason == "INSUFFICIENT_FUNDS"


def test_mark_failed_requires_a_reason() -> None:
    with pytest.raises(InvalidTransferError):
        _new_transfer().mark_failed("")


@pytest.mark.parametrize("terminal_transition", ["mark_processed", "mark_failed"])
def test_mark_processed_on_already_processed_transfer_is_rejected(terminal_transition: str) -> None:
    processed = _new_transfer().mark_processed()

    with pytest.raises(InvalidTransferTransitionError):
        _apply(processed, terminal_transition)


@pytest.mark.parametrize("terminal_transition", ["mark_processed", "mark_failed"])
def test_mark_failed_on_already_failed_transfer_is_rejected(terminal_transition: str) -> None:
    failed = _new_transfer().mark_failed("INSUFFICIENT_FUNDS")

    with pytest.raises(InvalidTransferTransitionError):
        _apply(failed, terminal_transition)


def _apply(transfer: Transfer, transition: str) -> Transfer:
    if transition == "mark_processed":
        return transfer.mark_processed()
    return transfer.mark_failed("SOME_REASON")
