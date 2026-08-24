from __future__ import annotations

from uuid import UUID, uuid4

import asyncpg

from wallet_transfer.domain.wallet import Wallet


async def seed_wallet(pool: asyncpg.Pool, *, balance: int, wallet_id: UUID | None = None) -> Wallet:
    wallet_id = wallet_id or uuid4()
    async with pool.acquire() as connection:
        row = await connection.fetchrow(
            """
            INSERT INTO wallets (id, balance) VALUES ($1, $2)
            RETURNING id, balance, created_at, updated_at
            """,
            wallet_id,
            balance,
        )
    assert row is not None
    return Wallet(
        id=row["id"],
        balance=row["balance"],
        created_at=row["created_at"],
        updated_at=row["updated_at"],
    )


async def fetch_wallet(pool: asyncpg.Pool, wallet_id: UUID) -> Wallet:
    async with pool.acquire() as connection:
        row = await connection.fetchrow(
            "SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1", wallet_id
        )
    assert row is not None
    return Wallet(
        id=row["id"],
        balance=row["balance"],
        created_at=row["created_at"],
        updated_at=row["updated_at"],
    )


async def count_transfers_for_key(pool: asyncpg.Pool, idempotency_key: str) -> int:
    async with pool.acquire() as connection:
        count = await connection.fetchval(
            """
            SELECT count(*) FROM transfers t
            JOIN idempotency_records ir ON ir.transfer_id = t.id
            WHERE ir.idempotency_key = $1
            """,
            idempotency_key,
        )
    assert isinstance(count, int)
    return count


async def count_ledger_entries(pool: asyncpg.Pool, transfer_id: UUID) -> int:
    async with pool.acquire() as connection:
        count = await connection.fetchval(
            "SELECT count(*) FROM ledger_entries WHERE transfer_id = $1", transfer_id
        )
    assert isinstance(count, int)
    return count
