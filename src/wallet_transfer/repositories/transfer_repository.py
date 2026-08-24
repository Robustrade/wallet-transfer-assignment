from __future__ import annotations

from typing import Protocol
from uuid import UUID

from wallet_transfer.domain.transfer import Transfer


class TransferRepository(Protocol):
    async def insert(self, transfer: Transfer) -> None:
        """Insert a new transfer row. Called with a PENDING transfer, before the wallet locks
        are acquired (ADR-0002/ADR-0003 write order)."""
        ...

    async def update(self, transfer: Transfer) -> None:
        """Persist a transfer's resolved terminal state (PROCESSED or FAILED)."""
        ...

    async def get_by_id(self, transfer_id: UUID) -> Transfer | None:
        """Look up a transfer by id — used to return the cached result of an idempotent
        replay."""
        ...
