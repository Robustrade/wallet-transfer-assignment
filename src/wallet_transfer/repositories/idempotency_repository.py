from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from typing import Protocol
from uuid import UUID


@dataclass(frozen=True, slots=True)
class IdempotencyRecord:
    idempotency_key: str
    request_fingerprint: str
    transfer_id: UUID
    created_at: datetime


class IdempotencyRepository(Protocol):
    async def insert(
        self, *, idempotency_key: str, request_fingerprint: str, transfer_id: UUID
    ) -> None:
        """Insert the idempotency record, referencing a transfer id that does not need to exist
        yet in this same transaction (the FK is DEFERRABLE INITIALLY DEFERRED — ADR-0002/0004).
        Raises IdempotencyKeyConflictError if the key already exists (unique violation) —
        another attempt is already holding, or has already resolved, this key."""
        ...

    async def get_by_key(self, idempotency_key: str) -> IdempotencyRecord | None:
        """Look up an existing idempotency record by key, to compare fingerprints and find the
        transfer it guards."""
        ...
