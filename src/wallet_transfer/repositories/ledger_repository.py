from __future__ import annotations

from collections.abc import Sequence
from typing import Protocol

from wallet_transfer.domain.ledger_entry import LedgerEntry


class LedgerRepository(Protocol):
    async def insert_entries(self, entries: Sequence[LedgerEntry]) -> None:
        """Insert the ledger entries for a PROCESSED transfer — exactly one DEBIT and one
        CREDIT, equal amounts (LT1)."""
        ...
