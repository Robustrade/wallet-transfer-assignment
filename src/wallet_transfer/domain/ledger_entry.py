from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from enum import StrEnum
from uuid import UUID


class LedgerEntryType(StrEnum):
    DEBIT = "DEBIT"
    CREDIT = "CREDIT"


@dataclass(frozen=True, slots=True)
class LedgerEntry:
    id: UUID
    transfer_id: UUID
    wallet_id: UUID
    type: LedgerEntryType
    amount: int
    created_at: datetime
