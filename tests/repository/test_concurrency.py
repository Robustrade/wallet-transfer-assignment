from __future__ import annotations

import asyncio

import asyncpg
import pytest

from tests.repository.helpers import (
    count_ledger_entries,
    count_transfers_for_key,
    fetch_wallet,
    seed_wallet,
)
from wallet_transfer.domain.transfer import Transfer, TransferStatus
from wallet_transfer.repositories.unit_of_work_asyncpg import AsyncpgUnitOfWorkFactory
from wallet_transfer.services.errors import IdempotencyKeyReusedError
from wallet_transfer.services.transfer_service import CreateTransferRequest, TransferService


@pytest.mark.parametrize("fan_in", [2, 5, 20])
async def test_ct1_concurrent_debits_exceeding_balance_affordable_ones_win(
    pool: asyncpg.Pool, uow_factory: AsyncpgUnitOfWorkFactory, fan_in: int
) -> None:
    # Only the source wallet is shared — a distinct destination per request keeps the contention
    # scoped to what CT1 actually tests (concurrent debits on one wallet), rather than also
    # serializing on a destination none of the ADRs ask this scenario to share. 50-100 concurrent
    # attempts is scripts/simulate.py's job (ADR-0003's CT6), not this automated tier.
    amount = 100
    affordable = fan_in // 2
    initial_balance = affordable * amount
    from_wallet = await seed_wallet(pool, balance=initial_balance)
    to_wallets = [await seed_wallet(pool, balance=0) for _ in range(fan_in)]
    service = TransferService(uow_factory)

    requests = [
        CreateTransferRequest(
            idempotency_key=f"ct1-{fan_in}-{i}",
            from_wallet_id=from_wallet.id,
            to_wallet_id=to_wallets[i].id,
            amount=amount,
        )
        for i in range(fan_in)
    ]

    results = await asyncio.gather(*(service.create_transfer(r) for r in requests))

    processed = [r for r in results if r.status is TransferStatus.PROCESSED]
    failed = [r for r in results if r.status is TransferStatus.FAILED]
    assert len(processed) == affordable
    assert len(failed) == fan_in - affordable

    final_from = await fetch_wallet(pool, from_wallet.id)
    assert final_from.balance == initial_balance - affordable * amount
    assert final_from.balance >= 0

    credited_balances = [(await fetch_wallet(pool, w.id)).balance for w in to_wallets]
    assert sum(credited_balances) == affordable * amount


async def test_ct2_opposite_direction_transfers_both_complete_without_deadlock(
    pool: asyncpg.Pool, uow_factory: AsyncpgUnitOfWorkFactory
) -> None:
    wallet_a = await seed_wallet(pool, balance=1000)
    wallet_b = await seed_wallet(pool, balance=1000)
    service = TransferService(uow_factory)

    request_ab = CreateTransferRequest(
        idempotency_key="ct2-ab", from_wallet_id=wallet_a.id, to_wallet_id=wallet_b.id, amount=100
    )
    request_ba = CreateTransferRequest(
        idempotency_key="ct2-ba", from_wallet_id=wallet_b.id, to_wallet_id=wallet_a.id, amount=50
    )

    # A genuine deadlock takes at least Postgres's ~1s deadlock_timeout to detect and break,
    # plus our own retry backoff, before it would resolve — bounding this well under that makes
    # the timeout itself part of the proof that the ascending lock order avoided one.
    result_ab, result_ba = await asyncio.wait_for(
        asyncio.gather(service.create_transfer(request_ab), service.create_transfer(request_ba)),
        timeout=3.0,
    )

    assert result_ab.status is TransferStatus.PROCESSED
    assert result_ba.status is TransferStatus.PROCESSED

    final_a = await fetch_wallet(pool, wallet_a.id)
    final_b = await fetch_wallet(pool, wallet_b.id)
    assert final_a.balance == 1000 - 100 + 50
    assert final_b.balance == 1000 - 50 + 100


