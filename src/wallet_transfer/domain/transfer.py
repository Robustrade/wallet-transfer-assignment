from __future__ import annotations

from dataclasses import dataclass, replace
from datetime import UTC, datetime
from enum import StrEnum
from uuid import UUID


class TransferStatus(StrEnum):
    PENDING = "PENDING"
    PROCESSED = "PROCESSED"
    FAILED = "FAILED"


class InvalidTransferError(ValueError):
    """A Transfer would be constructed in a shape that is never valid (self-transfer,
    non-positive amount, a FAILED transfer with no reason)."""


class InvalidTransferTransitionError(ValueError):
    """A Transfer state transition other than PENDING -> PROCESSED/FAILED was attempted."""


@dataclass(frozen=True, slots=True)
class Transfer:
    id: UUID
    from_wallet_id: UUID
    to_wallet_id: UUID
    amount: int
    status: TransferStatus
    failure_reason: str | None
    created_at: datetime
    updated_at: datetime

    @staticmethod
    def create(
        *,
        id: UUID,
        from_wallet_id: UUID,
        to_wallet_id: UUID,
        amount: int,
        created_at: datetime | None = None,
    ) -> Transfer:
        if from_wallet_id == to_wallet_id:
            raise InvalidTransferError("a transfer cannot move funds to the same wallet")
        if amount <= 0:
            raise InvalidTransferError("transfer amount must be positive")
        now = created_at or datetime.now(UTC)
        return Transfer(
            id=id,
            from_wallet_id=from_wallet_id,
            to_wallet_id=to_wallet_id,
            amount=amount,
            status=TransferStatus.PENDING,
            failure_reason=None,
            created_at=now,
            updated_at=now,
        )

    def mark_processed(self, *, at: datetime | None = None) -> Transfer:
        self._require_pending("PROCESSED")
        return replace(
            self,
            status=TransferStatus.PROCESSED,
            updated_at=at or datetime.now(UTC),
        )

    def mark_failed(self, reason: str, *, at: datetime | None = None) -> Transfer:
        self._require_pending("FAILED")
        if not reason:
            raise InvalidTransferError("a FAILED transfer must have a failure_reason")
        return replace(
            self,
            status=TransferStatus.FAILED,
            failure_reason=reason,
            updated_at=at or datetime.now(UTC),
        )

    def _require_pending(self, target: str) -> None:
        if self.status is not TransferStatus.PENDING:
            raise InvalidTransferTransitionError(f"cannot mark {self.status} transfer as {target}")
