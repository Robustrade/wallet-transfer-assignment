from __future__ import annotations

from uuid import UUID

import asyncpg

from wallet_transfer.domain.transfer import Transfer, TransferStatus


class AsyncpgTransferRepository:
    def __init__(self, connection: asyncpg.pool.PoolConnectionProxy) -> None:
        self._connection = connection

    async def insert(self, transfer: Transfer) -> None:
        await self._connection.execute(
            """
            INSERT INTO transfers
                (id, from_wallet_id, to_wallet_id, amount, status, failure_reason,
                 created_at, updated_at)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
            """,
            transfer.id,
            transfer.from_wallet_id,
            transfer.to_wallet_id,
            transfer.amount,
            transfer.status.value,
            transfer.failure_reason,
            transfer.created_at,
            transfer.updated_at,
        )

    async def update(self, transfer: Transfer) -> None:
        await self._connection.execute(
            "UPDATE transfers SET status = $1, failure_reason = $2, updated_at = $3 WHERE id = $4",
            transfer.status.value,
            transfer.failure_reason,
            transfer.updated_at,
            transfer.id,
        )

    async def get_by_id(self, transfer_id: UUID) -> Transfer | None:
        row = await self._connection.fetchrow(
            """
            SELECT id, from_wallet_id, to_wallet_id, amount, status, failure_reason,
                   created_at, updated_at
            FROM transfers WHERE id = $1
            """,
            transfer_id,
        )
        if row is None:
            return None
        return Transfer(
            id=row["id"],
            from_wallet_id=row["from_wallet_id"],
            to_wallet_id=row["to_wallet_id"],
            amount=row["amount"],
            status=TransferStatus(row["status"]),
            failure_reason=row["failure_reason"],
            created_at=row["created_at"],
            updated_at=row["updated_at"],
        )
