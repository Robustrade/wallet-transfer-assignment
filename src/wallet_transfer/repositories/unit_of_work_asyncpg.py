from __future__ import annotations

import asyncpg

from wallet_transfer.repositories.errors import (
    DeadlockDetectedError,
    LockTimeoutError,
    PoolTimeoutError,
    RetryableRepositoryError,
)
from wallet_transfer.repositories.idempotency_repository_asyncpg import (
    AsyncpgIdempotencyRepository,
)
from wallet_transfer.repositories.ledger_repository_asyncpg import AsyncpgLedgerRepository
from wallet_transfer.repositories.transfer_repository_asyncpg import AsyncpgTransferRepository
from wallet_transfer.repositories.wallet_repository_asyncpg import AsyncpgWalletRepository


def _translate(error: BaseException) -> RetryableRepositoryError | None:
    """Map the two lock-wait error classes onto our own retryable errors. lock_timeout
    (SQLSTATE 55P03) covers both a wallet SELECT ... FOR UPDATE wait and the idempotency-insert's
    unique-constraint conflict wait, since both are lock waits under the same SET LOCAL
    lock_timeout (ADR-0003) — one translation point handles both. deadlock_detected (40P01) is
    kept as defense-in-depth even though the ascending lock order should make it unreachable."""
    if isinstance(error, asyncpg.exceptions.LockNotAvailableError):
        return LockTimeoutError(str(error))
    if isinstance(error, asyncpg.exceptions.DeadlockDetectedError):
        return DeadlockDetectedError(str(error))
    return None


class AsyncpgUnitOfWork:
    def __init__(
        self,
        pool: asyncpg.Pool,
        *,
        lock_timeout_ms: int = 2000,
        pool_acquire_timeout: float = 5.0,
    ) -> None:
        self._pool = pool
        self._lock_timeout_ms = lock_timeout_ms
        self._pool_acquire_timeout = pool_acquire_timeout
        self._connection: asyncpg.pool.PoolConnectionProxy | None = None
        self._transaction: asyncpg.transaction.Transaction | None = None

    async def __aenter__(self) -> AsyncpgUnitOfWork:
        try:
            self._connection = await self._pool.acquire(timeout=self._pool_acquire_timeout)
        except TimeoutError as error:
            raise PoolTimeoutError(str(error)) from error

        self._transaction = self._connection.transaction()
        await self._transaction.start()
        # Once per transaction (not per statement) — governs every lock wait inside it,
        # including the idempotency-insert's unique-constraint conflict wait (ADR-0003).
        await self._connection.execute(f"SET LOCAL lock_timeout = '{self._lock_timeout_ms}ms'")

        self.wallets = AsyncpgWalletRepository(self._connection)
        self.transfers = AsyncpgTransferRepository(self._connection)
        self.ledger = AsyncpgLedgerRepository(self._connection)
        self.idempotency = AsyncpgIdempotencyRepository(self._connection)
        return self

    async def __aexit__(self, exc_type: object, exc: BaseException | None, tb: object) -> None:
        assert self._connection is not None
        assert self._transaction is not None
        try:
            if exc_type is None:
                await self._transaction.commit()
            else:
                await self._transaction.rollback()
                if exc is not None:
                    translated = _translate(exc)
                    if translated is not None:
                        raise translated from exc
        finally:
            await self._pool.release(self._connection)
            self._connection = None
            self._transaction = None


class AsyncpgUnitOfWorkFactory:
    def __init__(
        self,
        pool: asyncpg.Pool,
        *,
        lock_timeout_ms: int = 2000,
        pool_acquire_timeout: float = 5.0,
    ) -> None:
        self._pool = pool
        self._lock_timeout_ms = lock_timeout_ms
        self._pool_acquire_timeout = pool_acquire_timeout

    def __call__(self) -> AsyncpgUnitOfWork:
        return AsyncpgUnitOfWork(
            self._pool,
            lock_timeout_ms=self._lock_timeout_ms,
            pool_acquire_timeout=self._pool_acquire_timeout,
        )
