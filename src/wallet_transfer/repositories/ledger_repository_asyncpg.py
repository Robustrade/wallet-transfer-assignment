from __future__ import annotations

from collections.abc import Sequence

import asyncpg

from wallet_transfer.domain.ledger_entry import LedgerEntry


class AsyncpgLedgerRepository:
    def __init__(self, connection: asyncpg.pool.PoolConnectionProxy) -> None:
        self._connection = connection

    async def insert_entries(self, entries: Sequence[LedgerEntry]) -> None:
        await self._connection.executemany(
            """
            INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount, created_at)
            VALUES ($1, $2, $3, $4, $5, $6)
            """,
            [
                (
                    entry.id,
                    entry.transfer_id,
                    entry.wallet_id,
                    entry.type.value,
                    entry.amount,
                    entry.created_at,
                )
                for entry in entries
            ],
        )
