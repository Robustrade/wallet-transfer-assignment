from __future__ import annotations

import asyncio
import logging
import random
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from uuid import UUID

from uuid6 import uuid7

from wallet_transfer.domain.ledger_entry import LedgerEntry, LedgerEntryType
from wallet_transfer.domain.transfer import Transfer
from wallet_transfer.repositories.errors import (
    IdempotencyKeyConflictError,
    RetryableRepositoryError,
)
from wallet_transfer.repositories.unit_of_work import UnitOfWorkFactory
from wallet_transfer.services.errors import (
    IdempotencyKeyReusedError,
    RetryExhaustedError,
    WalletNotFoundError,
)
from wallet_transfer.services.fingerprint import compute_request_fingerprint

logger = logging.getLogger(__name__)


@dataclass(frozen=True, slots=True)
class CreateTransferRequest:
    idempotency_key: str
    from_wallet_id: UUID
    to_wallet_id: UUID
    amount: int


class TransferService:
    def __init__(
        self,
        uow_factory: UnitOfWorkFactory,
        *,
        id_generator: Callable[[], UUID] = uuid7,
        clock: Callable[[], datetime] = lambda: datetime.now(UTC),
        sleep: Callable[[float], Awaitable[None]] = asyncio.sleep,
        max_attempts: int = 3,
        base_backoff_seconds: float = 0.05,
        on_retry: Callable[[int, RetryableRepositoryError], Awaitable[None]] | None = None,
    ) -> None:
        self._uow_factory = uow_factory
        self._id_generator = id_generator
        self._clock = clock
        self._sleep = sleep
        self.max_attempts = max_attempts
        self._base_backoff_seconds = base_backoff_seconds
        # Test-only synchronization seam (ADR-0003/CT7): fires right after an attempt rolls back
        # on a retryable error, before the next attempt starts, so a test can deterministically
        # land a client-initiated retry in that exact gap instead of guessing at a sleep duration.
        self._on_retry = on_retry

    async def create_transfer(self, request: CreateTransferRequest) -> Transfer:
        fingerprint = compute_request_fingerprint(
            from_wallet_id=request.from_wallet_id,
            to_wallet_id=request.to_wallet_id,
            amount=request.amount,
        )

        last_error: RetryableRepositoryError | None = None
        for attempt in range(1, self.max_attempts + 1):
            if attempt > 1:
                await self._sleep_backoff(attempt)

            # Domain-level construction validates self-transfer / non-positive amount
            # (InvalidTransferError) before any transaction opens (ADR-0002's validation
            # boundary) — nothing persisted for these, and no retry applies.
            pending_transfer = Transfer.create(
                id=self._id_generator(),
                from_wallet_id=request.from_wallet_id,
                to_wallet_id=request.to_wallet_id,
                amount=request.amount,
                created_at=self._clock(),
            )

            logger.debug(
                "transfer attempt %d/%d starting: idempotency_key=%s",
                attempt,
                self.max_attempts,
                request.idempotency_key,
            )

            try:
                result = await self._attempt(request, fingerprint, pending_transfer)
            except RetryableRepositoryError as error:
                last_error = error
                logger.warning(
                    "transfer attempt %d/%d hit %s, retrying: idempotency_key=%s",
                    attempt,
                    self.max_attempts,
                    type(error).__name__,
                    request.idempotency_key,
                )
                if self._on_retry is not None:
                    await self._on_retry(attempt, error)
                continue
            except (WalletNotFoundError, IdempotencyKeyReusedError) as error:
                logger.info(
                    "transfer rejected on attempt %d: idempotency_key=%s reason=%s",
                    attempt,
                    request.idempotency_key,
                    type(error).__name__,
                )
                raise
            else:
                logger.info(
                    "transfer resolved after %d attempt(s): idempotency_key=%s "
                    "transfer_id=%s status=%s",
                    attempt,
                    request.idempotency_key,
                    result.id,
                    result.status.value,
                )
                return result

        assert last_error is not None
        logger.error(
            "transfer retries exhausted after %d attempts: idempotency_key=%s",
            self.max_attempts,
            request.idempotency_key,
        )
        raise RetryExhaustedError(self.max_attempts) from last_error

    async def _attempt(
        self,
        request: CreateTransferRequest,
        fingerprint: str,
        pending_transfer: Transfer,
    ) -> Transfer:
        async with self._uow_factory() as uow:
            try:
                await uow.idempotency.insert(
                    idempotency_key=request.idempotency_key,
                    request_fingerprint=fingerprint,
                    transfer_id=pending_transfer.id,
                )
            except IdempotencyKeyConflictError:
                existing = await uow.idempotency.get_by_key(request.idempotency_key)
                assert existing is not None
                if existing.request_fingerprint != fingerprint:
                    raise IdempotencyKeyReusedError(request.idempotency_key) from None
                cached = await uow.transfers.get_by_id(existing.transfer_id)
                assert cached is not None
                return cached

            # Wallets are locked BEFORE the transfer row is inserted — not the order ADR-0002's
            # prose originally described. Postgres takes an implicit FOR KEY SHARE lock on a
            # referenced row for every FK column an INSERT touches, acquired in whatever order
            # the columns are checked, not in the ascending-id order our own explicit locking
            # uses below. Insert the transfer first and two concurrent attempts sharing a wallet
            # can each hold that row's implicit FK lock from their own INSERT while waiting on
            # the other's — a genuine deadlock (SQLSTATE 40P01) that the ascending lock order was
            # supposed to make impossible. Locking first means our own FOR UPDATE already holds
            # the strongest lock on both rows before the transfer INSERT's FK check ever runs, so
            # that check is uncontended. Found via real-Postgres testing (CT1/CT7) — the in-memory
            # fake can't reproduce Postgres's implicit FK locking, which is exactly why ADR-0006
            # requires this tier to run against the real thing.
            wallets = await uow.wallets.get_two_for_update(
                request.from_wallet_id, request.to_wallet_id
            )
            missing = [
                wallet_id
                for wallet_id in (request.from_wallet_id, request.to_wallet_id)
                if wallet_id not in wallets
            ]
            if missing:
                raise WalletNotFoundError(missing)

            from_wallet = wallets[request.from_wallet_id]
            to_wallet = wallets[request.to_wallet_id]

            await uow.transfers.insert(pending_transfer)

            if from_wallet.balance < request.amount:
                failed = pending_transfer.mark_failed("INSUFFICIENT_FUNDS", at=self._clock())
                await uow.transfers.update(failed)
                return failed

            await uow.wallets.update_balance(from_wallet.id, from_wallet.balance - request.amount)
            await uow.wallets.update_balance(to_wallet.id, to_wallet.balance + request.amount)

            processed = pending_transfer.mark_processed(at=self._clock())
            await uow.transfers.update(processed)

            now = self._clock()
            await uow.ledger.insert_entries(
                [
                    LedgerEntry(
                        id=self._id_generator(),
                        transfer_id=processed.id,
                        wallet_id=from_wallet.id,
                        type=LedgerEntryType.DEBIT,
                        amount=request.amount,
                        created_at=now,
                    ),
                    LedgerEntry(
                        id=self._id_generator(),
                        transfer_id=processed.id,
                        wallet_id=to_wallet.id,
                        type=LedgerEntryType.CREDIT,
                        amount=request.amount,
                        created_at=now,
                    ),
                ]
            )

            return processed

    async def _sleep_backoff(self, attempt: int) -> None:
        max_delay = self._base_backoff_seconds * (2 ** (attempt - 2))
        await self._sleep(random.uniform(0, max_delay))
