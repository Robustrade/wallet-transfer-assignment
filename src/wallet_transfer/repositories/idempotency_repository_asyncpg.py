from __future__ import annotations

from uuid import UUID

import asyncpg

from wallet_transfer.repositories.errors import IdempotencyKeyConflictError
from wallet_transfer.repositories.idempotency_repository import IdempotencyRecord


class AsyncpgIdempotencyRepository:
    def __init__(self, connection: asyncpg.pool.PoolConnectionProxy) -> None:
        self._connection = connection

    async def insert(
        self, *, idempotency_key: str, request_fingerprint: str, transfer_id: UUID
    ) -> None:
        try:
            # Nested inside the UnitOfWork's already-open transaction, this becomes a SAVEPOINT
            # (asyncpg's transaction() detects the outer transaction automatically). Without it,
            # a UniqueViolationError here poisons the *whole* outer transaction — every later
            # query on this connection, including the get_by_key() lookup the caller does next to
            # find the cached result, would fail with InFailedSQLTransactionError instead.
            async with self._connection.transaction():
                await self._connection.execute(
                    """
                    INSERT INTO idempotency_records
                        (idempotency_key, request_fingerprint, transfer_id)
                    VALUES ($1, $2, $3)
                    """,
                    idempotency_key,
                    request_fingerprint,
                    transfer_id,
                )
        except asyncpg.exceptions.UniqueViolationError as error:
            raise IdempotencyKeyConflictError(idempotency_key) from error

    async def get_by_key(self, idempotency_key: str) -> IdempotencyRecord | None:
        row = await self._connection.fetchrow(
            """
            SELECT idempotency_key, request_fingerprint, transfer_id, created_at
            FROM idempotency_records WHERE idempotency_key = $1
            """,
            idempotency_key,
        )
        if row is None:
            return None
        return IdempotencyRecord(
            idempotency_key=row["idempotency_key"],
            request_fingerprint=row["request_fingerprint"],
            transfer_id=row["transfer_id"],
            created_at=row["created_at"],
        )
