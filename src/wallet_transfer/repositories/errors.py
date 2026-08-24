from __future__ import annotations


class RetryableRepositoryError(Exception):
    """Base class for the three retryable error classes (ADR-0003): a lock_timeout, Postgres's
    own deadlock_detected, and a connection-pool-acquire timeout. The service layer catches this
    base class to drive bounded retry, regardless of which concrete class was raised."""


class LockTimeoutError(RetryableRepositoryError):
    """A lock wait (a wallet SELECT ... FOR UPDATE, or the idempotency-insert's unique-constraint
    conflict wait) exceeded the transaction's lock_timeout."""


class DeadlockDetectedError(RetryableRepositoryError):
    """Postgres detected and broke a deadlock. Kept as defense-in-depth: the ascending wallet_id
    lock order should make this unreachable, but a future code path that doesn't follow it would
    otherwise fail silently wrong instead of loudly retrying (ADR-0003)."""


class PoolTimeoutError(RetryableRepositoryError):
    """A connection could not be acquired from the pool before it timed out — a distinct failure
    mode from a row-lock timeout, since it happens before a transaction is even opened."""


class IdempotencyKeyConflictError(Exception):
    """Raised by IdempotencyRepository.insert() when the key already exists (unique violation).
    The caller looks up the existing record via get_by_key() to decide fingerprint match
    (replay the cached result) vs. mismatch (409)."""
