from __future__ import annotations

from typing import Protocol

from wallet_transfer.repositories.idempotency_repository import IdempotencyRepository
from wallet_transfer.repositories.ledger_repository import LedgerRepository
from wallet_transfer.repositories.transfer_repository import TransferRepository
from wallet_transfer.repositories.wallet_repository import WalletRepository


class UnitOfWork(Protocol):
    """Binds the four repositories to one database transaction — the "same transaction" every
    ADR assumes for a transfer attempt (ADR-0002, ADR-0003): the idempotency-record insert,
    wallet locks, balance check, ledger writes, and status update all commit or roll back
    together.

    Used as an async context manager: entering opens the transaction, a clean exit commits, and
    an exception propagating out of the block rolls back everything done inside it — including
    the idempotency-record insert, since its FK is deferred to commit (ADR-0002/0004)."""

    wallets: WalletRepository
    transfers: TransferRepository
    ledger: LedgerRepository
    idempotency: IdempotencyRepository

    async def __aenter__(self) -> UnitOfWork: ...

    async def __aexit__(self, exc_type: object, exc: object, tb: object) -> None: ...


class UnitOfWorkFactory(Protocol):
    def __call__(self) -> UnitOfWork:
        """Return a fresh UnitOfWork for one transaction attempt. Called once per bounded-retry
        attempt (ADR-0003) — a retried attempt gets its own new transaction, never a reused one."""
        ...
