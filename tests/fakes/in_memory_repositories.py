"""In-memory fakes for the repository Protocols (ADR-0006) — a second, real (if simplified)
implementation, run entirely in memory. Never a mock: nothing here patches internals, stubs a
method to return a canned value irrespective of input, or asserts on call counts/arguments.

FakeUnitOfWork gives real commit/rollback semantics without a database: entering stages a
snapshot of the shared FakeDatabase; a clean exit merges the snapshot back; an exception
discards it entirely, so a rolled-back attempt really does leave no trace, including its
idempotency record — the same guarantee ADR-0002's deferred FK gives for real."""

from __future__ import annotations

from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field, replace
from datetime import UTC, datetime
from typing import cast
from uuid import UUID

from wallet_transfer.domain.ledger_entry import LedgerEntry
from wallet_transfer.domain.transfer import Transfer
from wallet_transfer.domain.wallet import Wallet
from wallet_transfer.repositories.errors import IdempotencyKeyConflictError
from wallet_transfer.repositories.idempotency_repository import IdempotencyRecord
from wallet_transfer.repositories.unit_of_work import UnitOfWork


@dataclass
class FakeDatabase:
    wallets: dict[UUID, Wallet] = field(default_factory=dict)
    transfers: dict[UUID, Transfer] = field(default_factory=dict)
    ledger_entries: list[LedgerEntry] = field(default_factory=list)
    idempotency_records: dict[str, IdempotencyRecord] = field(default_factory=dict)

    def snapshot(self) -> FakeDatabase:
        return FakeDatabase(
            wallets=dict(self.wallets),
            transfers=dict(self.transfers),
            ledger_entries=list(self.ledger_entries),
            idempotency_records=dict(self.idempotency_records),
        )


class FakeWalletRepository:
    def __init__(self, database: FakeDatabase) -> None:
        self._database = database

    async def get_two_for_update(
        self, wallet_id_a: UUID, wallet_id_b: UUID
    ) -> Mapping[UUID, Wallet]:
        return {
            wallet_id: wallet
            for wallet_id in (wallet_id_a, wallet_id_b)
            if (wallet := self._database.wallets.get(wallet_id)) is not None
        }

    async def update_balance(self, wallet_id: UUID, new_balance: int) -> None:
        wallet = self._database.wallets[wallet_id]
        self._database.wallets[wallet_id] = replace(
            wallet, balance=new_balance, updated_at=datetime.now(UTC)
        )


class FakeTransferRepository:
    def __init__(self, database: FakeDatabase) -> None:
        self._database = database

    async def insert(self, transfer: Transfer) -> None:
        self._database.transfers[transfer.id] = transfer

    async def update(self, transfer: Transfer) -> None:
        self._database.transfers[transfer.id] = transfer

    async def get_by_id(self, transfer_id: UUID) -> Transfer | None:
        return self._database.transfers.get(transfer_id)


class FakeLedgerRepository:
    def __init__(self, database: FakeDatabase) -> None:
        self._database = database

    async def insert_entries(self, entries: Sequence[LedgerEntry]) -> None:
        self._database.ledger_entries.extend(entries)


class FakeIdempotencyRepository:
    def __init__(self, database: FakeDatabase) -> None:
        self._database = database

    async def insert(
        self, *, idempotency_key: str, request_fingerprint: str, transfer_id: UUID
    ) -> None:
        if idempotency_key in self._database.idempotency_records:
            raise IdempotencyKeyConflictError(idempotency_key)
        self._database.idempotency_records[idempotency_key] = IdempotencyRecord(
            idempotency_key=idempotency_key,
            request_fingerprint=request_fingerprint,
            transfer_id=transfer_id,
            created_at=datetime.now(UTC),
        )

    async def get_by_key(self, idempotency_key: str) -> IdempotencyRecord | None:
        return self._database.idempotency_records.get(idempotency_key)


class FakeUnitOfWork:
    """One transaction attempt against a shared FakeDatabase. Construct a new instance per
    attempt (FakeUnitOfWorkFactory does this) — reentering a used instance is not supported,
    same as a real database transaction can't be reused after commit/rollback."""

    def __init__(self, database: FakeDatabase) -> None:
        self._database = database
        self._staged: FakeDatabase | None = None

    async def __aenter__(self) -> FakeUnitOfWork:
        self._staged = self._database.snapshot()
        self.wallets = FakeWalletRepository(self._staged)
        self.transfers = FakeTransferRepository(self._staged)
        self.ledger = FakeLedgerRepository(self._staged)
        self.idempotency = FakeIdempotencyRepository(self._staged)
        return self

    async def __aexit__(self, exc_type: object, exc: object, tb: object) -> None:
        if exc_type is None and self._staged is not None:
            self._database.wallets = self._staged.wallets
            self._database.transfers = self._staged.transfers
            self._database.ledger_entries = self._staged.ledger_entries
            self._database.idempotency_records = self._staged.idempotency_records
        self._staged = None


class _FailingUnitOfWork:
    """Simulates an attempt that never got anywhere — a lock_timeout, deadlock_detected, or
    pool-acquire timeout all mean the transaction rolled back with nothing persisted, which is
    exactly what raising on __aenter__, before any repository is even handed out, models."""

    def __init__(self, error: Exception) -> None:
        self._error = error

    async def __aenter__(self) -> FakeUnitOfWork:
        raise self._error

    async def __aexit__(self, exc_type: object, exc: object, tb: object) -> None:
        return None


class FakeUnitOfWorkFactory:
    """A UnitOfWorkFactory whose next `attempts_to_fail` calls raise the given error instead of
    returning a working UnitOfWork — for ST3 (fails once, then succeeds) and ST4 (fails every
    attempt) without needing real lock contention to produce a retryable error."""

    def __init__(
        self,
        database: FakeDatabase | None = None,
        *,
        fail_with: Exception | None = None,
        attempts_to_fail: int = 0,
    ) -> None:
        self.database = database or FakeDatabase()
        self._fail_with = fail_with
        self._attempts_to_fail = attempts_to_fail
        self.call_count = 0

    def __call__(self) -> UnitOfWork:
        self.call_count += 1
        if self._fail_with is not None and self.call_count <= self._attempts_to_fail:
            return cast(UnitOfWork, _FailingUnitOfWork(self._fail_with))
        return FakeUnitOfWork(self.database)
