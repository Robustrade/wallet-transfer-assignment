from __future__ import annotations

from collections.abc import Mapping
from uuid import UUID

import asyncpg

from wallet_transfer.domain.wallet import Wallet


class AsyncpgWalletRepository:
    def __init__(self, connection: asyncpg.pool.PoolConnectionProxy) -> None:
        self._connection = connection

    async def get_two_for_update(
        self, wallet_id_a: UUID, wallet_id_b: UUID
    ) -> Mapping[UUID, Wallet]:
        # Two separate, sequentially-awaited statements in ascending id order — NOT a single
        # `WHERE id = ANY(...) ORDER BY ... FOR UPDATE` query. Postgres's LockRows plan node
        # locks rows in scan order, not ORDER BY order, so a single combined query does not
        # actually guarantee the ascending lock order ADR-0003's deadlock-avoidance depends on.
        result: dict[UUID, Wallet] = {}
        for wallet_id in sorted({wallet_id_a, wallet_id_b}):
            row = await self._connection.fetchrow(
                "SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1 FOR UPDATE",
                wallet_id,
            )
            if row is not None:
                result[row["id"]] = Wallet(
                    id=row["id"],
                    balance=row["balance"],
                    created_at=row["created_at"],
                    updated_at=row["updated_at"],
                )
        return result

    async def update_balance(self, wallet_id: UUID, new_balance: int) -> None:
        await self._connection.execute(
            "UPDATE wallets SET balance = $1, updated_at = now() WHERE id = $2",
            new_balance,
            wallet_id,
        )