async def test_ct3_contender_retries_after_lock_release(pool: asyncpg.Pool) -> None:
    from_wallet = await seed_wallet(pool, balance=1000)
    to_wallet = await seed_wallet(pool, balance=0)

    holder_connection = await pool.acquire()
    holder_tx = holder_connection.transaction()
    await holder_tx.start()
    await holder_connection.fetchrow(
        "SELECT * FROM wallets WHERE id = $1 FOR UPDATE", from_wallet.id
    )

    async def release_holder_after_delay() -> None:
        await asyncio.sleep(0.35)
        await holder_tx.commit()
        await pool.release(holder_connection)

    factory = AsyncpgUnitOfWorkFactory(pool, lock_timeout_ms=300)
    service = TransferService(factory, base_backoff_seconds=0.05)
    request = CreateTransferRequest(
        idempotency_key="ct3", from_wallet_id=from_wallet.id, to_wallet_id=to_wallet.id, amount=100
    )

    result, _ = await asyncio.wait_for(
        asyncio.gather(service.create_transfer(request), release_holder_after_delay()), timeout=10.0
    )

    assert result.status is TransferStatus.PROCESSED
    final_from = await fetch_wallet(pool, from_wallet.id)
    assert final_from.balance == 900


async def test_ct4_concurrent_identical_key_creates_exactly_one_transfer(
    pool: asyncpg.Pool, uow_factory: AsyncpgUnitOfWorkFactory
) -> None:
    from_wallet = await seed_wallet(pool, balance=1000)
    to_wallet = await seed_wallet(pool, balance=0)
    service = TransferService(uow_factory)
    request = CreateTransferRequest(
        idempotency_key="ct4", from_wallet_id=from_wallet.id, to_wallet_id=to_wallet.id, amount=100
    )

    result_a, result_b = await asyncio.gather(
        service.create_transfer(request), service.create_transfer(request)
    )

    assert result_a == result_b
    assert result_a.status is TransferStatus.PROCESSED
    assert await count_transfers_for_key(pool, "ct4") == 1
    assert await count_ledger_entries(pool, result_a.id) == 2


async def test_ct5_concurrent_same_key_different_payload_loser_gets_409(
    pool: asyncpg.Pool, uow_factory: AsyncpgUnitOfWorkFactory
) -> None:
    from_wallet = await seed_wallet(pool, balance=1000)
    to_wallet = await seed_wallet(pool, balance=0)
    service = TransferService(uow_factory)
    request_a = CreateTransferRequest(
        idempotency_key="ct5", from_wallet_id=from_wallet.id, to_wallet_id=to_wallet.id, amount=100
    )
    request_b = CreateTransferRequest(
        idempotency_key="ct5", from_wallet_id=from_wallet.id, to_wallet_id=to_wallet.id, amount=200
    )

    results = await asyncio.gather(
        service.create_transfer(request_a),
        service.create_transfer(request_b),
        return_exceptions=True,
    )

    successes = [r for r in results if isinstance(r, Transfer)]
    conflicts = [r for r in results if isinstance(r, IdempotencyKeyReusedError)]
    assert len(successes) == 1
    assert len(conflicts) == 1
    assert await count_transfers_for_key(pool, "ct5") == 1


async def test_ct7_client_retry_lands_between_original_internal_attempts(
    pool: asyncpg.Pool,
) -> None:
    from_wallet = await seed_wallet(pool, balance=1000)
    to_wallet = await seed_wallet(pool, balance=0)

    holder_connection = await pool.acquire()
    holder_tx = holder_connection.transaction()
    await holder_tx.start()
    await holder_connection.fetchrow(
        "SELECT * FROM wallets WHERE id = $1 FOR UPDATE", from_wallet.id
    )

    factory = AsyncpgUnitOfWorkFactory(pool, lock_timeout_ms=300)
    request = CreateTransferRequest(
        idempotency_key="ct7", from_wallet_id=from_wallet.id, to_wallet_id=to_wallet.id, amount=100
    )
    injected_task: asyncio.Task[Transfer] | None = None

    async def on_retry(attempt: int, error: Exception) -> None:
        nonlocal injected_task
        if attempt == 1:
            # Deterministic synchronization point: the client-initiated retry is started right
            # here, in the gap after the original's first attempt rolled back — not timed via a
            # guessed sleep duration (ADR-0003/CT7).
            injected_service = TransferService(factory, base_backoff_seconds=0.05)
            injected_task = asyncio.create_task(injected_service.create_transfer(request))
            await asyncio.sleep(0.1)  # let the injected retry reach its own idempotency insert
            await holder_tx.commit()
            await pool.release(holder_connection)

    service = TransferService(factory, base_backoff_seconds=0.05, on_retry=on_retry)

    original_result = await asyncio.wait_for(service.create_transfer(request), timeout=10.0)

    assert injected_task is not None
    injected_result = await asyncio.wait_for(injected_task, timeout=10.0)

    assert original_result == injected_result
    assert await count_transfers_for_key(pool, "ct7") == 1
    assert await count_ledger_entries(pool, original_result.id) == 2
