from __future__ import annotations

import hashlib
from uuid import UUID


def compute_request_fingerprint(*, from_wallet_id: UUID, to_wallet_id: UUID, amount: int) -> str:
    """SHA-256 of the canonicalized (fromWalletId, toWalletId, amount) tuple (ADR-0002) — not
    the raw request bytes, so incidental formatting differences in an otherwise-identical
    logical request don't produce a false idempotency-key conflict."""
    canonical = f"{from_wallet_id}:{to_wallet_id}:{amount}"
    return hashlib.sha256(canonical.encode("utf-8")).hexdigest()
