from __future__ import annotations

import asyncio

import asyncpg

from tests.repository.helpers import fetch_wallet, seed_wallet
from wallet_transfer.domain.transfer import TransferStatus
from wallet_transfer.repositories.unit_of_work_asyncpg import AsyncpgUnitOfWorkFactory
from wallet_transfer.services.transfer_service import CreateTransferRequest, TransferService


async def test_lt1_processed_transfer_has_exactly_two_matching_ledger_entries(
    pool: asyncpg.Pool, uow_factory: AsyncpgUnitOfWorkFactory
) -> None:
    from_wallet = await seed_wallet(pool, balance=1000)
    to_wallet = await seed_wallet(pool, balance=0)
    service = TransferService(uow_factory)
    request = CreateTransferRequest(
        idempotency_key="lt1", from_wallet_id=from_wallet.id, to_wallet_id=to_wallet.id, amount=250
    )

    result = await service.create_transfer(request)

    assert result.status is TransferStatus.PROCESSED
    async with pool.acquire() as connection:
        rows = await connection.fetch(
            "SELECT type, amount, wallet_id FROM ledger_entries WHERE transfer_id = $1", result.id
        )
    assert len(rows) == 2
    by_type = {row["type"]: row for row in rows}
    assert set(by_type) == {"CREDIT", "DEBIT"}
    assert by_type["DEBIT"]["amount"] == 250
    assert by_type["CREDIT"]["amount"] == 250
    assert by_type["DEBIT"]["wallet_id"] == from_wallet.id
    assert by_type["CREDIT"]["wallet_id"] == to_wallet.id


async def test_lt2_failed_transfer_has_zero_ledger_entries(
    pool: asyncpg.Pool, uow_factory: AsyncpgUnitOfWorkFactory
) -> None:
    from_wallet = await seed_wallet(pool, balance=10)
    to_wallet = await seed_wallet(pool, balance=0)
    service = TransferService(uow_factory)
    request = CreateTransferRequest(
        idempotency_key="lt2", from_wallet_id=from_wallet.id, to_wallet_id=to_wallet.id, amount=250
    )

    result = await service.create_transfer(request)

    assert result.status is TransferStatus.FAILED
    async with pool.acquire() as connection:
        count = await connection.fetchval(
            "SELECT count(*) FROM ledger_entries WHERE transfer_id = $1", result.id
        )
    assert count == 0


async def test_lt3_and_lt4_ledger_and_balances_reconcile_after_concurrent_batch(
    pool: asyncpg.Pool, uow_factory: AsyncpgUnitOfWorkFactory
) -> None:
    # A mixed concurrent batch — CT1-style fan-in (some affordable, some not) plus a CT2-style
    # opposite-direction pair, all at once — proves LT3/LT4 hold after a real batch of
    # concurrency scenarios (ADR-0006), not just a single isolated transfer.
    hub = await seed_wallet(pool, balance=250)
    fan_in_targets = [await seed_wallet(pool, balance=0) for _ in range(5)]
    pair_a = await seed_wallet(pool, balance=500)
    pair_b = await seed_wallet(pool, balance=500)
    all_wallets = [hub, *fan_in_targets, pair_a, pair_b]
    initial_balances = {wallet.id: wallet.balance for wallet in all_wallets}

    service = TransferService(uow_factory)
    fan_in_requests = [
        CreateTransferRequest(
            idempotency_key=f"lt-fan-{i}", from_wallet_id=hub.id, to_wallet_id=target.id, amount=100
        )
        for i, target in enumerate(fan_in_targets)
    ]
    pair_requests = [
        CreateTransferRequest(
            idempotency_key="lt-pair-ab",
            from_wallet_id=pair_a.id,
            to_wallet_id=pair_b.id,
            amount=150,
        ),
        CreateTransferRequest(
            idempotency_key="lt-pair-ba",
            from_wallet_id=pair_b.id,
            to_wallet_id=pair_a.id,
            amount=75,
        ),
    ]

    await asyncio.gather(*(service.create_transfer(r) for r in [*fan_in_requests, *pair_requests]))

    async with pool.acquire() as connection:
        totals = await connection.fetchrow(
            """
            SELECT
                COALESCE(SUM(amount) FILTER (WHERE type = 'DEBIT'), 0) AS debit,
                COALESCE(SUM(amount) FILTER (WHERE type = 'CREDIT'), 0) AS credit
            FROM ledger_entries
            """
        )
    assert totals is not None
    assert totals["debit"] == totals["credit"]

    for wallet in all_wallets:
        async with pool.acquire() as connection:
            credit_sum = await connection.fetchval(
                "SELECT COALESCE(SUM(amount), 0) FROM ledger_entries "
                "WHERE wallet_id = $1 AND type = 'CREDIT'",
                wallet.id,
            )
            debit_sum = await connection.fetchval(
                "SELECT COALESCE(SUM(amount), 0) FROM ledger_entries "
                "WHERE wallet_id = $1 AND type = 'DEBIT'",
                wallet.id,
            )
        final = await fetch_wallet(pool, wallet.id)
        assert final.balance == initial_balances[wallet.id] + credit_sum - debit_sum
