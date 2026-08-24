from __future__ import annotations

from collections.abc import Callable
from datetime import UTC, datetime
from uuid import UUID, uuid4

import pytest

from tests.fakes.in_memory_repositories import FakeUnitOfWorkFactory
from wallet_transfer.domain.transfer import InvalidTransferError, TransferStatus
from wallet_transfer.domain.wallet import Wallet
from wallet_transfer.repositories.errors import (
    DeadlockDetectedError,
    LockTimeoutError,
    PoolTimeoutError,
)
from wallet_transfer.services.errors import (
    IdempotencyKeyReusedError,
    RetryExhaustedError,
    WalletNotFoundError,
)
from wallet_transfer.services.transfer_service import CreateTransferRequest, TransferService


async def _no_op_sleep(_seconds: float) -> None:
    return None


def _wallet(balance: int, wallet_id: UUID | None = None) -> Wallet:
    now = datetime.now(UTC)
    return Wallet(id=wallet_id or uuid4(), balance=balance, created_at=now, updated_at=now)


def _request(
    *,
    idempotency_key: str = "key-1",
    from_wallet_id: UUID | None = None,
    to_wallet_id: UUID | None = None,
    amount: int = 100,
) -> CreateTransferRequest:
    return CreateTransferRequest(
        idempotency_key=idempotency_key,
        from_wallet_id=from_wallet_id or uuid4(),
        to_wallet_id=to_wallet_id or uuid4(),
        amount=amount,
    )


def _service(factory: FakeUnitOfWorkFactory) -> TransferService:
    return TransferService(factory, sleep=_no_op_sleep, base_backoff_seconds=0.0)


async def test_st1_cached_replay_skips_wallet_locking() -> None:
    from_wallet = _wallet(1000)
    to_wallet = _wallet(500)
    factory = FakeUnitOfWorkFactory()
    factory.database.wallets[from_wallet.id] = from_wallet
    factory.database.wallets[to_wallet.id] = to_wallet
    service = _service(factory)
    request = _request(from_wallet_id=from_wallet.id, to_wallet_id=to_wallet.id, amount=100)

    original = await service.create_transfer(request)
    assert original.status is TransferStatus.PROCESSED

    # Remove the wallets so a replay that incorrectly re-executes the transfer (instead of
    # returning the cached result) would observably fail with WalletNotFoundError.
    factory.database.wallets.clear()

    replayed = await service.create_transfer(request)

    assert replayed == original


async def test_st2_same_key_different_payload_is_rejected() -> None:
    from_wallet = _wallet(1000)
    to_wallet = _wallet(500)
    factory = FakeUnitOfWorkFactory()
    factory.database.wallets[from_wallet.id] = from_wallet
    factory.database.wallets[to_wallet.id] = to_wallet
    service = _service(factory)
    first = _request(
        idempotency_key="dup-key", from_wallet_id=from_wallet.id, to_wallet_id=to_wallet.id
    )
    await service.create_transfer(first)

    second = _request(
        idempotency_key="dup-key",
        from_wallet_id=from_wallet.id,
        to_wallet_id=to_wallet.id,
        amount=200,
    )

    with pytest.raises(IdempotencyKeyReusedError):
        await service.create_transfer(second)


@pytest.mark.parametrize(
    "error",
    [LockTimeoutError(), DeadlockDetectedError(), PoolTimeoutError()],
    ids=["lock_timeout", "deadlock_detected", "pool_timeout"],
)
async def test_st3_retries_once_after_a_retryable_error(error: Exception) -> None:
    from_wallet = _wallet(1000)
    to_wallet = _wallet(500)
    factory = FakeUnitOfWorkFactory(fail_with=error, attempts_to_fail=1)
    factory.database.wallets[from_wallet.id] = from_wallet
    factory.database.wallets[to_wallet.id] = to_wallet
    service = _service(factory)
    request = _request(from_wallet_id=from_wallet.id, to_wallet_id=to_wallet.id, amount=100)

    result = await service.create_transfer(request)

    assert result.status is TransferStatus.PROCESSED
    assert factory.call_count == 2


async def test_st4_exhausts_retries_and_raises() -> None:
    factory = FakeUnitOfWorkFactory(fail_with=LockTimeoutError(), attempts_to_fail=999)
    service = _service(factory)
    request = _request()

    with pytest.raises(RetryExhaustedError):
        await service.create_transfer(request)

    assert factory.call_count == service.max_attempts


async def test_st5_insufficient_funds_resolves_failed_without_retry() -> None:
    from_wallet = _wallet(50)
    to_wallet = _wallet(0)
    factory = FakeUnitOfWorkFactory()
    factory.database.wallets[from_wallet.id] = from_wallet
    factory.database.wallets[to_wallet.id] = to_wallet
    service = _service(factory)
    request = _request(from_wallet_id=from_wallet.id, to_wallet_id=to_wallet.id, amount=100)

    result = await service.create_transfer(request)

    assert result.status is TransferStatus.FAILED
    assert result.failure_reason == "INSUFFICIENT_FUNDS"
    assert factory.database.ledger_entries == []
    assert factory.call_count == 1


@pytest.mark.parametrize(
    "build_request",
    [
        lambda wallet_id: _request(from_wallet_id=wallet_id, to_wallet_id=wallet_id, amount=100),
        lambda wallet_id: _request(from_wallet_id=wallet_id, to_wallet_id=uuid4(), amount=0),
        lambda wallet_id: _request(from_wallet_id=wallet_id, to_wallet_id=uuid4(), amount=-5),
    ],
    ids=["self-transfer", "zero-amount", "negative-amount"],
)
async def test_st6_pure_validation_failures_touch_no_repository(
    build_request: Callable[[UUID], CreateTransferRequest],
) -> None:
    factory = FakeUnitOfWorkFactory()
    service = _service(factory)
    request = build_request(uuid4())

    with pytest.raises(InvalidTransferError):
        await service.create_transfer(request)

    assert factory.call_count == 0
    assert factory.database.idempotency_records == {}


async def test_st7_nonexistent_wallet_rolls_back_everything() -> None:
    to_wallet = _wallet(500)
    factory = FakeUnitOfWorkFactory()
    factory.database.wallets[to_wallet.id] = to_wallet
    service = _service(factory)
    request = _request(from_wallet_id=uuid4(), to_wallet_id=to_wallet.id, amount=100)

    with pytest.raises(WalletNotFoundError):
        await service.create_transfer(request)

    assert factory.database.idempotency_records == {}
    assert factory.database.transfers == {}
    assert factory.call_count == 1
