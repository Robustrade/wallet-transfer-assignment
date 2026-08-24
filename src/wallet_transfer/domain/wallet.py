from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from uuid import UUID


@dataclass(frozen=True, slots=True)
class Wallet:
    id: UUID
    balance: int
    created_at: datetime
    updated_at: datetime
