from __future__ import annotations

from collections.abc import Mapping
from typing import Protocol
from uuid import UUID

from wallet_transfer.domain.wallet import Wallet


class WalletRepository(Protocol):
    async def get_two_for_update(
        self, wallet_id_a: UUID, wallet_id_b: UUID
    ) -> Mapping[UUID, Wallet]:
        """Lock both wallet rows (SELECT ... FOR UPDATE), always in ascending wallet_id order
        regardless of the order the two ids are passed in — this is the actual deadlock-avoidance
        mechanism (ADR-0003), not a caller convention. Returns only the wallets that exist; a
        missing id is simply absent from the result, letting the caller detect a nonexistent
        wallet without a separate existence check."""
        ...

    async def update_balance(self, wallet_id: UUID, new_balance: int) -> None:
        """Persist a wallet's new balance. Must be called only on a row already locked for
        update in the same transaction."""
        ...
