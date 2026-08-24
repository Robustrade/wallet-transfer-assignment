from __future__ import annotations

from collections.abc import Sequence
from uuid import UUID


class WalletNotFoundError(Exception):
    """A referenced wallet does not exist. Raised inside the transaction (the locking
    SELECT ... FOR UPDATE is the only query that can discover this), so the whole transaction —
    idempotency record included — has already rolled back by the time this propagates
    (ADR-0002, ADR-0003). Maps to 404."""

    def __init__(self, wallet_ids: Sequence[UUID]) -> None:
        super().__init__(f"wallet(s) not found: {', '.join(str(w) for w in wallet_ids)}")
        self.wallet_ids = tuple(wallet_ids)


class IdempotencyKeyReusedError(Exception):
    """The idempotency key was already used for a request with a different fingerprint. Maps
    to 409."""

    def __init__(self, idempotency_key: str) -> None:
        super().__init__(f"idempotency key already used for a different request: {idempotency_key}")
        self.idempotency_key = idempotency_key


class RetryExhaustedError(Exception):
    """Every bounded-retry attempt hit a RetryableRepositoryError; nothing was persisted by any
    of them, so it's safe to retry later with the same idempotency key (ADR-0003). Maps to 503."""

    def __init__(self, attempts: int) -> None:
        super().__init__(f"exhausted {attempts} attempts under lock contention")
        self.attempts = attempts
